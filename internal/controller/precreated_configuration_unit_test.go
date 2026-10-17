package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

func seedValidPrecreatedInputs(r *KubernautReconciler, kn *kubernautv1alpha2.Kubernaut) {
	objects := []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: llmSecretName, Namespace: kn.Namespace},
			Data:       map[string][]byte{"api_key": []byte("unit-api-key")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.AIAnalysis.Policy.ConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"approval.rego": "package kubernaut.aianalysis"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.SignalProcessing.Policy.ConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"policy.rego": "package kubernaut.signalprocessing"},
		},
	}
	for _, object := range objects {
		err := r.Create(ctx, object)
		ExpectWithOffset(1, err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue(), "seed %s/%s: %v", object.GetNamespace(), object.GetName(), err)
	}
}

func seedPrecreatedObject(r *KubernautReconciler, object client.Object) {
	ExpectWithOffset(1, r.Create(context.Background(), object)).To(Succeed())
}

var _ = Describe("pre-created configuration validation", Label("unit"), func() {
	It("UT-KA-527-CTRL-001 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V16.5.2] ignores an unresolved logical-agent profile while validation owns the user-facing error", func() {
		kn := newMinimalCR()
		collector := newPrecreatedReferenceCollector()
		appendPrecreatedLLMProfileReference(collector, kn, "does-not-exist")

		Expect(collector.references).To(BeEmpty(), "an unresolved profile must not create a Secret reference")
		Expect(collector.errs).To(BeEmpty(), "profile referential integrity reports this error before pre-created Secret collection")
	})

	It("UT-419-CTRL-001 [IA-5, AC-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V8.2.1, v5.0.0-V13.3.1] collects pre-created Secrets for the enabled alignment sibling agent", func() {
		kn := newMinimalCR()
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "alignment-profile",
		}
		kn.Spec.LLMProfiles["alignment-profile"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-3-5-sonnet",
			CredentialsSecretName: "alignment-llm-credentials",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled:              true,
				CredentialsSecretRef: "alignment-oauth",
			},
			TLSCertFile:        "/etc/llm/tls.crt",
			TLSKeyFile:         "/etc/llm/tls.key",
			TLSClientSecretRef: "alignment-tls",
		}
		collector := newPrecreatedReferenceCollector()
		appendPrecreatedLLMReferences(collector, kn)
		hasReference := func(path, name string, requiredKeys ...string) bool {
			for _, reference := range collector.references {
				if reference.path != path || reference.name != name || len(reference.requiredKeys) != len(requiredKeys) {
					continue
				}
				keys := make(map[string]struct{}, len(reference.requiredKeys))
				for _, key := range reference.requiredKeys {
					keys[key] = struct{}{}
				}
				matched := true
				for _, key := range requiredKeys {
					if _, ok := keys[key]; !ok {
						matched = false
						break
					}
				}
				if matched {
					return true
				}
			}
			return false
		}

		Expect(collector.errs).To(BeEmpty())
		Expect(hasReference(
			"spec.llmProfiles[\"alignment-profile\"].credentialsSecretName",
			"alignment-llm-credentials", "api_key",
		)).To(BeTrue())
		Expect(hasReference(
			"spec.llmProfiles[\"alignment-profile\"].oauth2.credentialsSecretRef",
			"alignment-oauth", "client-id", "client-secret",
		)).To(BeTrue())
		Expect(hasReference(
			"spec.llmProfiles[\"alignment-profile\"].tlsClientSecretRef",
			"alignment-tls", "tls.crt", "tls.key",
		)).To(BeTrue())
	})

	It("UT-CONFIG-512-001 rejects a missing required ConfigMap before migration", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)
		kn.Spec.AIAnalysis.Policy.ConfigMapName = "missing-approval-policy"

		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		refreshBusinessUnitCR(r, kn)

		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("PrecreatedConfigurationInvalid"))
		Expect(condition.Message).To(ContainSubstring("missing-approval-policy"))
		jobs := &batchv1.JobList{}
		Expect(r.List(ctx, jobs)).To(Succeed())
		Expect(jobs.Items).To(BeEmpty())
	})

	It("UT-CONFIG-512-002 rejects a referenced Secret with a missing required key", func() {
		kn := newMinimalCR()
		kn.Spec.Notification.Slack.SecretName = "missing-slack-key"
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)
		seedPrecreatedObject(r, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "missing-slack-key", Namespace: kn.Namespace},
			Data:       map[string][]byte{},
		})

		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		refreshBusinessUnitCR(r, kn)

		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("PrecreatedConfigurationInvalid"))
		Expect(condition.Message).To(ContainSubstring("webhook-url"))
	})

	It("UT-CONFIG-512-003 accepts valid conditional references", func() {
		kn := newMinimalCR()
		kn.Spec.KubernautAgent.RuntimeConfigMapName = "custom-llm-runtime"
		kn.Spec.Notification.Routing = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: "custom-routing"}
		kn.Spec.Notification.Slack.SecretName = "slack-webhook"
		kn.Spec.Gateway.Auth.AlertManager.TokenSecretName = "alertmanager-token"
		kn.Spec.WorkflowExecution.Ansible.Enabled = true
		kn.Spec.WorkflowExecution.Ansible.APIURL = "https://awx.example.com"
		kn.Spec.WorkflowExecution.Ansible.TokenSecretRef = &kubernautv1alpha2.SecretKeyRef{Name: "ansible-token"}
		kn.Spec.WorkflowExecution.Ansible.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "ansible-ca"}
		kn.Spec.Console.Enabled = ptr.To(true)
		kn.Spec.Console.Auth.SecretName = "console-oidc"
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{LLMProfileRef: "primary"}
		kn.Spec.Valkey.TLS = &kubernautv1alpha2.ValkeyTLSSpec{
			Enabled:              true,
			CASecretName:         "valkey-ca",
			ClientCertSecretName: "valkey-client",
		}
		profile := kn.Spec.LLMProfiles["primary"]
		profile.OAuth2.Enabled = true
		profile.OAuth2.TokenURL = "https://idp.example/token"
		profile.OAuth2.CredentialsSecretRef = "llm-oauth"
		profile.TLSCertFile = "/etc/llm/tls.crt"
		profile.TLSKeyFile = "/etc/llm/tls.key"
		profile.TLSClientSecretRef = "llm-tls"
		kn.Spec.LLMProfiles["primary"] = profile
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)
		for _, object := range []client.Object{
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "custom-llm-runtime", Namespace: kn.Namespace}, Data: map[string]string{"llm-runtime.yaml": "model: gpt-4o"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "custom-routing", Namespace: kn.Namespace}, Data: map[string]string{"routing.yaml": "route: {}"}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "slack-webhook", Namespace: kn.Namespace}, Data: map[string][]byte{"webhook-url": []byte("https://hooks.slack.example/unit")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alertmanager-token", Namespace: kn.Namespace}, Data: map[string][]byte{"token": []byte("unit-token")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ansible-token", Namespace: kn.Namespace}, Data: map[string][]byte{"token": []byte("unit-token")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ansible-ca", Namespace: kn.Namespace}, Data: map[string][]byte{"ca.crt": []byte("unit-ca")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "console-oidc", Namespace: kn.Namespace}, Data: map[string][]byte{"client-id": []byte("cid"), "client-secret": []byte("csec"), "cookie-secret": []byte("cookie")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "llm-oauth", Namespace: kn.Namespace}, Data: map[string][]byte{"client-id": []byte("cid"), "client-secret": []byte("csec")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "llm-tls", Namespace: kn.Namespace}, Data: validPrecreatedTLSData()},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-ca", Namespace: kn.Namespace}, Data: validPrecreatedCAData()},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-client", Namespace: kn.Namespace}, Data: validPrecreatedTLSData()},
		} {
			seedPrecreatedObject(r, object)
		}

		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))
		refreshBusinessUnitCR(r, kn)
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)).To(BeTrue())
	})

	It("UT-CONFIG-512-006 covers invalid and conditional reference branches", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedPrecreatedObject(r, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "missing-key", Namespace: kn.Namespace}})
		seedPrecreatedObject(r, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "empty-key", Namespace: kn.Namespace}, Data: map[string]string{"required": ""}})
		seedPrecreatedObject(r, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "binary-key", Namespace: kn.Namespace}, BinaryData: map[string][]byte{"required": []byte("binary")}})
		seedPrecreatedObject(r, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "binary-empty", Namespace: kn.Namespace}, BinaryData: map[string][]byte{"required": nil}})
		emptySecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-secret", Namespace: kn.Namespace},
			Data:       map[string][]byte{"required": nil},
		}
		seedPrecreatedObject(r, emptySecret)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.test.configMap", name: "missing-key", requiredKeys: []string{"required"},
		})).To(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.test.configMap", name: "empty-key", requiredKeys: []string{"required"},
		})).To(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.test.configMap", name: "binary-key", requiredKeys: []string{"required"},
		})).NotTo(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.test.configMap", name: "binary-empty", requiredKeys: []string{"required"},
		})).To(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.test.secret", name: "missing-secret", requiredKeys: []string{"required"},
		})).To(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.test.secret", name: "empty-secret", requiredKeys: []string{"required"},
		})).To(HaveOccurred())
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedReferenceKind("unsupported"), path: "spec.test.unsupported", name: "object",
		})).To(HaveOccurred())

		defaultNames := newMinimalCR()
		defaultNames.Spec.AIAnalysis.Policy.ConfigMapName = ""
		defaultNames.Spec.SignalProcessing.Policy.ConfigMapName = ""
		_, defaultErrs := precreatedConfigurationReferences(defaultNames)
		Expect(defaultErrs).To(BeEmpty())

		kn.Spec.AIAnalysis.Policy.ConfigMapName = ""
		kn.Spec.WorkflowExecution.Ansible.Enabled = true
		kn.Spec.WorkflowExecution.Ansible.TokenSecretRef = nil
		const primaryProfile = "primary"
		profile := kn.Spec.LLMProfiles[primaryProfile]
		profile.OAuth2.Enabled = true
		profile.OAuth2.TokenURL = "https://idp.example/token"
		profile.OAuth2.CredentialsSecretRef = ""
		kn.Spec.LLMProfiles[primaryProfile] = profile
		kn.Spec.LLMProfiles["vertex"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              "vertex_ai",
			Model:                 "gemini",
			CredentialsSecretName: "vertex-credentials",
		}
		kn.Spec.KubernautAgent.Investigation.LLMProfileRef = "vertex"
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = primaryProfile
		_, errs := precreatedConfigurationReferences(kn)
		Expect(errs).NotTo(BeEmpty())

		kn = newMinimalCR()
		kn.Spec.Gateway.Ingress = kubernautv1alpha2.IngressSpec{
			Enabled: ptr.To(true), IngressClassName: "nginx", Host: "gateway.example.com", TLSSecretName: "gateway-ingress-tls",
		}
		_, errs = precreatedConfigurationReferences(kn)
		Expect(errs).To(BeEmpty())
		kn.Spec.Gateway.Ingress.IngressClassName = ""
		_, errs = precreatedConfigurationReferences(kn)
		Expect(errs).NotTo(BeEmpty())

		enabled := true
		kn = newMinimalCR()
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &enabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "fleetmetadatacache"},
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Endpoint: "https://mcp.example.com", Type: "eaigw", Namespace: kn.Namespace},
			OAuth2:     kubernautv1alpha2.FleetOAuth2Spec{CredentialsSecretRef: "fleet-default"},
		}
		kn.Spec.Gateway.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "gateway-fleet"}
		kn.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "fmc-fleet"}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = "workflow-fleet"
		_, errs = precreatedConfigurationReferences(kn)
		Expect(errs).To(BeEmpty())
	})

	It("UT-CONFIG-512-004 does not require omitted optional references or generated runtime ConfigMaps", func() {
		kn := newMinimalCR()
		kn.Spec.KubernautAgent.RuntimeConfigMapName = ""
		kn.Spec.Notification.Routing = nil
		kn.Spec.Notification.Slack.SecretName = ""
		kn.Spec.WorkflowExecution.Ansible.Enabled = false
		kn.Spec.Console.Enabled = ptr.To(false)
		kn.Spec.APIFrontend.Enabled = ptr.To(false)
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)

		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)).To(BeTrue())
	})

	It("UT-CONFIG-512-005 does not create migration or workloads when a required reference is absent", func() {
		kn := newMinimalCR()
		kn.Spec.KubernautAgent.RuntimeConfigMapName = "missing-runtime-config"
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)

		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}
		_, err := r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		result, err := r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))

		refreshBusinessUnitCR(r, kn)
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		jobs := &batchv1.JobList{}
		Expect(r.List(ctx, jobs)).To(Succeed())
		Expect(jobs.Items).To(BeEmpty())
		deployments := &appsv1.DeploymentList{}
		Expect(r.List(ctx, deployments)).To(Succeed())
		Expect(deployments.Items).To(BeEmpty())
	})

	It("UT-CONFIG-512-007 validates content semantics and maps external input changes", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())

		invalidYAML := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-runtime", Namespace: kn.Namespace},
			Data:       map[string]string{"llm-runtime.yaml": "not: [valid"},
		}
		seedPrecreatedObject(r, invalidYAML)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.runtime", name: invalidYAML.Name, requiredKeys: []string{"llm-runtime.yaml"},
		})).To(MatchError(ContainSubstring("invalid YAML")))

		invalidJSON := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-credentials", Namespace: kn.Namespace},
			Data:       map[string][]byte{"credentials.json": []byte("{invalid")},
		}
		seedPrecreatedObject(r, invalidJSON)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.credentials", name: invalidJSON.Name, requiredKeys: []string{"credentials.json"},
		})).To(MatchError(ContainSubstring("invalid JSON")))

		validRuntime := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valid-runtime", Namespace: kn.Namespace},
			Data:       map[string]string{"llm-runtime.yaml": "models:\n  primary: {}\n"},
		}
		seedPrecreatedObject(r, validRuntime)
		kn.Spec.KubernautAgent.RuntimeConfigMapName = validRuntime.Name
		Expect(r.Update(ctx, kn)).To(Succeed())
		other := newMinimalCR()
		other.Name = "precreated-other"
		other.Namespace = "precreated-other"
		Expect(r.Create(ctx, other)).To(Succeed())
		requests := r.precreatedConfigurationToKubernaut(ctx, validRuntime)
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))
		Expect(r.precreatedConfigurationToKubernaut(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-secret", Namespace: kn.Namespace}})).To(BeEmpty())
		noAPIReader := &KubernautReconciler{Client: r.Client, Scheme: r.Scheme}
		Expect(noAPIReader.precreatedConfigurationToKubernaut(ctx, validRuntime)).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))

		unrelated := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: kn.Namespace}}
		Expect(r.precreatedConfigurationToKubernaut(ctx, unrelated)).To(BeEmpty())

		invalidRego := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-rego", Namespace: kn.Namespace},
			Data:       map[string]string{"approval.rego": "default allow = true"},
		}
		seedPrecreatedObject(r, invalidRego)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.policy", name: invalidRego.Name, requiredKeys: []string{"approval.rego"},
		})).To(MatchError(ContainSubstring("Rego package declaration")))

		emptyYAML := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-runtime", Namespace: kn.Namespace},
			Data:       map[string]string{"llm-runtime.yaml": "{}"},
		}
		seedPrecreatedObject(r, emptyYAML)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedConfigMapReference, path: "spec.runtime", name: emptyYAML.Name, requiredKeys: []string{"llm-runtime.yaml"},
		})).To(MatchError(ContainSubstring("must contain a YAML object")))

		invalidCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-ca", Namespace: kn.Namespace},
			Data:       map[string][]byte{"ca.crt": []byte("not-pem")},
		}
		seedPrecreatedObject(r, invalidCA)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.ca", name: invalidCA.Name, requiredKeys: []string{"ca.crt"},
		})).To(MatchError(ContainSubstring("valid CA PEM")))

		invalidTLS := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-tls", Namespace: kn.Namespace},
			Data:       map[string][]byte{corev1.TLSCertKey: []byte("cert"), corev1.TLSPrivateKeyKey: []byte("key")},
		}
		seedPrecreatedObject(r, invalidTLS)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.tls", name: invalidTLS.Name, requiredKeys: []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey},
		})).To(MatchError(ContainSubstring("invalid TLS certificate/key pair")))

		invalidURL := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-url", Namespace: kn.Namespace},
			Data:       map[string][]byte{"webhook-url": []byte("not-a-url")},
		}
		seedPrecreatedObject(r, invalidURL)
		Expect(r.validatePrecreatedReference(ctx, kn.Namespace, precreatedReference{
			kind: precreatedSecretReference, path: "spec.slack", name: invalidURL.Name, requiredKeys: []string{"webhook-url"},
		})).To(MatchError(ContainSubstring("valid URL")))

		Expect(r.precreatedConfigurationToKubernaut(ctx, nil)).To(BeEmpty())
		Expect(r.precreatedConfigurationToKubernaut(ctx, &appsv1.Deployment{})).To(BeEmpty())
		listErrorReconciler := businessUnitReconciler(kn, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return fmt.Errorf("list failed")
		}})
		Expect(listErrorReconciler.precreatedConfigurationToKubernaut(ctx, validRuntime)).To(BeEmpty())
	})
})

func validPrecreatedTLSData() map[string][]byte {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "precreated.example"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return map[string][]byte{
		corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
}

func validPrecreatedCAData() map[string][]byte {
	return map[string][]byte{"ca.crt": validPrecreatedTLSData()[corev1.TLSCertKey]}
}

func clientInterceptorNone() interceptor.Funcs {
	return interceptor.Funcs{}
}
