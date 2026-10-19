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
	"fmt"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	postgresImage = "docker.io/library/postgres:16.4"
	valkeyImage   = "docker.io/valkey/valkey:8.0.2"
	// This is a disposable Secret name, not credential material.
	defaultInternalCASecretName = "kubernaut-internal-ca" //nolint:gosec
)

// ensureKubernautInfrastructure installs only disposable dependency and input
// fixtures. The managed application image references are replaced with the
// local contract image; no upstream Kubernaut image is deployed by this suite.
func ensureKubernautInfrastructure(ctx context.Context) error {
	if err := ensureNamespace(ctx, kubernautNamespace); err != nil {
		return err
	}
	args := []string{
		"upgrade", "--install", "kubernaut-dev-bootstrap", devBootstrapChartPath(),
		"--namespace", kubernautNamespace,
		"--create-namespace",
		"--set", "postgresql.commandMode=official",
		"--set", "postgresql.securityContext.enabled=false",
		"--set", "valkey.securityContext.enabled=false",
		"--set-string", "postgresql.image.repository=docker.io/library/postgres",
		"--set-string", "postgresql.image.tag=16.4",
		"--set-string", "postgresql.image.digest=sha256:e62fbf9d3e2b49816a32c400ed2dba83e3b361e6833e624024309c35d334b412",
		"--wait", "--timeout", "10m",
	}
	if _, err := helmInCluster(ctx, args...); err != nil {
		return fmt.Errorf("installing Kubernaut development bootstrap chart: %w", err)
	}
	return nil
}

func devBootstrapChartPath() string {
	return filepath.Join(repositoryRoot(), "charts", "kubernaut-dev-bootstrap")
}

// ensureCertManagerRuntimeTLS creates the cert-manager objects consumed by
// the CertManager TLS source. The operator only reads the resulting Secrets;
// cert-manager remains the owner and rotation authority for all certificates.
func ensureCertManagerRuntimeTLS(ctx context.Context) error {
	if _, err := kubectlStdin(ctx, certManagerBootstrapIssuerManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying cert-manager bootstrap issuer: %w", err)
	}
	if _, err := kubectlStdin(ctx, certManagerRootCertificateManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying cert-manager root certificate: %w", err)
	}
	if err := waitForCertificate(ctx, defaultInternalCASecretName); err != nil {
		return err
	}
	if err := waitForSecretTLSMaterial(ctx, defaultInternalCASecretName); err != nil {
		return err
	}

	if _, err := kubectlStdin(ctx, certManagerRuntimeIssuerManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying cert-manager runtime issuer: %w", err)
	}
	if _, err := kubectlStdin(ctx, certManagerLeafCertificatesManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying cert-manager runtime certificates: %w", err)
	}
	for _, name := range certManagerRuntimeCertificateNames {
		if err := waitForCertificate(ctx, name); err != nil {
			return err
		}
		if err := waitForSecretTLSMaterial(ctx, certManagerSecretName(name)); err != nil {
			return err
		}
	}
	return nil
}

func waitForCertificate(ctx context.Context, name string) error {
	if _, err := kubectl(
		ctx, "wait", "--for=condition=Ready", "certificate/"+name,
		"-n", kubernautNamespace, "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for cert-manager certificate %s: %w", name, err)
	}
	return nil
}

