package resources

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing/fstest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("independent resource-generation boundary outcomes", func() {
	It("UT-AUDIT-515-RESOURCES-001: normalizes source paths, explicit references and nullable issuer defaults", func() {
		kn := testKubernaut()
		kn.Spec.TLS.InterService = &kubernautv1alpha2.InterServiceTLSConfig{}
		Expect(InterServiceTLSCertDirFor(kn)).To(Equal("/etc/tls"))
		Expect(InterServiceTLSCAFileFor(kn)).To(Equal("/etc/tls-ca/ca.crt"))
		Expect(isHelmTLSMode(nil)).To(BeFalse())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
			InternalCASecretName: "administrator-ca", ServiceTLSSecretNames: map[string]string{TLSServiceGateway: "custom-gateway"},
		}
		Expect(InterServiceTLSCAVolume(kn).Secret.SecretName).To(Equal("administrator-ca"))
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, "custom-gateway"))
		kn.Spec.KubernautAgent.RuntimeConfigMapName = "administrator-runtime"
		Expect(KubernautAgentLLMRuntimeConfigName(kn)).To(Equal("administrator-runtime"))
		Expect(ResolveLLMProfile(kn, "")).To(Equal(kubernautv1alpha2.LLMProfileSpec{}))
		kn.Spec.AIAnalysis.Policy.ConfigMapName = ""
		kn.Spec.SignalProcessing.Policy.ConfigMapName = ""
		Expect(AIAnalysisPolicyName(kn)).To(Equal("aianalysis-policy"))
		Expect(SignalProcessingPolicyName(kn)).To(Equal("signalprocessing-policy"))
		Expect(EffectiveAlertManagerURL(kn)).NotTo(BeEmpty())
		Expect(isComponentActive(kn, kn, ComponentConsole)).To(BeFalse())
		var configuration *kubernautv1alpha2.CertManagerTLSConfig
		Expect(configuration.EffectiveIssuerRef()).To(Equal(kubernautv1alpha2.TLSIssuerRef{}))
	})

	It("UT-AUDIT-515-RESOURCES-002: honors signing overrides and emits bounded IP and DNS certificate SANs", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeCertManager
		Expect(CertManagerTLSProvisioningEnabled(nil)).To(BeFalse())
		Expect(DataStorageSigningSecretName(nil)).To(BeEmpty())
		_, err := CertManagerTLSResources(nil)
		Expect(err).To(HaveOccurred())
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{},
		}
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.InternalCASecretName).To(Equal("kubernaut-interservice-ca-secret"))
		Expect(DataStorageSigningSecretName(kn)).To(Equal("datastorage-signing-cert"))
		setDataStorageSigningCertificate(kn, &kubernautv1alpha2.SigningCertSpec{SecretName: "custom-signing", MountPath: "/custom"})
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{ExtraSANs: []string{" ", "127.0.0.1", "10.0.0.1", "extra.example"}},
		}
		Expect(DataStorageSigningSecretName(kn)).To(Equal("custom-signing"))
		objects, err := CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, object := range objects {
			if object.GetName() != GatewayTLSSecretName {
				continue
			}
			addresses, _, getErr := unstructured.NestedStringSlice(object.Object, "spec", "ipAddresses")
			Expect(getErr).NotTo(HaveOccurred())
			Expect(addresses).To(ConsistOf("127.0.0.1", "10.0.0.1"))
		}
		Expect(dataStorageServerConfig(kn).SignerCertDir).To(Equal("/custom"))
		Expect(firstNonEmpty("", "")).To(BeEmpty())
	})

	It("UT-AUDIT-515-RESOURCES-003: returns missing-image errors from deployment and migration builders", func() {
		kn := testKubernautWithAF()
		kn.Spec.WorkflowExecution.Ansible.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "aap-ca"}
		withoutImage("RELATED_IMAGE_INIT_UBI_MINIMAL")
		for _, build := range []func() error{
			func() error { _, err := WorkflowExecutionDeployment(kn, kn); return err },
			func() error { _, err := EffectivenessMonitorDeployment(kn, kn); return err },
			func() error { _, err := KubernautAgentDeployment(kn, kn); return err },
			func() error { _, err := APIFrontendDeployment(kn, kn); return err },
		} {
			Expect(build()).To(MatchError(ContainSubstring("no image found")))
		}
		withoutImage("RELATED_IMAGE_DB_MIGRATE")
		_, err := MigrationJob(kn)
		Expect(err).To(HaveOccurred())
		_, err = DataStorageDeployment(kn)
		Expect(err).To(HaveOccurred())
		withoutImage("RELATED_IMAGE_CONSOLE")
		_, err = ConsoleDeployment(kn, "")
		Expect(err).To(HaveOccurred())
		withoutImage("RELATED_IMAGE_API_FRONTEND")
		_, err = APIFrontendDeployment(kn, kn)
		Expect(err).To(HaveOccurred())
		_, err = buildDeployment(kn, DeploymentParams{ImageName: "unknown-image"})
		Expect(err).To(HaveOccurred())
		kn.Spec.LLMProfiles = nil
		_, err = KubernautAgentDeployment(kn, kn)
		Expect(err).To(MatchError(ContainSubstring("credentialsSecretName")))
	})

	It("UT-AUDIT-515-RESOURCES-004: renders requested ports, probe paths, mTLS credentials and platform metadata", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.MetricsPort = ptr.To[int32](9180)
		kn.Spec.APIFrontend.HealthPort = ptr.To[int32](8181)
		_, metricsPort, healthPort := apifrontendPorts(kn)
		Expect(metricsPort).To(Equal(int32(9180)))
		Expect(healthPort).To(Equal(int32(8181)))
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSClientSecretRef = "llm-client"
		volumes, mounts := kaCredentialVolumesAndMounts(kn, profile, nil, nil)
		Expect(volumes).To(ContainElement(HaveField("Name", "llm-tls-client")))
		Expect(mounts).To(ContainElement(HaveField("ReadOnly", true)))
		kn.Spec.Image.Overrides = map[string]string{"custom": "image"}
		deployment, err := buildDeployment(kn, DeploymentParams{Component: ComponentGateway, ImageName: "custom",
			LivenessPath: "/custom-live", ReadinessPath: "/custom-ready"})
		Expect(err).NotTo(HaveOccurred())
		container := deployment.Spec.Template.Spec.Containers[0]
		Expect(container.Ports[0].ContainerPort).To(Equal(int32(8443)))
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/custom-live"))
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/custom-ready"))
		kn.Spec.PostgreSQL.Host = unitLoopbackHost
		initContainer, err := dataStorageInitContainer(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(initContainer.Command).NotTo(BeEmpty())
		service := buildService(kn, serviceDefinition{Component: ComponentGateway, ServiceName: "custom",
			Annotations: map[string]string{"platform-key": "retained"}})
		Expect(service.Annotations).To(HaveKeyWithValue("platform-key", "retained"))
		roles := markCoreClusterRBAC([]*rbacv1.ClusterRole{{}})
		Expect(roles[0].Labels).NotTo(BeEmpty())
		labels := map[string]string{}
		Expect(EnsureRestrictedPSALabels(labels)).To(BeTrue())
		Expect(EnsureRestrictedPSALabels(labels)).To(BeFalse())
	})

	It("UT-AUDIT-515-RESOURCES-007: rejects missing primary and sidecar images independently of init-container resolution", func() {
		kn := testKubernautWithAF()
		withoutImage("RELATED_IMAGE_API_FRONTEND")
		_, err := APIFrontendDeployment(kn, kn)
		Expect(err).To(MatchError(ContainSubstring("apifrontend")))
		withoutImage("RELATED_IMAGE_OAUTH2_PROXY")
		_, err = ConsoleDeployment(testKubernautWithConsole(), "apps.example.com")
		Expect(err).To(MatchError(ContainSubstring("oauth2-proxy")))
		kn.Spec.TLS.Mode = "unsupported"
		for _, service := range Services(kn, kn) {
			Expect(service.Annotations).NotTo(HaveKey(OCPServingCertAnnotation))
		}
	})

	It("UT-AUDIT-515-RESOURCES-005: rejects invalid source modes and non-serializable resource values", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = "unknown"
		Expect(TLSSecretName(kn, ComponentGateway)).To(BeEmpty())
		object := &unstructured.Unstructured{Object: map[string]interface{}{"invalid": math.NaN()}}
		Expect(SpecHash(object)).To(BeEmpty())
		_, err := marshalYAML(map[string]float64{"invalid": math.Inf(1)})
		Expect(err).To(HaveOccurred())
		Expect(parseFloatDefault("invalid", 0.5)).To(Equal(0.5))
		Expect(parseLLMTemperature("invalid")).To(BeNil())
		Expect(afAgentLLMConfig(kubernautv1alpha2.LLMProfileSpec{}).Provider).To(Equal(LLMProviderVertexAI))
		kn.Spec.KubernautAgent.Audit.FlushIntervalSeconds = "NaN"
		_, err = KubernautAgentConfigMap(kn, kn)
		Expect(err).To(MatchError(ContainSubstring("yaml marshal")))
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Temperature = "NaN"
		kn.Spec.LLMProfiles["primary"] = profile
		_, err = KubernautAgentLLMRuntimeConfigMap(kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{LLMConfidence: "NaN"}
		_, err = APIFrontendConfigMap(kn, kn)
		Expect(err).To(HaveOccurred())
	})

	It("UT-AUDIT-515-RESOURCES-006: normalizes optional interaction and trust settings and rejects contradictory trust inputs", func() {
		Expect(kaInteractiveConfig(&kubernautv1alpha2.InteractiveSpec{Enabled: ptr.To(false)})).To(BeNil())
		interactive := kaInteractiveConfig(&kubernautv1alpha2.InteractiveSpec{SessionTTL: "40m", InactivityTimeout: "20m"})
		Expect(interactive.SessionTTL).To(Equal("40m"))
		Expect(interactive.InactivityTimeout).To(Equal("20m"))
		for _, trust := range []*kubernautv1alpha2.FleetTrustSpec{
			{Source: kubernautv1alpha2.FleetTrustSourceSystem}, {Source: kubernautv1alpha2.FleetTrustSourceFile, CAFile: "/ca.crt"},
			{Source: "unknown"},
		} {
			file := resolveFleetTrustCAFile(trust, "/default", "/secret")
			switch trust.Source {
			case kubernautv1alpha2.FleetTrustSourceSystem:
				Expect(file).To(BeEmpty())
			case kubernautv1alpha2.FleetTrustSourceFile:
				Expect(file).To(Equal("/ca.crt"))
			default:
				Expect(file).To(Equal("/default"))
			}
		}
		Expect(validateFleetTrust("trust", &kubernautv1alpha2.FleetTrustSpec{})).To(BeEmpty())
		for _, trust := range []*kubernautv1alpha2.FleetTrustSpec{
			{Source: kubernautv1alpha2.FleetTrustSourceFile},
			{Source: kubernautv1alpha2.FleetTrustSourceFile, CAFile: "/ca", CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{}},
			{Source: kubernautv1alpha2.FleetTrustSourceSecret, CAFile: "/ca"}, {Source: "unknown"},
		} {
			Expect(validateFleetTrust("trust", trust)).NotTo(BeEmpty())
		}
		Expect(validateJWTProviderJWKSURL(strings.Repeat("x", maxJWKSURLLength+1), "jwt", false, "insecure")).To(HaveOccurred())
		role := kubernautv1alpha2.ToolRoleBinding{ClusterRoleName: "custom", Groups: []string{"engineers"}}
		Expect(validateToolRoleBinding(role, 1, map[string]bool{"custom": true})).To(HaveOccurred())
		kn, fleet := testKubernautWithFMC()
		fleet.Spec.Fleet.MCPGateway.Type = "unknown"
		Expect(validateFleetConfig(fleet)).NotTo(BeEmpty())
		fleet.Spec.Fleet.OAuth2.CredentialsSecretRef = ""
		fleet.Spec.Gateway.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "own"}
		Expect(validateFleetOAuth2(fleet, &fleet.Spec.Fleet)).NotTo(BeEmpty())
		fleet.Spec.Fleet.OAuth2.CredentialsSecretRef = "shared"
		fleet.Spec.Fleet.OAuth2.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "oauth-ca"}}
		deployment, err := FleetMetadataCacheDeployment(kn, fleet)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "fleet-oauth2-ca")))
		llm := &kaLLMYAML{}
		applyKALLMProfileFields(llm, kubernautv1alpha2.LLMProfileSpec{VertexProject: "project", VertexLocation: "location"})
		Expect(llm.VertexProject).To(Equal("project"))
		Expect(llm.VertexLocation).To(Equal("location"))
	})
})

