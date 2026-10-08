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

package kind

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const (
	spireChartURL        = "https://github.com/spiffe/helm-charts/releases/download/spire-0.13.0/spire-0.13.0.tgz"
	spireChartSHA256     = "f0178ef5f50a7d69fc532bdf60f79eaf211110c420ae00105115338d0f33126a"
	spireChartVersion    = "0.13.0"
	spireServerVersion   = "1.7.2"
	spireNamespace       = "spire-system"
	spireTrustDomain     = "kubernaut.test"
	spireServerSA        = "spire-qualification-server"
	spireBadClientSA     = "spire-qualification-bad-client"
	spireServerName      = "spire-qualification-server"
	spireBadClientName   = "spire-qualification-bad-client"
	spireServerIDName    = "kubernaut-spire-qualification-server"
	spireBadIDName       = "kubernaut-spire-qualification-bad-client"
	spireServerConfig    = "spire-qualification-server-config"
	spireClientConfig    = "spire-qualification-client-config"
	spireBadConfig       = "spire-qualification-bad-client-config"
	spireClientName      = "spire-identity-client"
	spireCSIName         = "spiffe-workload-api"
	spireCertsName       = "spire-certs"
	spireConfigVolume    = "spire-helper-config"
	spireCertsDirectory  = "/certs"
	spireSocketDirectory = "/spiffe-workload-api"
)

var configuredSPIRE bool

func spireEnabledFromEnvironment() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_SPIRE"))) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("invalid KUBERNAUT_E2E_SPIRE value; expected true or false")
	}
}

func spireProbeImage() string {
	if image := strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_SPIRE_PROBE_IMAGE")); image != "" {
		return image
	}
	return "localhost/kubernaut-operator-e2e-spire-probe:" + kubernautOperatorVersion()
}

func kubernautOperatorVersion() string {
	if version := strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_OPERATOR_VERSION")); version != "" {
		return version
	}
	return "1.6.0-rc20"
}

func installSPIRE(ctx context.Context) error {
	tmpDir, err := os.MkdirTemp("", "kubernaut-spire-chart-")
	if err != nil {
		return fmt.Errorf("creating SPIRE chart directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // qualification scratch directory

	chartPath := filepath.Join(tmpDir, "spire.tgz")
	if _, err := runCmd(ctx, "curl", "-fsSL", spireChartURL, "-o", chartPath); err != nil {
		return fmt.Errorf("downloading SPIRE chart %s: %w", spireChartVersion, err)
	}
	chart, err := os.ReadFile(chartPath) //nolint:gosec // chartPath is a private, freshly-created qualification file
	if err != nil {
		return fmt.Errorf("reading downloaded SPIRE chart: %w", err)
	}
	digest := sha256.Sum256(chart)
	if actual := hex.EncodeToString(digest[:]); actual != spireChartSHA256 {
		return fmt.Errorf("SPIRE chart checksum mismatch: got %s, want %s", actual, spireChartSHA256)
	}

	args := []string{
		"upgrade", "--install", "spire", chartPath,
		"--namespace", spireNamespace, "--create-namespace",
		"--set", "global.spire.clusterName=" + kindClusterName(),
		"--set", "global.spire.trustDomain=" + spireTrustDomain,
		"--set", "spire-server.image.tag=" + spireServerVersion,
		"--set", "spire-agent.image.tag=" + spireServerVersion,
		"--set", "spire-server.controllerManager.image.tag=0.2.3",
		"--set", "spiffe-csi-driver.image.tag=0.2.3",
		// Short TTL makes the renewal assertion deterministic in the opt-in lane.
		"--set", "spire-server.defaultX509SvidTTL=90s",
		"--set", "spire-server.defaultJwtSvidTTL=90s",
		"--wait", "--timeout", "15m",
	}
	if _, err := helmInCluster(ctx, args...); err != nil {
		return fmt.Errorf("installing SPIRE chart %s (server %s): %w", spireChartVersion, spireServerVersion, err)
	}
	if _, err := kubectl(
		ctx, "wait", "--for=condition=Established", "crd/clusterspiffeids.spire.spiffe.io", "--timeout=15m",
	); err != nil {
		return fmt.Errorf("waiting for SPIRE ClusterSPIFFEID CRD: %w", err)
	}
	for _, resource := range []string{
		"statefulset/spire-server",
		"daemonset/spire-agent",
		"daemonset/spire-spiffe-csi-driver",
	} {
		if _, err := kubectl(
			ctx, "rollout", "status", resource, "-n", spireNamespace, "--timeout=15m",
		); err != nil {
			return fmt.Errorf("waiting for SPIRE %s: %w", resource, err)
		}
	}
	return nil
}

func spiffeID(serviceAccount string) string {
	return fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", spireTrustDomain, kubernautNamespace, serviceAccount)
}

func qualificationClusterSPIFFEID(name, serviceAccount, component string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "spire.spiffe.io/v1alpha1",
			"kind":       "ClusterSPIFFEID",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"qualification.kubernaut.ai/issue": "479",
				},
			},
			"spec": map[string]interface{}{
				"spiffeIDTemplate": spiffeID(serviceAccount),
				"podSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app.kubernetes.io/component": component,
					},
				},
				"namespaceSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"kubernetes.io/metadata.name": kubernautNamespace,
					},
				},
			},
		},
	}
	return obj
}

func qualificationServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kubernautNamespace,
			Labels: map[string]string{
				"qualification.kubernaut.ai/issue": "479",
			},
		},
	}
}

func qualificationHelperConfig(name, mode, self, peer, address string, once bool) *corev1.ConfigMap {
	args := strings.Join(qualificationProbeArgs(mode, self, peer, address, !once), " ")
	helperConfig := fmt.Sprintf(`agent_address = %q
cert_dir = %q
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "svid_bundle.pem"
daemon_mode = %t
cert_file_mode = 0444
key_file_mode = 0444
`, spireSocketDirectory+"/spire-agent.sock", spireCertsDirectory, !once)
	if !once {
		helperConfig += fmt.Sprintf(`cmd = "/usr/local/bin/spire-probe"
cmd_args = %q
renew_signal = "SIGUSR1"
`, args)
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kubernautNamespace,
			Labels: map[string]string{
				"qualification.kubernaut.ai/issue": "479",
			},
		},
		Data: map[string]string{"helper.conf": helperConfig},
	}
}

func qualificationProbeArgs(mode, self, peer, address string, daemon bool) []string {
	args := []string{
		"-mode=" + mode,
		"-cert-dir=" + spireCertsDirectory,
		"-expected-self=" + self,
		"-expected-peer=" + peer,
	}
	if mode == "server" {
		args = append(args, "-listen=:8443")
	} else {
		args = append(args, "-address="+address)
		if daemon {
			args = append(args, "-interval=5s")
		} else {
			args = append(args, "-once")
		}
	}
	return args
}

func qualificationVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: spireCSIName, MountPath: spireSocketDirectory, ReadOnly: true},
		{Name: spireCertsName, MountPath: spireCertsDirectory},
		{Name: spireConfigVolume, MountPath: "/etc/spiffe-helper", ReadOnly: true},
	}
}

func qualificationVolumes(configName string) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: spireCSIName,
			VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{
				Driver:   "csi.spiffe.io",
				ReadOnly: ptr.To(true),
			}},
		},
		{Name: spireCertsName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{
			Name: spireConfigVolume,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configName},
			}},
		},
	}
}

func qualificationProbeContainer() corev1.Container {
	return corev1.Container{
		Name:            "spire-probe",
		Image:           spireProbeImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/spiffe-helper"},
		Args:            []string{"-config", "/etc/spiffe-helper/helper.conf"},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(false),
			RunAsUser:                ptr.To(int64(0)),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
		},
		VolumeMounts: qualificationVolumeMounts(),
	}
}

func qualificationClientProbeContainer() corev1.Container {
	container := qualificationProbeContainer()
	container.Name = spireClientName
	return container
}

func qualificationDirectProbeContainer(mode, self, peer, address string) corev1.Container {
	container := qualificationProbeContainer()
	container.Command = []string{"/usr/local/bin/spire-probe"}
	container.Args = qualificationProbeArgs(mode, self, peer, address, false)
	return container
}

func qualificationServerDeployment() *appsv1.Deployment {
	labels := map[string]string{
		"app.kubernetes.io/name":           spireServerName,
		"app.kubernetes.io/component":      spireServerName,
		"qualification.kubernaut.ai/issue": "479",
	}
	replicas := int32(1)
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      spireServerName,
			Namespace: kubernautNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": spireServerName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName: spireServerSA,
					Containers:         []corev1.Container{qualificationProbeContainer()},
					Volumes:            qualificationVolumes(spireServerConfig),
				},
			},
		},
	}
}

func qualificationServerService() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      spireServerName,
			Namespace: kubernautNamespace,
			Labels:    map[string]string{"qualification.kubernaut.ai/issue": "479"},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app.kubernetes.io/name": spireServerName},
			Ports: []corev1.ServicePort{{
				Name: "https", Port: 8443, TargetPort: intstr.FromInt(8443),
			}},
		},
	}
}

