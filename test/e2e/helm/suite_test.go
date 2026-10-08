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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
)

func TestHelmE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "operator Helm Kind e2e suite")
}

var (
	e2eContext      = "kind-" + clusterName()
	e2eNamespace    = "kubernaut-helm-e2e"
	e2eTLSMode      = "development"
	e2eMetrics      = false
	e2eDisconnected = false
	manualTLS       *manualTLSFixture
)

const (
	helmTLSManual          = "manual"
	helmTLSCertManager     = "certmanager"
	helmTLSDevelopment     = "development"
	helmCertManagerName    = "kubernaut-helm-e2e-issuer"
	helmPullSecretName     = "kubernaut-helm-e2e-pull"
	helmMetricsBinding     = "kubernaut-helm-e2e-metrics-reader"
	helmCertManagerVersion = "v1.20.2"
)

const helmCertManagerManifest = "https://github.com/cert-manager/cert-manager/releases/download/" +
	helmCertManagerVersion + "/cert-manager.yaml"

const helmKindNodeImage = "kindest/node:v1.35.0"

const helmCertBootstrapDigest = "sha256:6e2cdb22d6ab7264ea198c717f555e30536b54029d26c8781b9f25f78951b564"

const helmCertBootstrapSourceImage = "docker.io/bitnami/kubectl@" + helmCertBootstrapDigest

const helmCertBootstrapLocalImage = "localhost/bitnami/kubectl:issue489"

const helmCertManagerOwnerReferencePatch = `[
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enable-certificate-owner-ref=true"}
]`

type manualTLSFixture struct {
	namespace string
	caCert    *x509.Certificate
	caKey     *rsa.PrivateKey
	caPEM     []byte
	serial    int64
}

var _ = BeforeSuite(func() {
	var err error
	e2eTLSMode, err = tlsModeFromEnvironment()
	Expect(err).NotTo(HaveOccurred())
	e2eMetrics = strings.EqualFold(strings.TrimSpace(os.Getenv("KUBERNAUT_HELM_E2E_METRICS")), "true")
	e2eDisconnected = strings.EqualFold(strings.TrimSpace(os.Getenv("KUBERNAUT_HELM_E2E_DISCONNECTED")), "true")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	Expect(runChecked(ctx, "kind", "create", "cluster", "--name", clusterName(),
		"--image", helmKindNodeImage, "--wait", "90s")).To(Succeed())
	DeferCleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cleanupCancel()
		_, _ = run(cleanupCtx, "kind", "delete", "cluster", "--name", clusterName())
	})

	Expect(runChecked(ctx, "kind", "load", "docker-image", operatorImage(), "--name", clusterName())).To(Succeed())
	if e2eDisconnected {
		Expect(ensureDisconnectedBootstrapImage(ctx)).To(Succeed())
	}
	Expect(ensureNamespace(ctx, e2eNamespace)).To(Succeed())
	Expect(ensurePullSecret(ctx)).To(Succeed())

	switch e2eTLSMode {
	case helmTLSManual:
		manualTLS, err = newManualTLSFixture(e2eNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(probeMissingManualSecret(ctx)).To(Succeed())
		Expect(probeMalformedManualSecret(ctx)).To(Succeed())
		Expect(applyManualTLSSecret(ctx, e2eNamespace, "kubernaut-operator-webhook-cert")).To(Succeed())
	case helmTLSCertManager:
		Expect(installCertManager(ctx)).To(Succeed())
		Expect(ensureCertManagerIssuer(ctx)).To(Succeed())
	}

	Expect(helmInstall(ctx)).To(Succeed())
	if e2eMetrics {
		Expect(ensureMetricsReaderBinding(ctx)).To(Succeed())
	}
})