const unitLoopbackHost = "localhost"

func withoutImage(name string) {
	previous, existed := os.LookupEnv(name)
	ExpectWithOffset(1, os.Unsetenv(name)).To(Succeed())
	DeferCleanup(func() {
		if existed {
			Expect(os.Setenv(name, previous)).To(Succeed())
		} else {
			Expect(os.Unsetenv(name)).To(Succeed())
		}
	})
}

var _ = Describe("embedded CRD publication over a simulated external API", func() {
	It("UT-AUDIT-515-CRDS-001: creates or updates raw CRDs and reports independent API failures", func() {
		for _, operation := range []string{"create", "update", "get-error", "create-error", "update-error"} {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				defer GinkgoRecover()
				response.Header().Set("Content-Type", "application/json")
				if request.Method == http.MethodGet && strings.HasPrefix(operation, "create") {
					response.WriteHeader(http.StatusNotFound)
					_, err := response.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`))
					Expect(err).NotTo(HaveOccurred())
					return
				}
				if strings.HasSuffix(operation, "error") && (operation == "get-error" || request.Method != http.MethodGet) {
					response.WriteHeader(http.StatusForbidden)
					_, err := response.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`))
					Expect(err).NotTo(HaveOccurred())
					return
				}
				object := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
					"metadata": map[string]interface{}{"name": "fixture", "resourceVersion": "1"},
				}}
				object.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kubernaut-operator"})
				if request.Method != http.MethodGet {
					Expect(json.NewDecoder(request.Body).Decode(&object.Object)).To(Succeed())
					Expect(object.Object).To(HaveKey("spec"))
					writes.Add(1)
				}
				Expect(json.NewEncoder(response).Encode(object.Object)).To(Succeed())
			}))
			DeferCleanup(server.Close)
			err := EnsureCRDs(context.Background(), &rest.Config{Host: server.URL, Timeout: 5 * time.Second})
			if strings.HasSuffix(operation, "error") {
				Expect(err).To(HaveOccurred(), operation)
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(writes.Load()).To(BeNumerically(">", 0))
			}
		}
		Expect(EnsureCRDs(context.Background(), &rest.Config{Host: "://"})).To(HaveOccurred())
		for _, invalid := range []string{"[broken yaml", "42"} {
			_, err := yamlToUnstructured([]byte(invalid))
			Expect(err).To(HaveOccurred())
		}
	})
	It("UT-AUDIT-515-ADMISSION-RESOURCE-001: renders the singleton admission endpoint identity", func() {
		configuration := SingletonValidatingWebhookConfiguration("operator", map[string]string{"managed": "true"})
		Expect(configuration.Name).To(Equal("operator-kubernaut-singleton"))
		Expect(configuration.Webhooks[0].ClientConfig.Service.Namespace).To(Equal("operator"))
		Expect(configuration.Webhooks[0].ClientConfig.Service.Path).To(Equal(ptr.To("/validate-kubernaut-singleton")))
		Expect(configuration.Webhooks[0].Rules[0].Resources).To(Equal([]string{"kubernauts"}))
		Expect(configuration.Labels).To(Equal(map[string]string{"managed": "true"}))
		Expect(configuration.CreationTimestamp).To(Equal(metav1.Time{}))
	})
	It("UT-AUDIT-515-TRUST-002: executes uncached trust reads through the real Kubernetes client and preserves API denials", func() {
		var denied atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			defer GinkgoRecover()
			Expect(request.URL.Path).To(BeElementOf([]string{
				"/api/v1/namespaces/tenant/configmaps/" + InterServiceCAConfigMapName,
				"/api/v1/namespaces/" + defaultIngressCertNamespace + "/configmaps/" + defaultIngressCertName,
			}))
			response.Header().Set("Content-Type", "application/json")
			if denied.Load() {
				response.WriteHeader(http.StatusForbidden)
				_, err := response.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"Forbidden","code":403}`))
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(json.NewEncoder(response).Encode(map[string]interface{}{
				"apiVersion": "v1", "kind": "ConfigMap", "data": map[string]string{
					trustBundleServiceCAKey: "service-ca", defaultIngressCertKey: "router-ca",
				},
			})).To(Succeed())
		}))
		DeferCleanup(server.Close)
		previousConfig := trustBundleInClusterConfigFunc
		DeferCleanup(func() { trustBundleInClusterConfigFunc = previousConfig })
		trustBundleInClusterConfigFunc = func() (*rest.Config, error) {
			return &rest.Config{Host: server.URL, Timeout: 5 * time.Second}, nil
		}
		serviceCA, err := readServiceCA("tenant")
		Expect(err).NotTo(HaveOccurred())
		Expect(serviceCA).To(Equal("service-ca"))
		routerCA, err := readDefaultIngressCert()
		Expect(err).NotTo(HaveOccurred())
		Expect(routerCA).To(Equal("router-ca"))
		denied.Store(true)
		_, err = readServiceCA("tenant")
		Expect(err).To(HaveOccurred())
		_, err = readDefaultIngressCert()
		Expect(err).To(HaveOccurred())
		trustBundleInClusterConfigFunc = func() (*rest.Config, error) {
			return &rest.Config{Host: "://"}, nil
		}
		_, err = trustBundleClientset()
		Expect(err).To(MatchError(ContainSubstring("build kubernetes clientset")))
	})
	It("UT-AUDIT-515-ASSETS-001: preserves filesystem and parse errors and skips directories using real asset-loading logic", func() {
		kn := testKubernaut()
		migrations := fstest.MapFS{
			"migrations/001.sql":            {Data: []byte("SELECT 1;")},
			"migrations/nested/ignored.sql": {Data: []byte("not a top-level migration")},
		}
		config, err := migrationConfigMapFromFS(kn, migrations)
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Data).To(Equal(map[string]string{"001.sql": "SELECT 1;"}))
		_, err = migrationConfigMapFromFS(kn, fstest.MapFS{})
		Expect(err).To(MatchError(ContainSubstring("reading embedded migrations")))
		_, err = migrationConfigMapFromFS(kn, unitReadFailingFS{FS: migrations, path: "migrations/001.sql"})
		Expect(err).To(MatchError(ContainSubstring("reading migration 001.sql")))
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			defer GinkgoRecover()
			response.Header().Set("Content-Type", "application/json")
			if request.Method == http.MethodGet {
				response.WriteHeader(http.StatusNotFound)
				_, writeErr := response.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`))
				Expect(writeErr).NotTo(HaveOccurred())
				return
			}
			object := &unstructured.Unstructured{}
			Expect(json.NewDecoder(request.Body).Decode(&object.Object)).To(Succeed())
			Expect(object.GetName()).To(Equal("fixture.example.com"))
			Expect(json.NewEncoder(response).Encode(object.Object)).To(Succeed())
		}))
		DeferCleanup(server.Close)
		api, err := dynamic.NewForConfig(&rest.Config{Host: server.URL, Timeout: 5 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		crds := fstest.MapFS{
			"crds/fixture.yaml":        {Data: []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: fixture.example.com\n")},
			"crds/nested/ignored.yaml": {Data: []byte("not a top-level CRD")},
		}
		Expect(ensureEmbeddedCRDs(context.Background(), api.Resource(crdGVR), crds)).To(Succeed())
		Expect(ensureEmbeddedCRDs(context.Background(), api.Resource(crdGVR), fstest.MapFS{})).
			To(MatchError(ContainSubstring("reading embedded CRD directory")))
		Expect(ensureEmbeddedCRDs(context.Background(), api.Resource(crdGVR), unitReadFailingFS{FS: crds, path: "crds/fixture.yaml"})).
			To(MatchError(ContainSubstring("reading embedded CRD fixture.yaml")))
		crds["crds/fixture.yaml"].Data = []byte("[malformed yaml")
		Expect(ensureEmbeddedCRDs(context.Background(), api.Resource(crdGVR), crds)).
			To(MatchError(ContainSubstring("parsing CRD fixture.yaml")))
	})
	It("UT-AUDIT-515-SERIALIZATION-001: preserves real serializer failures instead of manufacturing invalid resource payloads", func() {
		metadata := ObjectMeta(testKubernaut(), "config", ComponentDataStorage)
		config, err := yamlConfigMap(metadata, "config.yaml", "service config", math.NaN())
		Expect(config).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("service config: resources: yaml marshal")))
		secret, err := yamlSecret(metadata, "db-secrets.yaml", math.NaN())
		Expect(secret).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("marshalling db-secrets.yaml")))
		Expect(CertManagerBootstrapIssuerNames(nil)).To(BeEmpty())
		kn := testKubernaut()
		Expect(CertManagerBootstrapIssuerNames(kn)).To(BeEmpty())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeCertManager
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{},
		}
		Expect(CertManagerBootstrapIssuerNames(kn)).To(Equal([]string{defaultCertManagerBootstrapIssuerName, defaultCertManagerCAIssuerName}))
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{
			BootstrapIssuerName: "custom-bootstrap", InternalCAIssuerName: "custom-ca",
		}}
		Expect(CertManagerBootstrapIssuerNames(kn)).To(Equal([]string{"custom-bootstrap", "custom-ca"}))
	})
})

// Only the external filesystem transport fails. Real directory traversal,
// content handling, YAML parsing and Kubernetes publication stay unchanged.
type unitReadFailingFS struct {
	fs.FS
	path string
}

func (f unitReadFailingFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.FS.Open(name)
}