func waitForSecretTLSMaterial(ctx context.Context, name string) error {
	err := pollUntilSuccess(ctx, 5*time.Minute, 2*time.Second, func() error {
		output, err := kubectl(
			ctx, "get", "secret", name, "-n", kubernautNamespace,
			"-o", "jsonpath={.data.tls\\.crt}:{.data.tls\\.key}",
		)
		if err != nil {
			return err
		}
		parts := strings.Split(strings.TrimSpace(output), ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("secret %s does not yet contain tls.crt and tls.key", name)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("waiting for cert-manager secret %s: %w", name, err)
	}
	return nil
}

func certManagerSecretName(certificateName string) string {
	if certificateName == defaultInternalCASecretName {
		return certificateName
	}
	return certificateName + "-tls"
}

const certManagerBootstrapIssuerManifest = `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: kubernaut-bootstrap
  namespace: kubernaut-system
spec:
  selfSigned: {}
`

const certManagerRootCertificateManifest = `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kubernaut-internal-ca
  namespace: kubernaut-system
spec:
  isCA: true
  commonName: kubernaut-internal-ca
  secretName: kubernaut-internal-ca
  duration: 8760h
  renewBefore: 720h
  privateKey:
    algorithm: RSA
    size: 2048
  issuerRef:
    name: kubernaut-bootstrap
    kind: Issuer
    group: cert-manager.io
`

const certManagerRuntimeIssuerManifest = `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: kubernaut-ca
  namespace: kubernaut-system
spec:
  ca:
    secretName: kubernaut-internal-ca
`

const certManagerLeafCertificatesManifest = `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: gateway
  namespace: kubernaut-system
spec:
  secretName: gateway-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
  - gateway-service
  - gateway-service.kubernaut-system
  - gateway-service.kubernaut-system.svc
  - gateway-service.kubernaut-system.svc.cluster.local
  issuerRef:
    name: kubernaut-ca
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: datastorage
  namespace: kubernaut-system
spec:
  secretName: datastorage-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
  - data-storage-service
  - data-storage-service.kubernaut-system
  - data-storage-service.kubernaut-system.svc
  - data-storage-service.kubernaut-system.svc.cluster.local
  issuerRef:
    name: kubernaut-ca
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kubernautagent
  namespace: kubernaut-system
spec:
  secretName: kubernautagent-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
  - kubernaut-agent
  - kubernaut-agent.kubernaut-system
  - kubernaut-agent.kubernaut-system.svc
  - kubernaut-agent.kubernaut-system.svc.cluster.local
  issuerRef:
    name: kubernaut-ca
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: apifrontend
  namespace: kubernaut-system
spec:
  secretName: apifrontend-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
  - apifrontend
  - apifrontend.kubernaut-system
  - apifrontend.kubernaut-system.svc
  - apifrontend.kubernaut-system.svc.cluster.local
  issuerRef:
    name: kubernaut-ca
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: authwebhook
  namespace: kubernaut-system
spec:
  secretName: authwebhook-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
  - authwebhook-service
  - authwebhook-service.kubernaut-system
  - authwebhook-service.kubernaut-system.svc
  - authwebhook-service.kubernaut-system.svc.cluster.local
  issuerRef:
    name: kubernaut-ca
    kind: Issuer
    group: cert-manager.io
`

var certManagerRuntimeCertificateNames = []string{
	"gateway",
	"datastorage",
	"kubernautagent",
	"apifrontend",
	"authwebhook",
}

func ensureNamespace(ctx context.Context, namespace string) error {
	output, err := kubectl(ctx, "create", "namespace", namespace)
	if err != nil && !strings.Contains(output, "AlreadyExists") {
		return fmt.Errorf("creating namespace %s: %w", namespace, err)
	}
	return nil
}

func applyKubernautCR(ctx context.Context) error {
	return pollUntilSuccess(ctx, 2*time.Minute, 2*time.Second, func() error {
		if err := applyYAML(ctx, kubernautCR()); err != nil {
			return fmt.Errorf("creating Kubernaut CR: %w", err)
		}
		return nil
	})
}

func kubernautCR() *kubernautv1alpha2.Kubernaut {
	tlsMode, err := TLSModeForSource(string(configuredTLS), string(activeManualTLSSelection))
	if err != nil {
		panic(fmt.Sprintf("resolving configured TLS fixture mode: %v", err))
	}
	tls := kubernautv1alpha2.TLSConfigSpec{Mode: tlsMode}
	dataStorage := kubernautv1alpha2.DataStorageSpec{}
	monitoring := kubernautv1alpha2.MonitoringSpec{}
	switch configuredTLS {
	case tlsDevelopment:
		tls.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
	case tlsHook:
		tls.Hooks = &kubernautv1alpha2.TLSHooksConfig{TLSCerts: kubernautv1alpha2.TLSCertsConfig{
			ExtraSANs: []string{"localhost"},
		}}
	case tlsManualAdmin:
		tls = manualAdminTLSConfig(activeManualTLSSelection)
		prefix := string(manualTLSSelectionManual)
		if activeManualTLSSelection == manualTLSSelectionAdmin {
			prefix = manualTLSAdminPrefix
		}
		dataStorage.Audit = &kubernautv1alpha2.DataStorageAuditSpec{
			SigningCertificate: &kubernautv1alpha2.SigningCertSpec{
				SecretName: manualTLSSigningSecretName(prefix),
			},
		}
	case tlsCertManager:
		tls = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{ //nolint:gosec // disposable Kind fixture Secret references
				IssuerRef: kubernautv1alpha2.TLSIssuerRef{
					Name:  "kubernaut-ca",
					Kind:  "Issuer",
					Group: "cert-manager.io",
				},
				InternalCASecretName: defaultInternalCASecretName,
				ServiceTLSSecretNames: map[string]string{
					"gateway":        "gateway-tls",
					"datastorage":    "datastorage-tls",
					"kubernautagent": "kubernautagent-tls",
					"apifrontend":    "apifrontend-tls",
					"authwebhook":    "authwebhook-tls",
				},
			},
		}
	}
	apiFrontend := kubernautv1alpha2.APIFrontendSpec{Enabled: ptr.To(false)}
	return &kubernautv1alpha2.Kubernaut{
		TypeMeta:   metav1.TypeMeta{APIVersion: kubernautv1alpha2.GroupVersion.String(), Kind: "Kubernaut"},
		ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha2.SingletonName, Namespace: kubernautNamespace},
		Spec: kubernautv1alpha2.KubernautSpec{
			Image: kubernautv1alpha2.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
				Overrides:  ContractImageOverrides(contractImage()),
			},
			PostgreSQL: kubernautv1alpha2.PostgreSQLSpec{ //nolint:gosec // disposable Kind fixture secret reference
				SecretName: "postgresql-secret",
				Host:       "postgresql.kubernaut-system.svc.cluster.local",
				SSLMode:    "require",
			},
			Valkey: kubernautv1alpha2.ValkeySpec{
				SecretName: "valkey-secret",
				Host:       "valkey.kubernaut-system.svc.cluster.local",
			},
			DataStorage: dataStorage,
			AIAnalysis: kubernautv1alpha2.AIAnalysisSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "aianalysis-policy"},
			},
			SignalProcessing: kubernautv1alpha2.SignalProcessingSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
			Monitoring: monitoring,
			LLMProfiles: map[string]kubernautv1alpha2.LLMProfileSpec{
				"primary": {
					Provider:              "openai",
					Model:                 "gpt-4o",
					Endpoint:              "http://llm.invalid",
					CredentialsSecretName: "llm-credentials",
				},
			},
			KubernautAgent: kubernautv1alpha2.KubernautAgentSpec{
				Investigation: kubernautv1alpha2.InvestigationSpec{LLMProfileRef: "primary"},
			},
			Gateway:         kubernautv1alpha2.GatewaySpec{Enabled: ptr.To(false)},
			APIFrontend:     apiFrontend,
			TLS:             tls,
			NetworkPolicies: kubernautv1alpha2.NetworkPoliciesSpec{},
		},
	}
}

