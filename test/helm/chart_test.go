/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

var _ = Describe("operator bootstrap chart", func() {
	It("renders only bootstrap resources and retains the Kubernaut CRD", func() {
		objects := renderChart()

		crds := objects.ofKind("CustomResourceDefinition")
		Expect(crds).To(HaveLen(1))
		Expect(crds[0].name()).To(Equal("kubernauts.kubernaut.ai"))
		Expect(crds[0].nestedString("metadata", "annotations", "helm.sh/resource-policy")).To(Equal("keep"))
		Expect(crds[0].nestedString("spec", "group")).To(Equal("kubernaut.ai"))
		Expect(crds[0].nestedString("spec", "scope")).To(Equal("Namespaced"))

		Expect(objects.ofKind("Kubernaut")).To(BeEmpty(), "the chart must not create an operand CR")
		Expect(objects.ofKind("Deployment")).To(HaveLen(1))
		Expect(objects.ofKind("Deployment")[0].name()).To(Equal("kubernaut-operator-controller-manager"))

		Expect(objects.ofKind("Secret")).To(BeEmpty(),
			"development TLS private keys must be created by the restricted bootstrap Job, not stored in Helm release data")
		jobs := objects.ofKind("Job")
		Expect(jobs).To(HaveLen(2), "development TLS renders certificate bootstrap and CA publication hooks")
		bootstrap := jobs.named("Job", "kubernaut-operator-cert-bootstrap")
		Expect(bootstrap.nestedString("spec", "template", "spec", "serviceAccountName")).To(Equal(
			"kubernaut-operator-cert-bootstrap"))
		Expect(bootstrap.nestedString("spec", "template", "spec", "containers", "0", "name")).To(Equal(
			"certificate-bootstrap"))
		patcher := jobs.named("Job", "kubernaut-operator-cert-patch")
		Expect(patcher.nestedString("spec", "template", "spec", "serviceAccountName")).To(Equal(
			"kubernaut-operator-cert-patch"))
	})

	It("renders a fail-closed namespaced singleton webhook with deterministic names", func() {
		webhooks := renderChart().ofKind("ValidatingWebhookConfiguration")
		Expect(webhooks).To(HaveLen(1))

		webhook := webhooks[0]
		Expect(webhook.name()).To(Equal("kubernaut-operator-singleton"))
		Expect(webhook.nestedString("webhooks", "0", "failurePolicy")).To(Equal("Fail"))
		Expect(webhook.nestedString("webhooks", "0", "rules", "0", "scope")).To(Equal("Namespaced"))
		Expect(webhook.nestedString(
			"webhooks", "0", "clientConfig", "service", "name",
		)).To(Equal("kubernaut-operator-webhook"))
		Expect(webhook.nestedString("webhooks", "0", "clientConfig", "service", "port")).To(Equal("9443"))
		Expect(webhook.nestedString(
			"webhooks", "0", "clientConfig", "service", "path",
		)).To(Equal("/validate-kubernaut-singleton"))
		Expect(webhook.hasPath("webhooks", "0", "clientConfig", "caBundle")).To(BeFalse(),
			"the development CA publisher, not Helm, must own the injected CA field")
		Expect(webhook.annotations()).NotTo(HaveKey("service.beta.openshift.io/inject-cabundle"))
		Expect(webhook.annotations()).NotTo(HaveKey("cert-manager.io/inject-ca-from"))
	})

	It("uses security defaults accepted by the OpenShift restricted SCC", func() {
		deployment := renderChart("--set", "webhook.tls.mode=openshift").ofKind("Deployment")[0]
		podSpec := []string{"spec", "template", "spec"}

		Expect(deployment.hasPath(append(podSpec, "hostUsers")...)).To(BeFalse(),
			"the platform must select the compatible user-namespace mode by default")
		Expect(deployment.hasPath(append(podSpec, "securityContext", "fsGroup")...)).To(BeFalse(),
			"OpenShift must allocate the supplemental group instead of receiving a fixed GID")

		development := renderChart().ofKind("Deployment")[0]
		Expect(development.nestedString(append(podSpec, "securityContext", "fsGroup")...)).To(Equal("65534"),
			"development TLS must preserve the generic Kubernetes supplemental-group default")
		Expect(development.hasPath(append(podSpec, "initContainers")...)).To(BeFalse(),
			"certificate provisioning must not run in the manager Pod")
		Expect(development.nestedString(
			append(podSpec, "volumes", "0", "secret", "secretName")...,
		)).To(Equal("kubernaut-operator-webhook-cert"))

		override := renderChart("--set", "hostUsers=false").ofKind("Deployment")[0]
		Expect(override.nestedString(append(podSpec, "hostUsers")...)).To(Equal("false"))
	})

	It("does not render development resources for administrator-managed TLS", func() {
		objects := renderChart(
			"--set", "webhook.tls.mode=manual",
			"--set", "webhook.tls.existingSecret=operator-webhook-cert",
			"--set", "webhook.tls.caBundle=Y2E=",
		)
		Expect(objects.ofKind("Job")).To(BeEmpty())
		Expect(objects.ofKind("Certificate")).To(BeEmpty())
		Expect(objects.ofKind("Secret")).To(BeEmpty())

		webhook := objects.ofKind("ValidatingWebhookConfiguration")[0]
		Expect(webhook.nestedString("webhooks", "0", "clientConfig", "caBundle")).To(Equal("Y2E="))
		Expect(webhook.annotations()).NotTo(HaveKey("service.beta.openshift.io/inject-cabundle"))
	})

	It("renders cert-manager only when the cert-manager profile is selected", func() {
		objects := renderChart(
			"--set", "webhook.tls.mode=certManager",
			"--set", "webhook.tls.certManager.issuerRef.name=operator-issuer",
			"--set", "webhook.tls.certManager.issuerRef.kind=ClusterIssuer",
		)
		Expect(objects.ofKind("Job")).To(BeEmpty())
		certificates := objects.ofKind("Certificate")
		Expect(certificates).To(HaveLen(1))
		Expect(certificates[0].name()).To(Equal("kubernaut-operator-webhook-cert"))
		Expect(certificates[0].nestedString("metadata", "annotations", "helm.sh/resource-policy")).To(Equal("keep"))
		Expect(certificates[0].nestedString("spec", "secretName")).To(Equal("kubernaut-operator-webhook-cert"))
		Expect(certificates[0].nestedString("spec", "issuerRef", "name")).To(Equal("operator-issuer"))

		webhook := objects.ofKind("ValidatingWebhookConfiguration")[0]
		Expect(webhook.annotations()).To(HaveKeyWithValue(
			"cert-manager.io/inject-ca-from", "default/kubernaut-operator-webhook-cert",
		))
	})

	It("rejects incomplete administrator-managed and cert-manager TLS values", func() {
		output, err := runHelm(
			"template", "kubernaut-operator", chartPath(), "--namespace", "default",
			"--set", "webhook.tls.mode=manual",
			"--set", "webhook.tls.caBundle=Y2E=",
		)
		Expect(err).To(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("webhook.tls.existingSecret is required"))

		output, err = runHelm(
			"template", "kubernaut-operator", chartPath(), "--namespace", "default",
			"--set", "webhook.tls.mode=manual",
			"--set", "webhook.tls.existingSecret=operator-webhook-cert",
		)
		Expect(err).To(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("webhook.tls.caBundle is required"))

		output, err = runHelm(
			"template", "kubernaut-operator", chartPath(), "--namespace", "default",
			"--set", "webhook.tls.mode=certManager",
		)
		Expect(err).To(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("webhook.tls.certManager.issuerRef.name is required"))
	})

	It("rejects fail-open webhook policy overrides", func() {
		output, err := runHelm(
			"template", "kubernaut-operator", chartPath(), "--namespace", "default",
			"--set", "webhook.failurePolicy=Ignore",
		)
		Expect(err).To(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("failurePolicy"))
	})

	It("renders immutable operator images, pull Secrets, and secure metrics", func() {
		objects := renderChart(
			"--set", "image.repository=registry.example.invalid/kubernaut/operator",
			"--set", "image.tag=ignored",
			"--set", "image.digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"--set", "image.pullSecrets[0].name=operator-pull",
			"--set", "webhook.tls.development.image.pullSecrets[0].name=bootstrap-pull",
			"--set", "metrics.enabled=true",
		)

		deployment := objects.ofKind("Deployment")[0]
		Expect(deployment.nestedString("spec", "template", "spec", "containers", "0", "image")).To(Equal(
			"registry.example.invalid/kubernaut/operator@sha256:" +
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		))
		Expect(deployment.nestedString(
			"spec", "template", "spec", "imagePullSecrets", "0", "name",
		)).To(Equal("operator-pull"))
		Expect(deployment.nestedString(
			"spec", "template", "spec", "imagePullSecrets", "1", "name",
		)).To(Equal("bootstrap-pull"))
		Expect(deployment.nestedString(
			"spec", "template", "spec", "containers", "0", "args", "2",
		)).To(Equal("--metrics-bind-address=:8443"))
		Expect(deployment.hasPath("spec", "template", "spec", "initContainers")).To(BeFalse())
		bootstrap := objects.ofKind("Job").named("Job", "kubernaut-operator-cert-bootstrap")
		Expect(bootstrap.nestedString(
			"spec", "template", "spec", "containers", "0", "image",
		)).To(MatchRegexp(`@sha256:[0-9a-f]{64}$`))
		Expect(objects.ofKind("Service")).To(HaveLen(2))
		serviceNames := make([]string, 0, len(objects.ofKind("Service")))
		for _, service := range objects.ofKind("Service") {
			serviceNames = append(serviceNames, service.name())
		}
		Expect(serviceNames).To(ContainElement("kubernaut-operator-metrics"))
		clusterRoleNames := make([]string, 0, len(objects.ofKind("ClusterRole")))
		for _, role := range objects.ofKind("ClusterRole") {
			clusterRoleNames = append(clusterRoleNames, role.name())
		}
		Expect(clusterRoleNames).To(ContainElement("kubernaut-operator-metrics-reader"))
	})

	It("renders the explicit OpenShift service-CA profile without generic certificate jobs", func() {
		objects := renderChart("--set", "webhook.tls.mode=openshift")
		Expect(objects.ofKind("Job")).To(BeEmpty())
		Expect(objects.ofKind("Certificate")).To(BeEmpty())
		Expect(objects.ofKind("Service")[0].annotations()).To(HaveKeyWithValue(
			"service.beta.openshift.io/serving-cert-secret-name", "kubernaut-operator-webhook-cert",
		))
		Expect(objects.ofKind("ValidatingWebhookConfiguration")[0].annotations()).To(HaveKeyWithValue(
			"service.beta.openshift.io/inject-cabundle", "true",
		))
	})

	It("rejects application configuration values", func() {
		_, err := runHelm(
			"template", "kubernaut-operator", chartPath(), "--namespace", "default",
			"--set", "postgresql.host=database",
		)
		Expect(err).To(HaveOccurred())
	})

	It("keeps default related images synchronized with the generated manager manifest", func() {
		managerData, err := os.ReadFile(filepath.Join(repositoryRoot(), "config", "manager", "manager.yaml"))
		Expect(err).NotTo(HaveOccurred())
		valuesData, err := os.ReadFile(filepath.Join(chartPath(), "values.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var manager struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Env []struct {
								Name  string `yaml:"name"`
								Value string `yaml:"value"`
							} `yaml:"env"`
						} `yaml:"containers"`
					} `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		}
		var values struct {
			RelatedImages map[string]string `yaml:"relatedImages"`
		}
		Expect(yaml.Unmarshal(managerData, &manager)).To(Succeed())
		Expect(yaml.Unmarshal(valuesData, &values)).To(Succeed())

		expected := make(map[string]string)
		for _, env := range manager.Spec.Template.Spec.Containers[0].Env {
			if strings.HasPrefix(env.Name, "RELATED_IMAGE_") {
				expected[strings.TrimPrefix(env.Name, "RELATED_IMAGE_")] = env.Value
			}
		}
		Expect(values.RelatedImages).To(Equal(expected))
	})
})

type manifest map[string]any
type manifests []manifest

func renderChart(args ...string) manifests {
	output, err := runHelm(append(
		[]string{"template", "kubernaut-operator", chartPath(), "--namespace", "default", "--include-crds"},
		args...,
	)...)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), string(output))

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	var result manifests
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err.Error() == "EOF" {
				break
			}
			Fail(fmt.Sprintf("decoding Helm output: %v", err))
		}
		if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var object manifest
		ExpectWithOffset(1, json.Unmarshal(raw, &object)).To(Succeed())
		if len(object) > 0 {
			result = append(result, object)
		}
	}
	return result
}

func runHelm(args ...string) ([]byte, error) {
	bin := os.Getenv("HELM_BIN")
	if bin == "" {
		bin = "helm"
	}
	//nolint:gosec // Helm binary and arguments are controlled by the chart test harness
	command := exec.CommandContext(context.Background(), bin, args...)
	command.Dir = repositoryRoot()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}

func chartPath() string {
	return filepath.Join(repositoryRoot(), "charts", "kubernaut-operator")
}

func repositoryRoot() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		Fail("unable to locate Helm test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func (items manifests) ofKind(kind string) manifests {
	var result manifests
	for _, item := range items {
		if item.kind() == kind {
			result = append(result, item)
		}
	}
	return result
}

func (items manifests) named(kind, name string) manifest {
	for _, item := range items {
		if item.kind() == kind && item.name() == name {
			return item
		}
	}
	return nil
}

func (item manifest) kind() string {
	value, _ := item["kind"].(string)
	return value
}

func (item manifest) name() string {
	metadata, _ := item["metadata"].(map[string]any)
	value, _ := metadata["name"].(string)
	return value
}

func (item manifest) annotations() map[string]any {
	metadata, _ := item["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	return annotations
}

func (item manifest) nestedString(path ...string) string {
	var current any = map[string]any(item)
	for _, part := range path {
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || index < 0 || index >= len(value) {
				return ""
			}
			current = value[index]
		default:
			return ""
		}
	}

	if value, ok := current.(string); ok {
		return value
	}
	if value, ok := current.(float64); ok {
		return fmt.Sprintf("%g", value)
	}
	return strings.TrimSpace(fmt.Sprint(current))
}

func (item manifest) hasPath(path ...string) bool {
	var current any = map[string]any(item)
	for _, part := range path {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return false
			}
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || index < 0 || index >= len(value) {
				return false
			}
			current = value[index]
		default:
			return false
		}
	}
	return true
}
