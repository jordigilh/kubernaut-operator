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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const (
	testNamespace         = "default"
	pgSecretName          = "postgresql-secret"
	vkSecretName          = "valkey-secret"
	llmSecretName         = "llm-credentials"
	consoleOIDCSecretName = "console-oidc"
	timeout               = 10 * time.Second
	interval              = 250 * time.Millisecond

	// testWEFleetOAuth2SecretRef is WorkflowExecution's own write-scoped
	// fleet OAuth2 credential (#235, DD-235). Any test that enables
	// spec.fleet.oauth2 and drives the CR through phaseValidate must set
	// this -- WE's credential is independently required and never falls
	// back to the shared spec.fleet.oauth2.credentialsSecretRef.
	testWEFleetOAuth2SecretRef = "we-write-oauth2-creds"
)

func singletonKey() types.NamespacedName {
	return types.NamespacedName{Name: kubernautv1alpha1.SingletonName, Namespace: testNamespace}
}

func newMinimalCR() *kubernautv1alpha1.Kubernaut {
	return &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernautv1alpha1.SingletonName,
			Namespace: testNamespace,
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			Image: kubernautv1alpha1.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
			},
			PostgreSQL: kubernautv1alpha1.PostgreSQLSpec{
				SecretName: pgSecretName,
				Host:       "postgresql",
			},
			Valkey: kubernautv1alpha1.ValkeySpec{
				SecretName: vkSecretName,
				Host:       "valkey",
			},
			APIFrontend: kubernautv1alpha1.APIFrontendSpec{
				Auth: kubernautv1alpha1.APIFrontendAuthSpec{
					IssuerURL: "https://login.kubernaut.ai/realms/kubernaut",
					Audience:  "kubernaut-apifrontend",
				},
			},
			LLMProfiles: map[string]kubernautv1alpha1.LLMProfileSpec{
				"primary": {
					Provider:              "openai",
					Model:                 "gpt-4o",
					Endpoint:              "http://llm-gateway:8080",
					CredentialsSecretName: llmSecretName,
				},
			},
			KubernautAgent: kubernautv1alpha1.KubernautAgentSpec{
				LLMProfileRef: "primary",
			},
			AIAnalysis: kubernautv1alpha1.AIAnalysisSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "aianalysis-policy"},
			},
			SignalProcessing: kubernautv1alpha1.SignalProcessingSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
		},
	}
}

func newReconciler() *KubernautReconciler {
	return &KubernautReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: events.NewFakeRecorder(100),
		RestCfg:  cfg,
		now:      time.Now,
	}
}

func createBYOSecrets(ctx context.Context) {
	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
			Data: map[string][]byte{
				"POSTGRES_USER":     []byte("kubernaut"),
				"POSTGRES_PASSWORD": []byte("secret"),
				"POSTGRES_DB":       []byte("kubernaut"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: vkSecretName, Namespace: testNamespace},
			Data: map[string][]byte{
				"valkey-secrets.yaml": []byte("password: secret"),
			},
		},
	}
	for _, s := range secrets {
		existing := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, existing)
		if errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, s)).To(Succeed())
		}
	}
}

func deleteBYOSecrets(ctx context.Context) {
	// #372: consoleOIDCSecretName is BYO/user-supplied (not
	// app.kubernetes.io/managed-by: kubernaut-operator labeled, so
	// cleanupNamespacedResources's Secret sweep won't catch it either) and
	// created directly by name in several console-auth specs
	// (kubernaut_lifecycle_test.go). Must be cleaned up here so those specs
	// don't collide under any run order.
	for _, name := range []string{pgSecretName, vkSecretName, consoleOIDCSecretName} {
		s := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, s)
		if err == nil {
			_ = k8sClient.Delete(ctx, s)
		}
	}
}

func deleteCRIfExists(ctx context.Context) {
	cr := &kubernautv1alpha1.Kubernaut{}
	err := k8sClient.Get(ctx, singletonKey(), cr)
	if err == nil {
		controllerutil.RemoveFinalizer(cr, kubernautv1alpha1.FinalizerName)
		_ = k8sClient.Update(ctx, cr)
		_ = k8sClient.Delete(ctx, cr)
	}
}