func ensureSPIREQualification(ctx context.Context) error {
	serverID := spiffeID(spireServerSA)
	afID := spiffeID(resources.ServiceAccountName(resources.ComponentAPIFrontend))
	badID := spiffeID(spireBadClientSA)

	objects := []interface{}{
		qualificationServiceAccount(spireServerSA),
		qualificationServiceAccount(spireBadClientSA),
		qualificationHelperConfig(spireServerConfig, "server", serverID, afID, "", false),
		qualificationHelperConfig(spireClientConfig, "client", afID, serverID, spireServerName+":8443", false),
		qualificationHelperConfig(spireBadConfig, "client", badID, serverID, spireServerName+":8443", true),
		qualificationClusterSPIFFEID(spireServerIDName, spireServerSA, spireServerName),
		qualificationClusterSPIFFEID(spireBadIDName, spireBadClientSA, "spire-qualification-bad-client"),
		qualificationServerDeployment(),
		qualificationServerService(),
	}
	if err := applyYAML(ctx, objects...); err != nil {
		return fmt.Errorf("applying SPIRE qualification fixtures: %w", err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/"+spireServerName, "-n", kubernautNamespace, "--timeout=5m",
	); err != nil {
		return fmt.Errorf("waiting for SPIRE qualification server: %w", err)
	}
	if err := waitForPodLog(
		ctx, kubernautNamespace, "app.kubernetes.io/name="+spireServerName, "spire-probe", "mTLS server ready",
	); err != nil {
		return fmt.Errorf("waiting for SPIRE qualification server identity: %w", err)
	}

	if err := patchAPIFrontendWithSPIREClient(ctx, spireClientConfig); err != nil {
		return err
	}
	if err := waitForPodLog(
		ctx, kubernautNamespace, "app.kubernetes.io/component=apifrontend", spireClientName, "mTLS handshake succeeded",
	); err != nil {
		return fmt.Errorf("qualifying API Frontend SVID and mTLS client: %w", err)
	}
	if err := waitForPodLog(
		ctx, kubernautNamespace, "app.kubernetes.io/component=apifrontend", spireClientName, "SVID serial rotated",
	); err != nil {
		return fmt.Errorf("qualifying SVID rotation: %w", err)
	}

	if err := runRejectedSPIREClient(ctx, spireBadConfig); err != nil {
		return err
	}
	if err := assertNoOptionalKagentiResources(ctx); err != nil {
		return err
	}
	if _, err := kubectl(ctx, "get", "clusterspiffeids.spire.spiffe.io", "kubernaut-apifrontend"); err != nil {
		return fmt.Errorf("operator SPIFFE registration was not present before cleanup: %w", err)
	}

	if _, err := kubectl(ctx, "patch", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace,
		"--type=merge", "-p", `{"spec":{"apiFrontend":{"spire":{"enabled":false}}}}`); err != nil {
		return fmt.Errorf("disabling SPIRE registration: %w", err)
	}
	if err := waitForNotFound(ctx, "clusterspiffeids.spire.spiffe.io", "kubernaut-apifrontend"); err != nil {
		return fmt.Errorf("waiting for operator SPIFFE registration cleanup: %w", err)
	}
	if _, err := kubectl(ctx, "get", "clusterspiffeids.spire.spiffe.io", spireServerIDName); err != nil {
		return fmt.Errorf("operator cleanup removed qualification-owned SPIFFE registration: %w", err)
	}
	return nil
}

func patchAPIFrontendWithSPIREClient(ctx context.Context, configName string) error {
	containerJSON, err := json.Marshal(qualificationClientProbeContainer())
	if err != nil {
		return fmt.Errorf("marshalling SPIRE client container: %w", err)
	}
	patchOps := make([]map[string]interface{}, 0, len(qualificationVolumes(configName))+1)
	for _, volume := range qualificationVolumes(configName) {
		patchOps = append(patchOps, map[string]interface{}{
			"op":    "add",
			"path":  "/spec/template/spec/volumes/-",
			"value": volume,
		})
	}
	patchOps = append(patchOps, map[string]interface{}{
		"op":    "add",
		"path":  "/spec/template/spec/containers/-",
		"value": json.RawMessage(containerJSON),
	})
	patch, err := json.Marshal(patchOps)
	if err != nil {
		return fmt.Errorf("marshalling API Frontend SPIRE patch: %w", err)
	}
	if _, err := kubectl(ctx, "patch", "deployment", resources.DeploymentName(resources.ComponentAPIFrontend),
		"-n", kubernautNamespace, "--type=json", "-p", string(patch)); err != nil {
		return fmt.Errorf("adding qualification CSI client to API Frontend: %w", err)
	}
	if _, err := kubectl(ctx, "rollout", "status", "deployment/"+resources.DeploymentName(resources.ComponentAPIFrontend),
		"-n", kubernautNamespace, "--timeout=5m"); err != nil {
		return fmt.Errorf("waiting for API Frontend qualification rollout: %w", err)
	}
	return nil
}

func runRejectedSPIREClient(ctx context.Context, configName string) error {
	badID := spiffeID(spireBadClientSA)
	serverID := spiffeID(spireServerSA)
	helper := qualificationProbeContainer()
	helper.Name = "spire-helper"
	badPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      spireBadClientName,
			Namespace: kubernautNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":           spireBadClientName,
				"app.kubernetes.io/component":      "spire-qualification-bad-client",
				"qualification.kubernaut.ai/issue": "479",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:            spireBadClientSA,
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: ptr.To(int64(5)),
			InitContainers:                []corev1.Container{helper},
			Containers: []corev1.Container{qualificationDirectProbeContainer(
				"client", badID, serverID, spireServerName+":8443",
			)},
			Volumes: qualificationVolumes(configName),
		},
	}
	if err := applyYAML(ctx, badPod); err != nil {
		return fmt.Errorf("applying rejected SPIRE client: %w", err)
	}
	if err := waitForPodPhase(ctx, spireBadClientName, corev1.PodFailed); err != nil {
		return fmt.Errorf("waiting for wrong-identity mTLS rejection: %w", err)
	}
	output, err := kubectl(ctx, "logs", "pod/"+spireBadClientName, "-n", kubernautNamespace, "-c", "spire-probe")
	if err != nil {
		return fmt.Errorf("reading wrong-identity mTLS logs: %w", err)
	}
	if !strings.Contains(output, "does not include") && !strings.Contains(output, "bad certificate") {
		return fmt.Errorf("wrong-identity mTLS client failed without an identity rejection: %s", strings.TrimSpace(output))
	}
	return nil
}