var _ = Describe("operator-only Helm lifecycle", Ordered, func() {
	It("starts the manager and publishes the singleton webhook CA", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		Eventually(func(g Gomega) {
			output, err := run(
				ctx, "kubectl", "--context", e2eContext, "get", "deployment",
				"kubernaut-operator-controller-manager", "-n", e2eNamespace,
				"-o", "jsonpath={.status.availableReplicas}",
			)
			g.Expect(err).NotTo(HaveOccurred(), output)
			g.Expect(strings.TrimSpace(output)).To(Equal("1"))
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		output, err := run(
			ctx, "kubectl", "--context", e2eContext, "get", "validatingwebhookconfiguration",
			"kubernaut-operator-singleton", "-o",
			"jsonpath={.webhooks[0].clientConfig.caBundle}:{.webhooks[0].failurePolicy}:{.webhooks[0].rules[0].scope}",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		parts := strings.Split(strings.TrimSpace(output), ":")
		Expect(parts).To(HaveLen(3))
		Expect(parts[0]).NotTo(BeEmpty())
		Expect(parts[1]).To(Equal("Fail"))
		Expect(parts[2]).To(Equal("Namespaced"))
		secretMaterial, err := run(
			ctx, "kubectl", "--context", e2eContext, "get", "secret",
			"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
			"-o", "jsonpath={.data.ca\\.crt}:{.data.tls\\.crt}:{.data.tls\\.key}",
		)
		Expect(err).NotTo(HaveOccurred(), secretMaterial)
		materialParts := strings.Split(strings.TrimSpace(secretMaterial), ":")
		Expect(materialParts).To(HaveLen(3))
		for _, material := range materialParts {
			Expect(material).NotTo(BeEmpty(), "webhook Secret must contain complete TLS material")
		}
		Expect(materialParts[0]).To(Equal(parts[0]),
			"the fail-closed webhook must publish the CA from its serving Secret")

		output, err = run(
			ctx, "kubectl", "--context", e2eContext, "get", "deployment",
			"kubernaut-operator-controller-manager", "-n", e2eNamespace,
			"-o", "jsonpath={.spec.template.spec.imagePullSecrets[0].name}",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(strings.TrimSpace(output)).To(Equal(helmPullSecretName))
		if e2eDisconnected {
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "deployment",
				"kubernaut-operator-controller-manager", "-n", e2eNamespace,
				"-o", "jsonpath={.spec.template.spec.initContainers}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(BeEmpty(),
				"the disconnected certificate image must run only in the restricted bootstrap Job")
		}

		if e2eTLSMode == helmTLSDevelopment {
			output, err := run(
				ctx, "kubectl", "--context", e2eContext, "get", "secret",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].kind}:{.metadata.ownerReferences[0].name}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(Equal(
				"Deployment:kubernaut-operator-controller-manager"),
				"development serving material must follow only the manager lifecycle")

			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "serviceaccount",
				"kubernaut-operator-cert-bootstrap", "-n", e2eNamespace,
			)
			Expect(err).To(HaveOccurred(), output)
			Expect(output).To(ContainSubstring("NotFound"))

			patchIdentity := "system:serviceaccount:" + e2eNamespace + ":kubernaut-operator-cert-patch"
			checks := []struct {
				args        []string
				expected    string
				description string
			}{
				{
					args:        []string{"get", "secrets", "-n", e2eNamespace},
					expected:    "no",
					description: "certificate patcher must not list or read arbitrary Secrets",
				},
				{
					args:        []string{"get", "deployments", "-n", e2eNamespace},
					expected:    "no",
					description: "certificate patcher must not read manager workloads",
				},
				{
					args:        []string{"get", "deployment/kubernaut-operator-controller-manager", "-n", e2eNamespace},
					expected:    "yes",
					description: "certificate patcher must read only the named manager Deployment",
				},
				{
					args:        []string{"get", "secret/kubernaut-operator-webhook-cert", "-n", e2eNamespace},
					expected:    "yes",
					description: "certificate patcher must read only the named serving Secret",
				},
				{
					args:        []string{"get", "validatingwebhookconfigurations"},
					expected:    "no",
					description: "certificate patcher must not read arbitrary webhook configurations",
				},
				{
					args:        []string{"get", "validatingwebhookconfiguration/kubernaut-operator-singleton"},
					expected:    "yes",
					description: "certificate patcher must read only the named webhook configuration",
				},
			}
			for _, check := range checks {
				can, canErr := run(ctx, "kubectl", append([]string{
					"--context", e2eContext, "auth", "can-i",
					"--as=" + patchIdentity,
				}, check.args...)...)
				if check.expected == "yes" {
					Expect(canErr).NotTo(HaveOccurred(), check.description)
				}
				Expect(authzResult(can)).To(Equal(check.expected), check.description)
			}
		}

		switch e2eTLSMode {
		case helmTLSManual:
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "secret",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
				"-o", "jsonpath={.metadata.annotations.meta\\.helm\\.sh/release-name}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(BeEmpty(), "manual TLS Secret must remain administrator-owned")
		case helmTLSCertManager:
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "certificate",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
				"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(Equal("True"))
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "secret",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].kind}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(Equal("Certificate"))
		}

		if e2eMetrics {
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "service",
				"kubernaut-operator-metrics", "-n", e2eNamespace,
				"-o", "jsonpath={.spec.ports[0].port}",
			)
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(Equal("8443"))
			metrics, metricsErr := scrapeMetrics(ctx)
			Expect(metricsErr).NotTo(HaveOccurred())
			Expect(metrics).To(ContainSubstring("# HELP"))
		}
	})

	It("rejects a competing Helm release before adopting cluster-scoped resources", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		output, err := run(
			ctx, helmBinary(), "--kube-context", e2eContext, "install", "kubernaut-operator-second",
			chartPath(), "--namespace", "kubernaut-helm-e2e-second", "--create-namespace",
			"--set", "webhook.tls.mode=manual", "--set", "webhook.tls.existingSecret=precreated",
			"--set", "webhook.tls.caBundle=Y2E=", "--wait=false",
		)
		Expect(err).To(HaveOccurred(), output)
		Expect(output).To(Or(ContainSubstring("invalid ownership metadata"), ContainSubstring("already exists")))
		Expect(runChecked(
			ctx, "kubectl", "--context", e2eContext, "delete", "namespace",
			"kubernaut-helm-e2e-second", "--ignore-not-found=true",
		)).To(Succeed())
	})

	It("upgrades in place without rotating a reusable development certificate", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		before, err := run(
			ctx, "kubectl", "--context", e2eContext, "get", "validatingwebhookconfiguration",
			"kubernaut-operator-singleton", "-o", "jsonpath={.webhooks[0].clientConfig.caBundle}",
		)
		Expect(err).NotTo(HaveOccurred(), before)
		Expect(strings.TrimSpace(before)).NotTo(BeEmpty())
		var rotatedSecretHash string
		if e2eTLSMode == helmTLSManual {
			Expect(applyManualTLSSecret(ctx, e2eNamespace, "kubernaut-operator-webhook-cert")).To(Succeed())
			rotatedSecretHash, err = secretCertificateHash(ctx, e2eNamespace)
			Expect(err).NotTo(HaveOccurred())
		}

		output, err := run(
			ctx, helmBinary(), "--kube-context", e2eContext, "upgrade", "kubernaut-operator",
			chartPath(), "--namespace", e2eNamespace, "--reuse-values",
			"--set", "podAnnotations.issue489Upgrade=enabled", "--wait", "--timeout", "10m",
		)
		Expect(err).NotTo(HaveOccurred(), output)

		after, err := run(
			ctx, "kubectl", "--context", e2eContext, "get", "validatingwebhookconfiguration",
			"kubernaut-operator-singleton", "-o", "jsonpath={.webhooks[0].clientConfig.caBundle}",
		)
		Expect(err).NotTo(HaveOccurred(), after)
		switch e2eTLSMode {
		case helmTLSDevelopment, helmTLSManual:
			Expect(strings.TrimSpace(after)).To(Equal(strings.TrimSpace(before)))
			if e2eTLSMode == helmTLSManual {
				currentHash, hashErr := secretCertificateHash(ctx, e2eNamespace)
				Expect(hashErr).NotTo(HaveOccurred())
				Expect(currentHash).To(Equal(rotatedSecretHash), "Helm must not overwrite administrator TLS rotation")
			}
		case helmTLSCertManager:
			beforeHash, hashErr := secretCertificateHash(ctx, e2eNamespace)
			Expect(hashErr).NotTo(HaveOccurred())
			Expect(runChecked(
				ctx, "kubectl", "--context", e2eContext, "delete", "secret",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace,
			)).To(Succeed())
			Eventually(func(g Gomega) {
				currentHash, currentErr := secretCertificateHash(ctx, e2eNamespace)
				g.Expect(currentErr).NotTo(HaveOccurred())
				g.Expect(currentHash).NotTo(Equal(beforeHash))
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
			Eventually(func(g Gomega) {
				ca, caErr := webhookCA(ctx)
				g.Expect(caErr).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(ca)).NotTo(BeEmpty())
				g.Expect(strings.TrimSpace(ca)).NotTo(Equal(strings.TrimSpace(before)),
					"cert-manager CA injection must publish the reissued certificate authority")
			}).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		}
	})

	It("upgrades and rolls back the retained CRD schema", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		baselineRevision, err := helmRevision(ctx)
		Expect(err).NotTo(HaveOccurred())
		variant, cleanup, err := chartVariant("issue489-upgrade")
		Expect(err).NotTo(HaveOccurred())
		defer cleanup()

		output, err := run(
			ctx, helmBinary(), "--kube-context", e2eContext, "upgrade", "kubernaut-operator",
			variant, "--namespace", e2eNamespace, "--reuse-values", "--wait", "--timeout", "10m",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(crdSchemaMarker(ctx)).To(Equal("issue489-upgrade"))
		Expect(crdSchemaProperty(ctx, "issue489UpgradeMarker")).To(Equal("string"))

		output, err = run(
			ctx, helmBinary(), "--kube-context", e2eContext, "rollback", "kubernaut-operator",
			strconv.Itoa(baselineRevision), "--namespace", e2eNamespace, "--wait", "--timeout", "10m",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(crdSchemaMarker(ctx)).To(BeEmpty())
		Expect(crdSchemaProperty(ctx, "issue489UpgradeMarker")).To(BeEmpty())
		Expect(crdResourcePolicy(ctx)).To(Equal("keep"))
	})

	It("recovers from a failed manager rollout by rolling back Helm", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		goodRevision, err := helmRevision(ctx)
		Expect(err).NotTo(HaveOccurred())
		output, err := run(
			ctx, helmBinary(), "--kube-context", e2eContext, "upgrade", "kubernaut-operator",
			chartPath(), "--namespace", e2eNamespace, "--reuse-values",
			"--set-string", "image.repository=registry.invalid/kubernaut/operator",
			"--set-string", "image.tag=does-not-exist", "--set", "image.digest=",
			"--wait", "--timeout", "45s",
		)
		Expect(err).To(HaveOccurred(), output)

		output, err = run(
			ctx, helmBinary(), "--kube-context", e2eContext, "rollback", "kubernaut-operator",
			strconv.Itoa(goodRevision), "--namespace", e2eNamespace, "--wait", "--timeout", "10m",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Eventually(func(g Gomega) {
			available, availableErr := run(
				ctx, "kubectl", "--context", e2eContext, "get", "deployment",
				"kubernaut-operator-controller-manager", "-n", e2eNamespace,
				"-o", "jsonpath={.status.availableReplicas}",
			)
			g.Expect(availableErr).NotTo(HaveOccurred(), available)
			g.Expect(strings.TrimSpace(available)).To(Equal("1"))
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	})

	It("retains an operand CR and CRD across uninstall and resumes after reinstall", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		Expect(applySampleCR(ctx)).To(Succeed())
		Expect(applyOperandSentinel(ctx)).To(Succeed())
		Expect(setCRFinalizer(ctx)).To(Succeed())
		output, err := run(
			ctx, "kubectl", "--context", e2eContext, "get", "kubernaut", "kubernaut",
			"-n", e2eNamespace, "-o", "name",
		)
		Expect(err).NotTo(HaveOccurred(), output)

		Expect(runChecked(
			ctx, helmBinary(), "--kube-context", e2eContext, "uninstall", "kubernaut-operator",
			"--namespace", e2eNamespace, "--wait", "--timeout", "5m",
		)).To(Succeed())

		output, err = run(
			ctx, "kubectl", "--context", e2eContext, "get", "crd", "kubernauts.kubernaut.ai",
			"-o", "jsonpath={.metadata.annotations.helm\\.sh/resource-policy}",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(strings.TrimSpace(output)).To(Equal("keep"))
		output, err = run(
			ctx, "kubectl", "--context", e2eContext, "get", "kubernaut", "kubernaut",
			"-n", e2eNamespace, "-o", "name",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		finalizers, err := getCRFinalizers(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(finalizers).To(ContainElement("test.kubernaut.ai/retain"))
		output, err = run(
			ctx, "kubectl", "--context", e2eContext, "get", "configmap", "operand-sentinel",
			"-n", e2eNamespace, "-o", "name",
		)
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(strings.TrimSpace(output)).To(Equal("configmap/operand-sentinel"))

		resources := []string{
			"deployment/kubernaut-operator-controller-manager",
			"validatingwebhookconfiguration/kubernaut-operator-singleton",
		}
		for _, resource := range resources {
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", resource, "-n", e2eNamespace,
			)
			Expect(err).To(HaveOccurred(), output)
			Expect(output).To(ContainSubstring("NotFound"), output)
		}
		if e2eTLSMode == helmTLSDevelopment {
			Eventually(func(g Gomega) {
				output, err = run(
					ctx, "kubectl", "--context", e2eContext, "get",
					"secret/kubernaut-operator-webhook-cert", "-n", e2eNamespace,
				)
				g.Expect(err).To(HaveOccurred(), output)
				g.Expect(output).To(ContainSubstring("NotFound"), output)
			}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		}
		if e2eTLSMode == helmTLSManual || e2eTLSMode == helmTLSCertManager {
			output, err = run(
				ctx, "kubectl", "--context", e2eContext, "get", "secret",
				"kubernaut-operator-webhook-cert", "-n", e2eNamespace, "-o", "name",
			)
			Expect(err).NotTo(HaveOccurred(), output)
		}

		Expect(helmInstall(ctx)).To(Succeed())
		Eventually(func(g Gomega) {
			output, err := run(
				ctx, "kubectl", "--context", e2eContext, "get", "deployment",
				"kubernaut-operator-controller-manager", "-n", e2eNamespace,
				"-o", "jsonpath={.status.availableReplicas}",
			)
			g.Expect(err).NotTo(HaveOccurred(), output)
			g.Expect(strings.TrimSpace(output)).To(Equal("1"))
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		finalizers, err = getCRFinalizers(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(finalizers).To(ContainElement("test.kubernaut.ai/retain"))
	})
})

func helmInstall(ctx context.Context) error {
	imageArgs, err := imageValues(operatorImage())
	if err != nil {
		return err
	}
	profileValues := helmProfileValues()
	args := make([]string, 0, 8+len(imageArgs)+len(profileValues))
	args = append(args,
		"--kube-context", e2eContext,
		"install", "kubernaut-operator", chartPath(),
		"--namespace", e2eNamespace,
		"--create-namespace",
		"--wait", "--timeout", "10m",
	)
	args = append(args, imageArgs...)
	args = append(args, profileValues...)
	_, err = run(ctx, helmBinary(), args...)
	return err
}

func tlsModeFromEnvironment() (string, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("KUBERNAUT_HELM_E2E_TLS_PROFILE")))
	if value == "" {
		return helmTLSDevelopment, nil
	}
	switch value {
	case helmTLSDevelopment, helmTLSManual, helmTLSCertManager:
		return value, nil
	case "cert-manager":
		return helmTLSCertManager, nil
	default:
		return "", fmt.Errorf("invalid KUBERNAUT_HELM_E2E_TLS_PROFILE %q", value)
	}
}

func helmBinary() string {
	if value := strings.TrimSpace(os.Getenv("HELM_BIN")); value != "" {
		return value
	}
	return "helm"
}

func helmProfileValues() []string {
	values := []string{
		"--set-string", "image.pullSecrets[0].name=" + helmPullSecretName,
	}
	switch e2eTLSMode {
	case helmTLSManual:
		values = append(values,
			"--set", "webhook.tls.mode=manual",
			"--set-string", "webhook.tls.existingSecret=kubernaut-operator-webhook-cert",
			"--set-string", "webhook.tls.caBundle="+base64.StdEncoding.EncodeToString(manualTLS.caPEM),
		)
	case helmTLSCertManager:
		values = append(values,
			"--set", "webhook.tls.mode=certManager",
			"--set-string", "webhook.tls.certManager.issuerRef.name="+helmCertManagerName,
			"--set", "webhook.tls.certManager.issuerRef.kind=Issuer",
		)
	default:
		values = append(values, "--set", "webhook.tls.mode=development")
	}
	if e2eMetrics {
		values = append(values, "--set", "metrics.enabled=true")
	}
	if e2eDisconnected {
		values = append(values,
			"--set-string", "webhook.tls.development.image.repository=localhost/bitnami/kubectl",
			"--set-string", "webhook.tls.development.image.tag=issue489",
			"--set", "webhook.tls.development.image.digest=",
		)
	}
	return values
}

func ensureNamespace(ctx context.Context, namespace string) error {
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)
	return applyManifest(ctx, manifest)
}

func ensurePullSecret(ctx context.Context) error {
	dockerConfig := fmt.Sprintf(`{"auths":{"registry.invalid":{"username":"issue489","password":"issue489","auth":"%s"}}}`,
		base64.StdEncoding.EncodeToString([]byte("issue489:issue489")),
	)
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: %s
`, helmPullSecretName, e2eNamespace, base64.StdEncoding.EncodeToString([]byte(dockerConfig)))
	return applyManifest(ctx, manifest)
}

func ensureDisconnectedBootstrapImage(ctx context.Context) error {
	containerTool := strings.TrimSpace(os.Getenv("KUBERNAUT_HELM_E2E_CONTAINER_TOOL"))
	if containerTool == "" {
		containerTool = "docker"
	}
	if _, err := run(ctx, containerTool, "pull", helmCertBootstrapSourceImage); err != nil {
		return fmt.Errorf("pulling the pinned bootstrap source image: %w", err)
	}
	if _, err := run(ctx, containerTool, "tag", helmCertBootstrapSourceImage, helmCertBootstrapLocalImage); err != nil {
		return fmt.Errorf("tagging the disconnected bootstrap image: %w", err)
	}
	if _, err := run(ctx, "kind", "load", "docker-image", helmCertBootstrapLocalImage,
		"--name", clusterName()); err != nil {
		return fmt.Errorf("loading the disconnected bootstrap image into Kind: %w", err)
	}
	return nil
}

func installCertManager(ctx context.Context) error {
	if _, err := run(ctx, "kubectl", "--context", e2eContext, "apply", "-f", helmCertManagerManifest); err != nil {
		return fmt.Errorf("installing cert-manager %s: %w", helmCertManagerVersion, err)
	}
	if _, err := run(ctx, "kubectl", "--context", e2eContext, "patch", "deployment/cert-manager", "-n", "cert-manager",
		"--type=json", "-p", helmCertManagerOwnerReferencePatch); err != nil {
		return fmt.Errorf("enabling cert-manager Secret owner references: %w", err)
	}
	for _, deployment := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		if _, err := run(ctx, "kubectl", "--context", e2eContext, "rollout", "status",
			"deployment/"+deployment, "-n", "cert-manager", "--timeout=10m"); err != nil {
			return fmt.Errorf("waiting for cert-manager deployment %s: %w", deployment, err)
		}
	}
	for _, crd := range []string{
		"issuers.cert-manager.io", "certificates.cert-manager.io", "clusterissuers.cert-manager.io",
	} {
		if _, err := run(ctx, "kubectl", "--context", e2eContext, "wait", "--for=condition=Established",
			"crd/"+crd, "--timeout=5m"); err != nil {
			return fmt.Errorf("waiting for cert-manager CRD %s: %w", crd, err)
		}
	}
	return nil
}

func ensureCertManagerIssuer(ctx context.Context) error {
	manifest := fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: %s
  namespace: %s
spec:
  selfSigned: {}
`, helmCertManagerName, e2eNamespace)
	return applyManifest(ctx, manifest)
}

func ensureMetricsReaderBinding(ctx context.Context) error {
	manifest := fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubernaut-operator-metrics-reader
subjects:
  - kind: ServiceAccount
    name: kubernaut-operator-controller-manager
    namespace: %s
`, helmMetricsBinding, e2eNamespace)
	return applyManifest(ctx, manifest)
}

func newManualTLSFixture(namespace string) (*manualTLSFixture, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating manual TLS CA key: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "issue-489 manual CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating manual TLS CA certificate: %w", err)
	}
	return &manualTLSFixture{
		namespace: namespace,
		caCert:    caTemplate,
		caKey:     caKey,
		caPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		serial:    1,
	}, nil
}

func (fixture *manualTLSFixture) servingCertificate() ([]byte, []byte, error) {
	fixture.serial++
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating manual TLS serving key: %w", err)
	}
	serviceName := "kubernaut-operator-webhook"
	certificateTemplate := &x509.Certificate{
		SerialNumber: fixture.serialNumber(),
		Subject:      pkix.Name{CommonName: serviceName + "." + fixture.namespace + ".svc"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames: []string{
			serviceName + "." + fixture.namespace + ".svc",
			serviceName + "." + fixture.namespace + ".svc.cluster.local",
		},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader, certificateTemplate, fixture.caCert, &key.PublicKey, fixture.caKey,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating manual TLS serving certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
}

func (fixture *manualTLSFixture) serialNumber() *big.Int {
	return big.NewInt(fixture.serial)
}

func applyManualTLSSecret(ctx context.Context, namespace, secretName string) error {
	if manualTLS == nil {
		return fmt.Errorf("manual TLS fixture has not been initialized")
	}
	certificate, key, err := manualTLS.servingCertificate()
	if err != nil {
		return err
	}
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/tls
data:
  ca.crt: %s
  tls.crt: %s
  tls.key: %s
`, secretName, namespace,
		base64.StdEncoding.EncodeToString(manualTLS.caPEM),
		base64.StdEncoding.EncodeToString(certificate),
		base64.StdEncoding.EncodeToString(key))
	return applyManifest(ctx, manifest)
}

func probeMissingManualSecret(ctx context.Context) error {
	values := []string{
		"--set", "webhook.tls.mode=manual",
		"--set-string", "webhook.tls.existingSecret=issue489-missing-webhook-cert",
		"--set-string", "webhook.tls.caBundle=" + base64.StdEncoding.EncodeToString(manualTLS.caPEM),
		"--set-string", "image.pullSecrets[0].name=" + helmPullSecretName,
	}
	args := make([]string, 0, 8+len(values))
	args = append(args,
		"--kube-context", e2eContext, "install", "kubernaut-operator", chartPath(),
		"--namespace", e2eNamespace, "--wait=false",
	)
	args = append(args, values...)
	if output, err := run(ctx, helmBinary(), args...); err != nil {
		return fmt.Errorf("installing missing-secret manual probe: %s: %w", output, err)
	}
	defer func() {
		_, _ = run(ctx, helmBinary(), "--kube-context", e2eContext, "uninstall", "kubernaut-operator",
			"--namespace", e2eNamespace, "--wait", "--timeout", "5m")
	}()
	if _, err := run(ctx, "kubectl", "--context", e2eContext, "get", "secret",
		"issue489-missing-webhook-cert", "-n", e2eNamespace); err == nil {
		return fmt.Errorf("manual profile unexpectedly created the missing TLS Secret")
	}
	available, err := run(ctx, "kubectl", "--context", e2eContext, "get", "deployment",
		"kubernaut-operator-controller-manager", "-n", e2eNamespace,
		"-o", "jsonpath={.status.availableReplicas}")
	if err != nil {
		return fmt.Errorf("checking missing-secret deployment: %s: %w", available, err)
	}
	if strings.TrimSpace(available) != "" {
		return fmt.Errorf("manual profile became ready without its TLS Secret: %s", available)
	}
	return nil
}

func probeMalformedManualSecret(ctx context.Context) error {
	invalidSecret := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: issue489-malformed-webhook-cert
  namespace: %s
type: kubernetes.io/tls
data:
  ca.crt: %s
  tls.crt: %s
  tls.key: %s
`, e2eNamespace,
		base64.StdEncoding.EncodeToString(manualTLS.caPEM),
		base64.StdEncoding.EncodeToString([]byte("not-a-certificate")),
		base64.StdEncoding.EncodeToString([]byte("not-a-private-key")))
	if err := applyManifest(ctx, invalidSecret); err != nil {
		return fmt.Errorf("creating malformed manual TLS Secret: %w", err)
	}
	defer func() {
		_, _ = run(ctx, helmBinary(), "--kube-context", e2eContext, "uninstall", "kubernaut-operator",
			"--namespace", e2eNamespace, "--wait", "--timeout", "5m")
		_, _ = run(ctx, "kubectl", "--context", e2eContext, "delete", "secret",
			"issue489-malformed-webhook-cert", "-n", e2eNamespace, "--ignore-not-found=true")
	}()
	values := []string{
		"--set", "webhook.tls.mode=manual",
		"--set-string", "webhook.tls.existingSecret=issue489-malformed-webhook-cert",
		"--set-string", "webhook.tls.caBundle=" + base64.StdEncoding.EncodeToString(manualTLS.caPEM),
		"--set-string", "image.pullSecrets[0].name=" + helmPullSecretName,
	}
	args := make([]string, 0, 8+len(values))
	args = append(args,
		"--kube-context", e2eContext, "install", "kubernaut-operator", chartPath(),
		"--namespace", e2eNamespace, "--wait=false",
	)
	args = append(args, values...)
	if output, err := run(ctx, helmBinary(), args...); err != nil {
		return fmt.Errorf("installing malformed-secret manual probe: %s: %w", output, err)
	}
	available, err := run(ctx, "kubectl", "--context", e2eContext, "get", "deployment",
		"kubernaut-operator-controller-manager", "-n", e2eNamespace,
		"-o", "jsonpath={.status.availableReplicas}")
	if err != nil {
		return fmt.Errorf("checking malformed-secret deployment: %s: %w", available, err)
	}
	if strings.TrimSpace(available) != "" {
		return fmt.Errorf("manual profile became ready with malformed TLS material: %s", available)
	}
	return nil
}

func scrapeMetrics(ctx context.Context) (string, error) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("allocating metrics port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("releasing metrics port: %w", err)
	}
	token, err := run(ctx, "kubectl", "--context", e2eContext, "create", "token",
		"kubernaut-operator-controller-manager", "-n", e2eNamespace)
	if err != nil {
		return "", fmt.Errorf("creating metrics token: %w", err)
	}
	portForward := exec.CommandContext(ctx, "kubectl", "--context", e2eContext, "-n", e2eNamespace,
		"port-forward", "service/kubernaut-operator-metrics", fmt.Sprintf("%d:8443", port),
		"--address", "127.0.0.1")
	portForward.Stdout = io.Discard
	portForward.Stderr = io.Discard
	if err := portForward.Start(); err != nil {
		return "", fmt.Errorf("starting metrics port-forward: %w", err)
	}
	defer func() {
		_ = portForward.Process.Kill()
		_, _ = portForward.Process.Wait()
	}()

	tlsConfig := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // local Kind port-forward
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://127.0.0.1:%d/metrics", port), nil)
	if err != nil {
		return "", fmt.Errorf("creating metrics request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	var responseBody string
	Eventually(func(g Gomega) {
		response, requestErr := client.Do(request)
		if requestErr != nil {
			g.Expect(requestErr).NotTo(HaveOccurred())
			return
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		g.Expect(readErr).NotTo(HaveOccurred())
		g.Expect(response.StatusCode).To(Equal(http.StatusOK), string(body))
		responseBody = string(body)
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	return responseBody, nil
}

func secretField(ctx context.Context, namespace, secretName, field string) (string, error) {
	jsonPath := "{.data." + strings.ReplaceAll(field, ".", `\.`) + "}"
	return run(ctx, "kubectl", "--context", e2eContext, "get", "secret", secretName,
		"-n", namespace, "-o", "jsonpath="+jsonPath)
}

func secretCertificateHash(ctx context.Context, namespace string) (string, error) {
	encoded, err := secretField(ctx, namespace, "kubernaut-operator-webhook-cert", "tls.crt")
	if err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decoding webhook certificate: %w", err)
	}
	digest := sha256.Sum256(decoded)
	return fmt.Sprintf("%x", digest[:]), nil
}

func webhookCA(ctx context.Context) (string, error) {
	return run(ctx, "kubectl", "--context", e2eContext, "get", "validatingwebhookconfiguration",
		"kubernaut-operator-singleton", "-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
}

func authzResult(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func chartVariant(marker string) (string, func(), error) {
	temporaryRoot, err := os.MkdirTemp("", "kubernaut-helm-chart-variant-")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating Helm chart variant directory: %w", err)
	}
	variant := filepath.Join(temporaryRoot, "kubernaut-operator")
	if err := filepath.Walk(chartPath(), func(sourcePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(chartPath(), sourcePath)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(variant, relative)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		contents, err := os.ReadFile(sourcePath) //nolint:gosec // filepath.Walk constrains this to the chart tree.
		if err != nil {
			return err
		}
		if relative == filepath.Join("files", "kubernaut.ai_kubernauts.yaml") {
			annotation := "    controller-gen.kubebuilder.io/version: v0.18.0\n"
			annotation += "    issue489.kubernaut.ai/schema-marker: " + marker + "\n"
			contents = []byte(strings.Replace(string(contents),
				"    controller-gen.kubebuilder.io/version: v0.18.0\n", annotation, 1))
			upgradeProperty := `            properties:
              issue489UpgradeMarker:
                description: Helm upgrade qualification marker.
                type: string
              additionalClusterRoles:`
			contents = []byte(strings.Replace(string(contents),
				"            properties:\n              additionalClusterRoles:", upgradeProperty, 1))
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			return err
		}
		//nolint:gosec // targetPath stays beneath the temporary chart variant
		return os.WriteFile(targetPath, contents, info.Mode().Perm())
	}); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", func() {}, fmt.Errorf("copying Helm chart variant: %w", err)
	}
	return variant, func() { _ = os.RemoveAll(temporaryRoot) }, nil
}

func helmRevision(ctx context.Context) (int, error) {
	output, err := run(ctx, helmBinary(), "--kube-context", e2eContext, "status", "kubernaut-operator",
		"--namespace", e2eNamespace, "-o", "json")
	if err != nil {
		return 0, err
	}
	var status struct {
		Version int `json:"version"`
	}
	jsonOutput := strings.TrimSpace(output)
	if start := strings.IndexByte(jsonOutput, '{'); start >= 0 {
		jsonOutput = jsonOutput[start:]
	}
	if err := json.Unmarshal([]byte(jsonOutput), &status); err != nil {
		return 0, fmt.Errorf("decoding Helm release status: %w", err)
	}
	return status.Version, nil
}

func crdResourcePolicy(ctx context.Context) (string, error) {
	return run(ctx, "kubectl", "--context", e2eContext, "get", "crd", "kubernauts.kubernaut.ai",
		"-o", "jsonpath={.metadata.annotations.helm\\.sh/resource-policy}")
}

type crdSchema struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties map[string]struct {
						Properties map[string]struct {
							Type string `json:"type"`
						} `json:"properties"`
					} `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

func getCRDSchema(ctx context.Context) (crdSchema, error) {
	output, err := run(ctx, "kubectl", "--context", e2eContext, "get", "crd", "kubernauts.kubernaut.ai", "-o", "json")
	if err != nil {
		return crdSchema{}, err
	}
	var object crdSchema
	if err := json.Unmarshal([]byte(output), &object); err != nil {
		return crdSchema{}, fmt.Errorf("decoding Kubernaut CRD: %w", err)
	}
	return object, nil
}

func crdSchemaMarker(ctx context.Context) string {
	object, err := getCRDSchema(ctx)
	if err != nil || len(object.Spec.Versions) == 0 {
		return ""
	}
	return object.Metadata.Annotations["issue489.kubernaut.ai/schema-marker"]
}

func crdSchemaProperty(ctx context.Context, property string) string {
	object, err := getCRDSchema(ctx)
	if err != nil || len(object.Spec.Versions) == 0 {
		return ""
	}
	return object.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties[property].Type
}

func applySampleCR(ctx context.Context) error {
	samplePath := filepath.Join(repositoryRoot(), "config", "samples", "v1alpha2_kubernaut.yaml")
	sample, err := os.ReadFile(samplePath) //nolint:gosec // path is fixed beneath the repository root
	if err != nil {
		return fmt.Errorf("reading Kubernaut sample: %w", err)
	}
	manifest := strings.ReplaceAll(string(sample), "namespace: kubernaut-system", "namespace: "+e2eNamespace)
	return applyManifest(ctx, manifest)
}

func applyOperandSentinel(ctx context.Context) error {
	return applyManifest(ctx, `apiVersion: v1
kind: ConfigMap
metadata:
  name: operand-sentinel
  namespace: kubernaut-helm-e2e
  labels:
    app.kubernetes.io/part-of: kubernaut
data:
  lifecycle: retained
`)
}

func setCRFinalizer(ctx context.Context) error {
	output, err := run(
		ctx, "kubectl", "--context", e2eContext, "get", "kubernaut", "kubernaut",
		"-n", e2eNamespace, "-o", "json",
	)
	if err != nil {
		return err
	}
	var object struct {
		Metadata struct {
			Finalizers []string `json:"finalizers"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(output), &object); err != nil {
		return fmt.Errorf("decoding Kubernaut finalizers: %w", err)
	}
	for _, finalizer := range object.Metadata.Finalizers {
		if finalizer == "test.kubernaut.ai/retain" {
			return nil
		}
	}
	object.Metadata.Finalizers = append(object.Metadata.Finalizers, "test.kubernaut.ai/retain")
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"finalizers": object.Metadata.Finalizers}})
	if err != nil {
		return fmt.Errorf("encoding Kubernaut finalizer patch: %w", err)
	}
	_, err = run(
		ctx, "kubectl", "--context", e2eContext, "patch", "kubernaut", "kubernaut",
		"-n", e2eNamespace, "--type=merge", "-p", string(patch),
	)
	return err
}

func getCRFinalizers(ctx context.Context) ([]string, error) {
	output, err := run(
		ctx, "kubectl", "--context", e2eContext, "get", "kubernaut", "kubernaut",
		"-n", e2eNamespace, "-o", "json",
	)
	if err != nil {
		return nil, err
	}
	var object struct {
		Metadata struct {
			Finalizers []string `json:"finalizers"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(output), &object); err != nil {
		return nil, fmt.Errorf("decoding Kubernaut finalizers: %w", err)
	}
	return object.Metadata.Finalizers, nil
}

func applyManifest(ctx context.Context, manifest string) error {
	command := exec.CommandContext(ctx, "kubectl", "--context", e2eContext, "apply", "-f", "-")
	command.Stdin = strings.NewReader(manifest)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("applying manifest: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func imageValues(image string) ([]string, error) {
	if at := strings.LastIndex(image, "@"); at > strings.LastIndex(image, "/") {
		return []string{"--set", "image.repository=" + image[:at], "--set", "image.digest=" + image[at+1:]}, nil
	}
	colon := strings.LastIndex(image, ":")
	if colon <= strings.LastIndex(image, "/") {
		return nil, fmt.Errorf("operator image %q must include a tag or digest", image)
	}
	return []string{"--set", "image.repository=" + image[:colon], "--set", "image.tag=" + image[colon+1:]}, nil
}

func run(ctx context.Context, command string, args ...string) (string, error) {
	//nolint:gosec // test commands and arguments are intentionally supplied by the E2E harness
	process := exec.CommandContext(ctx, command, args...)
	output, err := process.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf(
			"%s %s: %s: %w", command, strings.Join(args, " "), strings.TrimSpace(string(output)), err,
		)
	}
	return string(output), nil
}

func runChecked(ctx context.Context, command string, args ...string) error {
	_, err := run(ctx, command, args...)
	return err
}

func clusterName() string {
	if value := strings.TrimSpace(os.Getenv("KUBERNAUT_HELM_E2E_CLUSTER")); value != "" {
		return value
	}
	return "kubernaut-operator-helm-e2e"
}

func operatorImage() string {
	if value := strings.TrimSpace(os.Getenv("KUBERNAUT_OPERATOR_IMAGE")); value != "" {
		return value
	}
	return "localhost/kubernaut-operator:ci"
}

func chartPath() string {
	return filepath.Join(repositoryRoot(), "charts", "kubernaut-operator")
}

func repositoryRoot() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
}