var _ = Describe("Kubernaut Controller", func() {

	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	// ---- Singleton Guard ----

	Context("Singleton Guard", func() {
		It("should ignore a CR with a non-singleton name", func() {
			badCR := newMinimalCR()
			badCR.Name = "not-kubernaut"
			Expect(k8sClient.Create(ctx, badCR)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, badCR) }()

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "not-kubernaut", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "not-kubernaut", Namespace: testNamespace}, result)).To(Succeed())
			Expect(result.Finalizers).To(BeEmpty(), "non-singleton CR should not get a finalizer")
		})

		It("should accept the singleton name 'kubernaut'", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), result)).To(Succeed())
			Expect(result.Finalizers).To(ContainElement(kubernautv1alpha1.FinalizerName))
		})
	})

	// ---- Finalizer Lifecycle ----

	Context("Finalizer Lifecycle", func() {
		It("should add the finalizer on first reconcile", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName)).To(BeTrue())
		})

		It("should remove the finalizer on deletion", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()

			By("adding the finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
			stripWorkflowNamespaceCreatedByAnnotation(ctx)

			By("reconciling the deletion")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, singletonKey(), kn)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "CR should be deleted after finalizer removal")
		})
	})

	// ---- BYO Secret Validation ----

	Context("BYO Secret Validation", func() {
		It("should fail validation when PostgreSQL secret is missing", func() {
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()

			By("adding finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("attempting validation")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue on validation failure")

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("PostgreSQLSecretInvalid"))
		})

		It("should fail validation when Valkey secret is missing", func() {
			pgOnly := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"POSTGRES_USER":     []byte("u"),
					"POSTGRES_PASSWORD": []byte("p"),
					"POSTGRES_DB":       []byte("d"),
				},
			}
			Expect(k8sClient.Create(ctx, pgOnly)).To(Succeed())
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ValkeySecretInvalid"))
		})

		It("should fail when PostgreSQL secret is missing required keys", func() {
			badPG := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"POSTGRES_USER": []byte("u"),
				},
			}
			vk := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: vkSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"valkey-secrets.yaml": []byte("password: s"),
				},
			}
			Expect(k8sClient.Create(ctx, badPG)).To(Succeed())
			Expect(k8sClient.Create(ctx, vk)).To(Succeed())
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("missing required key"))
		})

		It("should pass validation with correct secrets", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	// ---- Phase Progression ----

	Context("Phase Progression", func() {
		It("should return success when CR does not exist", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	// ---- Status Conditions ----

	Context("Status Conditions", func() {
		It("should set ObservedGeneration on conditions", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Conditions).NotTo(BeEmpty(),
				"conditions should not be empty after reconciliation")

			for _, cond := range kn.Status.Conditions {
				Expect(cond.ObservedGeneration).To(Equal(kn.Generation),
					"condition %q should have ObservedGeneration matching CR generation", cond.Type)
			}
		})
	})

	// ---- LLM Reasoning CRD Schema (#211) ----
	//
	// Unit tests construct kubernautv1alpha1.LLMReasoningSpec Go values
	// directly and never touch the generated OpenAPI schema, so they cannot
	// prove the kubebuilder Enum/default markers on Effort/CapabilityOverride
	// are correct. Only a real envtest apiserver round-trip can (Pyramid
	// Invariant: "IT proves wiring").
	Context("LLM Reasoning CRD Schema", func() {
		It("LR-060 [CM-6]: an administrator's non-default reasoning configuration is accepted end-to-end and reconciles successfully", func() {
			createBYOSecrets(ctx)
			kn := newMinimalCR()
			profile := kn.Spec.LLMProfiles["primary"]
			profile.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "high"}
			kn.Spec.LLMProfiles["primary"] = profile
			Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
				"CM-6: the regenerated CRD schema must accept a legitimate, non-default reasoning block")

			r := newReconciler()
			By("first reconcile: adds finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("second reconcile: validates")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), result)).To(Succeed())
			cond := findCondition(result.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"CM-6: setting reasoning on a profile must not itself break reconciliation")
		})

		It("LR-061 [SI-10]: the API server itself rejects an out-of-enum reasoning.effort value, before any reconciler logic runs", func() {
			kn := newMinimalCR()
			profile := kn.Spec.LLMProfiles["primary"]
			profile.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "extreme"}
			kn.Spec.LLMProfiles["primary"] = profile

			err := k8sClient.Create(ctx, kn)
			Expect(err).To(HaveOccurred(),
				"SI-10: the CRD's Enum marker on reasoning.effort must be enforced by the apiserver itself, not just by application-level validation")
			Expect(errors.IsInvalid(err)).To(BeTrue(), "expected a schema validation rejection, got: %v", err)
		})
	})

	// ---- Monitoring integration is unconditional (#273) ----
	//
	// Upstream kubernaut#1839/#1841 removed AF's ungrounded LLM severity
	// fallback, so severity-gated RR creation now fails closed whenever
	// Prometheus lookups are unavailable. This operator used to let
	// spec.monitoring.enabled=false turn off severityTriage (and several
	// other integrations: Gateway AlertmanagerConfig, EM external Prometheus
	// config, KA's Prometheus/Alertmanager tools, alertmanager-view RBAC).
	// Since this operator is OCP-specific throughout and OCP ships
	// cluster-monitoring by default, that toggle never represented a safe
	// deployment topology -- as of v1.6/main the spec.monitoring field is
	// removed entirely (not just restricted to true) and every one of those
	// integrations is now wired unconditionally, with no spec field left
	// that can turn it off.
	Context("Monitoring integration is unconditional", func() {
		It("MON-001 [CM-6]: a v1.5-style manifest carrying the removed spec.monitoring.enabled=false is accepted -- the CRD's structural schema silently prunes the unknown field instead of rejecting the request -- and still reconciles with monitoring fully wired", func() {
			createBYOSecrets(ctx)
			kn := newCRWithRouteDisabled()

			u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(kn)
			Expect(err).NotTo(HaveOccurred())
			Expect(unstructured.SetNestedMap(u, map[string]interface{}{"enabled": false}, "spec", "monitoring")).To(Succeed())
			legacyCR := &unstructured.Unstructured{Object: u}
			legacyCR.SetGroupVersionKind(kubernautv1alpha1.GroupVersion.WithKind("Kubernaut"))

			Expect(k8sClient.Create(ctx, legacyCR)).To(Succeed(),
				"CM-6: a legacy manifest carrying the removed monitoring.enabled field must not be rejected -- structural schema pruning silently drops it")

			reconcileToDeployPhase(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apifrontend-config", Namespace: testNamespace}, cm)).To(Succeed())
			Expect(cm.Data["config.yaml"]).To(ContainSubstring(resources.OCPPrometheusURL),
				"CM-6: severityTriage must still reconcile against Thanos Querier even though the legacy manifest tried to disable monitoring -- there is no spec field left that can turn it off")
		})

		It("MON-002 [CM-6]: monitoring RBAC and NetworkPolicy egress to openshift-monitoring are provisioned even without any monitoring spec field present", func() {
			createBYOSecrets(ctx)
			kn := newCRWithRouteDisabled()
			kn.Spec.NetworkPolicies.Enabled = &enabled
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())

			reconcileToDeployPhase(ctx)

			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-effectivenessmonitor-alertmanager-view-binding"}, crb)).To(Succeed(),
				"CM-6: alertmanager-view RBAC must be provisioned unconditionally now that monitoring can no longer be disabled")

			np := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ComponentAPIFrontend + "-netpol", Namespace: testNamespace}, np)).To(Succeed())
			found := false
			for _, rule := range np.Spec.Egress {
				for _, peer := range rule.To {
					if peer.NamespaceSelector != nil &&
						peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == resources.OCPMonitoringNamespace {
						found = true
					}
				}
			}
			Expect(found).To(BeTrue(), "CM-6: AF's NetworkPolicy must always egress to openshift-monitoring, since severityTriage's Prometheus calls can no longer be disabled")
		})
	})

	// ---- KA Fleet GatewayType propagation (#204) ----
	//
	// Unit tests construct spec.fleet and call KubernautAgentConfigMap
	// directly, never proving the reconcile loop itself actually threads
	// spec.fleet through to KA's rendered ConfigMap in a live cluster
	// (Pyramid Invariant: "IT proves wiring").
	Context("KA Fleet GatewayType propagation", func() {
		It("KFG-060 [CM-6]: a Kubernaut CR with spec.fleet.enabled and mcpGatewayType=kuadrant reconciles successfully and KA's rendered ConfigMap carries the gatewayType through", func() {
			createBYOSecrets(ctx)
			kn := newCRWithRouteDisabled()
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())

			// Fleet moved to v1alpha2-only (fleet-branch-remove-v1alpha1):
			// set it via the v1alpha2 storage view rather than the
			// v1alpha1 create payload.
			knV2 := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled:            &enabled,
				Backend:            "fleetmetadatacache",
				Endpoint:           "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse",
				MCPGatewayType:     "kuadrant",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			// #235/DD-235: WorkflowExecution's own write-scoped credential
			// is independently required and never falls back to the
			// shared one above.
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			Expect(k8sClient.Update(ctx, knV2)).To(Succeed(),
				"CM-6: a legitimate spec.fleet configuration must be accepted end-to-end")

			reconcileToDeployPhase(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, cm)).To(Succeed())
			Expect(cm.Data["config.yaml"]).To(ContainSubstring("gatewayType: kuadrant"),
				"CM-6: the reconcile loop must render spec.fleet.mcpGatewayType into KA's live ConfigMap, not just in unit-level struct construction")
		})

		It("AF-TT-002 [CM-6]: AF's live ConfigMap renders mcp.sessionIdleTimeout/toolTimeout/toolTimeouts matching AF's own binary defaults through reconciliation, not just in unit-level struct construction", func() {
			// #258/#374: a unit test on resources.APIFrontendConfigMap
			// alone proves the struct construction is correct, but not
			// that the reconcile loop actually persists it to the live
			// ConfigMap. #374's root cause was a hand-maintained
			// operator-side copy of these values silently drifting from
			// AF's own binary defaults (config.DefaultConfig()) when AF
			// added new tools; #258 reintroduces them as CRD-configurable
			// fields whose defaults are set to match AF's binary defaults
			// exactly, making the CRD the single source of truth.
			createBYOSecrets(ctx)
			kn := newCRWithRouteDisabled()
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())

			reconcileToDeployPhase(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apifrontend-config", Namespace: testNamespace}, cm)).To(Succeed())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("mcp:"))
			Expect(data).To(ContainSubstring("sessionIdleTimeout: 30m"),
				"CM-6: AF's live ConfigMap must render mcp.sessionIdleTimeout matching AF's own config.DefaultConfig() default")
			Expect(data).To(ContainSubstring("toolTimeout: 30s"),
				"CM-6: AF's live ConfigMap must render mcp.toolTimeout matching AF's own config.DefaultConfig() default")
		})
	})
})