func waitForPodLog(ctx context.Context, namespace, selector, container, text string) error {
	return pollUntilSuccess(ctx, 10*time.Minute, 5*time.Second, func() error {
		output, err := kubectl(ctx, "get", "pods", "-n", namespace, "-l", selector,
			"-o", "jsonpath={.items[0].metadata.name}")
		if err != nil {
			return err
		}
		podName := strings.TrimSpace(output)
		if podName == "" {
			return fmt.Errorf("no pod matches %q", selector)
		}
		logs, err := kubectl(ctx, "logs", "pod/"+podName, "-n", namespace, "-c", container, "--tail=100")
		if err != nil {
			return err
		}
		if !strings.Contains(logs, text) {
			return fmt.Errorf("pod %s has not logged %q", podName, text)
		}
		return nil
	})
}

func waitForPodPhase(ctx context.Context, name string, phase corev1.PodPhase) error {
	return pollUntilSuccess(ctx, 5*time.Minute, 3*time.Second, func() error {
		output, err := kubectl(ctx, "get", "pod", name, "-n", kubernautNamespace,
			"-o", "jsonpath={.status.phase}")
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) != string(phase) {
			return fmt.Errorf("pod %s phase is %q, want %s", name, strings.TrimSpace(output), phase)
		}
		return nil
	})
}

func waitForNotFound(ctx context.Context, resource, name string) error {
	return pollUntilSuccess(ctx, 5*time.Minute, 3*time.Second, func() error {
		output, err := kubectl(ctx, "get", resource, name)
		if err == nil {
			return fmt.Errorf("%s %s still exists", resource, name)
		}
		if strings.Contains(strings.ToLower(output), "not found") {
			return nil
		}
		return fmt.Errorf("checking %s %s: %w", resource, name, err)
	})
}

func assertNoOptionalKagentiResources(ctx context.Context) error {
	output, err := kubectl(ctx, "get", "deployment", resources.DeploymentName(resources.ComponentAPIFrontend),
		"-n", kubernautNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("reading API Frontend metadata for optional-resource assertion: %w", err)
	}
	lower := strings.ToLower(output)
	for _, forbidden := range []string{"kagenti", "rossctl", "authbridge", "agent.kagenti.dev"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("API Frontend deployment contains forbidden optional integration reference %q", forbidden)
		}
	}
	return nil
}

func deleteSPIREQualificationWorkloads(ctx context.Context) error {
	for _, resource := range []string{
		"pod/" + spireBadClientName,
		"deployment/" + spireServerName,
		"service/" + spireServerName,
		"serviceaccount/" + spireServerSA,
		"serviceaccount/" + spireBadClientSA,
		"configmap/" + spireServerConfig,
		"configmap/" + spireClientConfig,
		"configmap/" + spireBadConfig,
		"clusterspiffeids.spire.spiffe.io/" + spireServerIDName,
		"clusterspiffeids.spire.spiffe.io/" + spireBadIDName,
	} {
		if _, err := kubectl(
			ctx, "delete", resource, "-n", kubernautNamespace, "--ignore-not-found=true", "--wait=false",
		); err != nil {
			return fmt.Errorf("deleting SPIRE qualification resource %s: %w", resource, err)
		}
	}
	return nil
}