func manualAdminTLSConfig(selection manualTLSSelection) kubernautv1alpha2.TLSConfigSpec {
	mode, err := TLSModeForSource(string(tlsManualAdmin), string(selection))
	if err != nil {
		panic(fmt.Sprintf("resolving manual/admin TLS fixture mode: %v", err))
	}
	prefix := string(manualTLSSelectionManual)
	if selection == manualTLSSelectionAdmin {
		prefix = manualTLSAdminPrefix
	}
	config := &kubernautv1alpha2.AdministratorManagedTLSConfig{
		ServiceTLSSecretNames: manualTLSServiceSecretNames(prefix),
	}
	if selection == manualTLSSelectionAdmin {
		config.InternalCASecretName = manualTLSCASecretName(prefix)
	}
	return kubernautv1alpha2.TLSConfigSpec{
		Mode:                 mode,
		AdministratorManaged: config,
	}
}

// ContractImageOverrides returns a complete image override map for the
// operator-only Kind harness. Keeping every resolved image on one local
// fixture prevents an accidental fallback to RELATED_IMAGE_* or an upstream
// Kubernaut registry image.
func ContractImageOverrides(image string) map[string]string {
	imageNames := []string{
		"gateway", "datastorage", "aianalysis", "signalprocessing", "remediationorchestrator",
		"workflowexecution", "effectivenessmonitor", "notification", "kubernautagent", "authwebhook",
		"apifrontend", "db-migrate", "console", "fleetmetadatacache", "init-ubi-minimal", "oauth2-proxy",
	}
	overrides := make(map[string]string, len(imageNames))
	for _, imageName := range imageNames {
		overrides[imageName] = image
	}
	return overrides
}