// enabled is a package-level *bool for FleetSpec.Enabled (a pointer field).
var enabled = true

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// ======================================================================
// contentDrifted Unit Tests (TDD Red — Phase 1, Issue #16)
// ======================================================================

var _ = Describe("contentDrifted", func() {

	It("D1: should return true when a desired key has a different value in the live ConfigMap", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "desired-content"},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "tampered-content"},
		}
		Expect(contentDrifted(desired, existing)).To(BeTrue())
	})

	It("D2: should return false for a ConfigMap with empty desired Data (OCP inject-cabundle)", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "inter-service-ca",
				Namespace:   "default",
				Annotations: map[string]string{"service.beta.openshift.io/inject-cabundle": "true"},
			},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "inter-service-ca", Namespace: "default"},
			Data:       map[string]string{"service-ca.crt": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----"},
		}
		Expect(contentDrifted(desired, existing)).To(BeFalse())
	})

	It("D3: should return true when a desired key is absent from the live ConfigMap", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "content", "extra.yaml": "extra"},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "content"},
		}
		Expect(contentDrifted(desired, existing)).To(BeTrue())
	})

	It("D5: should return false for non-ConfigMap types (Deployment)", func() {
		desired := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
		}
		existing := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
		}
		Expect(contentDrifted(desired, existing)).To(BeFalse())
	})

	It("D6: should return false when live ConfigMap Data is nil but desired Data is empty", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		}
		Expect(contentDrifted(desired, existing)).To(BeFalse())
	})

	It("D7: should return true when desired BinaryData key differs in the live ConfigMap", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			BinaryData: map[string][]byte{"cert.pem": []byte("desired-bytes")},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			BinaryData: map[string][]byte{"cert.pem": []byte("tampered-bytes")},
		}
		Expect(contentDrifted(desired, existing)).To(BeTrue())
	})

	It("D6b: should return false when live ConfigMap has nil Data and desired has nil Data", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		}
		Expect(contentDrifted(desired, existing)).To(BeFalse())
	})

	It("D3b: should return true when desired BinaryData key is absent from live ConfigMap", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			BinaryData: map[string][]byte{"cert.pem": []byte("desired")},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			BinaryData: map[string][]byte{},
		}
		Expect(contentDrifted(desired, existing)).To(BeTrue())
	})

	It("D8: should not flag drift when live CM has extra keys not in desired (OCP injection safe)", func() {
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "content"},
		}
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
			Data:       map[string]string{"config.yaml": "content", "service-ca.crt": "injected-by-ocp"},
		}
		Expect(contentDrifted(desired, existing)).To(BeFalse())
	})
})
