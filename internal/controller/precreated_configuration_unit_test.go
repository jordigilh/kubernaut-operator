package controller

import (
	"context"

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
		kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: "custom-proactive"}
		kn.Spec.Notification.Routing = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: "custom-routing"}
		kn.Spec.Notification.Slack.SecretName = "slack-webhook"
		kn.Spec.Gateway.AlertManagerTokenSecretName = "alertmanager-token"
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
		profile.OAuth2.CredentialsSecretRef = "llm-oauth"
		profile.TLSCertFile = "/etc/llm/tls.crt"
		profile.TLSKeyFile = "/etc/llm/tls.key"
		profile.TLSClientSecretRef = "llm-tls"
		kn.Spec.LLMProfiles["primary"] = profile
		r := businessUnitReconciler(kn, clientInterceptorNone())
		seedValidPrecreatedInputs(r, kn)
		for _, object := range []client.Object{
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "custom-llm-runtime", Namespace: kn.Namespace}, Data: map[string]string{"llm-runtime.yaml": "model: gpt-4o"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "custom-proactive", Namespace: kn.Namespace}, Data: map[string]string{"proactive-signal-mappings.yaml": "proactive_signal_mappings: {}"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "custom-routing", Namespace: kn.Namespace}, Data: map[string]string{"routing.yaml": "route: {}"}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "slack-webhook", Namespace: kn.Namespace}, Data: map[string][]byte{"webhook-url": []byte("https://hooks.slack.example/unit")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alertmanager-token", Namespace: kn.Namespace}, Data: map[string][]byte{"token": []byte("unit-token")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ansible-token", Namespace: kn.Namespace}, Data: map[string][]byte{"token": []byte("unit-token")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ansible-ca", Namespace: kn.Namespace}, Data: map[string][]byte{"ca.crt": []byte("unit-ca")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "console-oidc", Namespace: kn.Namespace}, Data: map[string][]byte{"client-id": []byte("cid"), "client-secret": []byte("csec"), "cookie-secret": []byte("cookie")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "llm-oauth", Namespace: kn.Namespace}, Data: map[string][]byte{"client-id": []byte("cid"), "client-secret": []byte("csec")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "llm-tls", Namespace: kn.Namespace}, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-ca", Namespace: kn.Namespace}, Data: map[string][]byte{"ca.crt": []byte("ca")}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-client", Namespace: kn.Namespace}, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}},
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
		kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha2.ConfigMapRef{}
		kn.Spec.WorkflowExecution.Ansible.Enabled = true
		kn.Spec.WorkflowExecution.Ansible.TokenSecretRef = nil
		profile := kn.Spec.LLMProfiles["primary"]
		profile.OAuth2.Enabled = true
		profile.OAuth2.CredentialsSecretRef = ""
		kn.Spec.LLMProfiles["primary"] = profile
		kn.Spec.LLMProfiles["vertex"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              "vertex_ai",
			Model:                 "gemini",
			CredentialsSecretName: "vertex-credentials",
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": "vertex", "validation": "undefined"}
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
		kn.Spec.SignalProcessing.ProactiveSignalMappings = nil
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
})

func clientInterceptorNone() interceptor.Funcs {
	return interceptor.Funcs{}
}