func kubernautCondition(ctx context.Context, condition string) (string, error) {
	output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace,
		"-o", fmt.Sprintf(`jsonpath={.status.conditions[?(@.type=="%s")].status}`, condition))
	return strings.TrimSpace(output), err
}

func kubernautPhase(ctx context.Context) (string, error) {
	output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace,
		"-o", "jsonpath={.status.phase}")
	return strings.TrimSpace(output), err
}

func secretTLSMaterialPresent(ctx context.Context, name string) (bool, error) {
	jsonPath := "jsonpath={.data.tls\\.crt}:{.data.tls\\.key}"
	expectsPair := true
	if name == defaultInternalCASecretName && configuredTLS != tlsCertManager {
		jsonPath = "jsonpath={.data.ca\\.crt}:{.data.ca\\.key}"
	}
	if configuredTLS == tlsManualAdmin {
		fixture, err := activeManualTLSFixture()
		if err != nil {
			return false, err
		}
		if activeManualTLSSelection == manualTLSSelectionManual {
			output, getErr := kubectl(
				ctx, "get", "configmap", fixture.caConfigMapName, "-n", kubernautNamespace,
				"-o", "jsonpath={.data.ca\\.crt}",
			)
			return strings.TrimSpace(output) != "", getErr
		}
		name = fixture.caSecretName
		jsonPath = "jsonpath={.data.ca\\.crt}"
		expectsPair = false
	}
	output, err := kubectl(
		ctx, "get", "secret", name, "-n", kubernautNamespace,
		"-o", jsonPath,
	)
	if err != nil {
		return false, err
	}
	if !expectsPair {
		return strings.TrimSpace(output) != "", nil
	}
	parts := strings.Split(strings.TrimSpace(output), ":")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "", nil
}

func selectedTrustBundlePresent(ctx context.Context) (bool, error) {
	if configuredTLS == tlsManualAdmin && activeManualTLSSelection == manualTLSSelectionManual {
		fixture, err := activeManualTLSFixture()
		if err != nil {
			return false, err
		}
		output, err := kubectl(
			ctx, "get", "configmap", fixture.caConfigMapName, "-n", kubernautNamespace,
			"-o", "jsonpath={.data.ca\\.crt}",
		)
		return strings.TrimSpace(output) != "", err
	}
	output, err := kubectl(
		ctx, "get", "configmap", "inter-service-trust-bundle", "-n", kubernautNamespace,
		"-o", "jsonpath={.data.ca\\.crt}",
	)
	return strings.TrimSpace(output) != "", err
}

func secretTLSCertificate(ctx context.Context, name string) (string, error) {
	output, err := kubectl(
		ctx, "get", "secret", name, "-n", kubernautNamespace,
		"-o", "jsonpath={.data.tls\\.crt}",
	)
	return strings.TrimSpace(output), err
}

func tlsProbeCAKey() (string, error) {
	if configuredTLS == tlsManualAdmin {
		if _, err := activeManualTLSFixture(); err != nil {
			return "", err
		}
	}
	return tlsCACertificateKey, nil
}

func hookTLSSecretNames() []string {
	return []string{
		defaultInternalCASecretName,
		"gateway-tls",
		"datastorage-tls",
		"kubernautagent-tls",
		"apifrontend-tls",
		"authwebhook-tls",
		"datastorage-signing-cert",
	}
}

func exerciseManualAdminAliases(ctx context.Context) error {
	if activeManualTLSSelection != manualTLSSelectionManual {
		return fmt.Errorf("manual/admin lane must start with the manual mode")
	}
	if err := verifyTLSWorkloadTrust(ctx, tlsCACertificateKey); err != nil {
		return err
	}
	if err := deleteKubernautCR(ctx); err != nil {
		return err
	}
	if err := assertManualTLSFixtureUnchanged(ctx, manualTLSSelectionManual); err != nil {
		return err
	}

	setActiveManualTLSSelection(manualTLSSelectionAdmin)
	if err := ensureManualTLSWebhookFixtures(ctx, manualTLSSelectionAdmin); err != nil {
		return err
	}
	if err := applyKubernautCR(ctx); err != nil {
		return err
	}
	if err := waitForContractReady(ctx); err != nil {
		return err
	}
	if err := assertManualTLSFixtureUnchanged(ctx, manualTLSSelectionAdmin); err != nil {
		return err
	}
	adminFixture, err := manualTLSFixtureFor(manualTLSSelectionAdmin)
	if err != nil {
		return err
	}
	if err := assertManualTLSWebhookBundlesUnchanged(ctx, adminFixture); err != nil {
		return err
	}
	if present, err := webhookOpenShiftCAInjectionPresent(ctx, "mutating"); err != nil || present {
		if err != nil {
			return err
		}
		return fmt.Errorf("administrator-managed mutating webhook uses OpenShift CA injection")
	}
	return verifyTLSWorkloadTrust(ctx, tlsCACertificateKey)
}

func completeMigrationJob(ctx context.Context) error {
	const migrationJobName = "kubernaut-db-migration"
	return pollUntilSuccess(ctx, 5*time.Minute, 2*time.Second, func() error {
		migrationCondition, err := kubernautCondition(ctx, "MigrationComplete")
		if err != nil {
			return fmt.Errorf("reading MigrationComplete condition: %w", err)
		}
		if migrationCondition == conditionTrue {
			return nil
		}
		if _, err := kubectl(ctx, "get", "job", migrationJobName, "-n", kubernautNamespace); err != nil {
			return fmt.Errorf("waiting for contract migration Job: %w", err)
		}
		completionTime := time.Now().UTC().Format(time.RFC3339)
		patch := fmt.Sprintf(
			`{
				"status": {
					"conditions": [
						{
							"type": "SuccessCriteriaMet",
							"status": "True",
							"reason": "ContractFixture",
							"message": "operator contract fixture completed migration"
						},
						{
							"type": "Complete",
							"status": "True",
							"reason": "ContractFixture",
							"message": "operator contract fixture completed migration"
						}
					],
					"succeeded": 1,
					"active": 0,
					"ready": 0,
					"completionTime": "%s"
				}
			}`,
			completionTime,
		)
		if output, err := kubectl(
			ctx, "patch", "job", migrationJobName, "-n", kubernautNamespace,
			"--subresource=status", "--type=merge", "-p", patch,
		); err != nil {
			return fmt.Errorf("completing contract migration Job: %w: %s", err, strings.TrimSpace(output))
		}
		return nil
	})
}

func waitForContractReady(ctx context.Context) error {
	if err := completeMigrationJob(ctx); err != nil {
		return err
	}
	if err := pollUntilSuccess(ctx, 10*time.Minute, 2*time.Second, func() error {
		if condition, err := kubernautCondition(ctx, "BYOValidated"); err != nil {
			return fmt.Errorf("reading BYOValidated condition: %w", err)
		} else if condition != conditionTrue {
			return fmt.Errorf("BYOValidated is not True: %s", condition)
		}
		if condition, err := kubernautCondition(ctx, "MigrationComplete"); err != nil {
			return fmt.Errorf("reading MigrationComplete condition: %w", err)
		} else if condition != conditionTrue {
			return fmt.Errorf("MigrationComplete is not True: %s", condition)
		}
		if condition, err := kubernautCondition(ctx, "ServicesDeployed"); err != nil {
			return fmt.Errorf("reading ServicesDeployed condition: %w", err)
		} else if condition != conditionTrue {
			return fmt.Errorf("ServicesDeployed is not True: %s", condition)
		}
		if condition, err := kubernautCondition(ctx, "TLSReady"); err != nil {
			return fmt.Errorf("reading TLSReady condition: %w", err)
		} else if condition != conditionTrue {
			return fmt.Errorf("TLSReady is not True: %s", condition)
		}
		phase, err := kubernautPhase(ctx)
		if err != nil {
			return fmt.Errorf("reading Kubernaut phase: %w", err)
		}
		if phase != string(kubernautv1alpha2.PhaseRunning) {
			return fmt.Errorf("kubernaut phase is not Running: %s", phase)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func deleteTLSSecret(ctx context.Context, name string) error {
	if _, err := kubectl(ctx, "delete", "secret", name, "-n", kubernautNamespace, "--ignore-not-found=true"); err != nil {
		return fmt.Errorf("deleting TLS Secret %q: %w", name, err)
	}
	return nil
}

func secretPresent(ctx context.Context, name string) (bool, error) {
	output, err := kubectl(ctx, "get", "secret", name, "-n", kubernautNamespace)
	if err != nil && (strings.Contains(output, "NotFound") || strings.Contains(output, "not found")) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func serviceHasNoPlaintextPort(ctx context.Context, name string) (bool, error) {
	output, err := kubectl(
		ctx, "get", "service", name, "-n", kubernautNamespace,
		"-o", `jsonpath={range .spec.ports[*]}{.name}{"\n"}{end}`,
	)
	if err != nil {
		return false, err
	}
	hasHTTPS := false
	for _, portName := range strings.Fields(output) {
		if portName == "http" {
			return false, nil
		}
		if portName == "https" {
			hasHTTPS = true
		}
	}
	return hasHTTPS, nil
}

func webhookOpenShiftCAInjectionPresent(ctx context.Context, webhookType string) (bool, error) {
	resource := webhookType + "webhookconfiguration"
	name := kubernautNamespace + "-authwebhook-" + webhookType
	output, err := kubectl(ctx, "get", resource, name, "-o", "json")
	if err != nil {
		return false, err
	}
	return strings.Contains(output, "service.beta.openshift.io/inject-cabundle"), nil
}

func patchCertificateForRotation(ctx context.Context, name string) error {
	patch := `{"spec":{"duration":"2h","renewBefore":"1h"}}`
	if _, err := kubectl(
		ctx, "patch", "certificate/"+name, "-n", kubernautNamespace,
		"--type=merge", "-p", patch,
	); err != nil {
		return fmt.Errorf("requesting cert-manager certificate rotation: %w", err)
	}
	return nil
}

func secretOwnerKind(ctx context.Context, name string) (string, error) {
	output, err := kubectl(
		ctx, "get", "secret", name, "-n", kubernautNamespace,
		"-o", "jsonpath={.metadata.ownerReferences[0].kind}",
	)
	return strings.TrimSpace(output), err
}

func webhookCABundlePresent(ctx context.Context, webhookType string) (bool, error) {
	resource := webhookType + "webhookconfiguration"
	name := kubernautNamespace + "-authwebhook-" + webhookType
	output, err := kubectl(
		ctx, "get", resource, name,
		"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}",
	)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func deleteKubernautCR(ctx context.Context) error {
	if _, err := kubectl(
		ctx, "delete", "kubernaut", kubernautv1alpha2.SingletonName,
		"-n", kubernautNamespace, "--ignore-not-found=true", "--wait=false",
	); err != nil {
		return fmt.Errorf("deleting Kubernaut CR: %w", err)
	}
	return pollUntilSuccess(ctx, 5*time.Minute, 2*time.Second, func() error {
		output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace)
		if err != nil && (strings.Contains(output, "NotFound") || strings.Contains(output, "not found")) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("checking Kubernaut CR deletion: %w", err)
		}
		return fmt.Errorf("kubernaut CR still exists")
	})
}
