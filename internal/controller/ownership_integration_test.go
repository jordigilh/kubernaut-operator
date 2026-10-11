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

	"github.com/go-logr/zapr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("Issue 514 ownership safety through real reconciliation", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-OWN-514-001 [AC-3, AC-6, AU-3, SI-4; SOC2 CC6.1, CC7.2; ASVS v5.0.0-V8.3.1, v5.0.0-V16.2.1] preserves an administrator's derived-secret name and reports the conflict", func() {
		createBYOSecrets(ctx)
		foreign := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "datastorage-db-secret", Namespace: testNamespace},
			Data:       map[string][]byte{"db-secrets.yaml": []byte("administrator-private-data")},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		core, logs := observer.New(zap.InfoLevel)
		tracedCtx := logf.IntoContext(ctx, zapr.NewLogger(zap.New(core)))
		_, err := r.Reconcile(tracedCtx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(tracedCtx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred()) // migration prereq errors become existing status/events

		live := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before), "a failed install must not take over administrator credentials")
		kn := fetchKnV2(ctx)
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionCRDsInstalled)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Message).To(ContainSubstring("ownership conflict"))
		Expect(condition.Message).To(ContainSubstring("Secret"))
		Expect(condition.Message).NotTo(ContainSubstring("administrator-private-data"))
		Eventually(r.Recorder.(*events.FakeRecorder).Events, timeout, interval).Should(Receive(ContainSubstring("OwnershipConflict")))
		entries := logs.FilterMessage("resource ownership conflict").All()
		Expect(entries).NotTo(BeEmpty())
		fields := entries[0].ContextMap()
		Expect(fields).To(HaveKeyWithValue("kind", "Secret"))
		Expect(fields).To(HaveKeyWithValue("name", foreign.Name))
		Expect(fields).To(HaveKeyWithValue("namespace", testNamespace))
		Expect(fields).To(HaveKeyWithValue("generation", kn.Generation))
		Expect(fields).To(HaveKey("resourceVersion"))
	})

	It("IT-OWN-514-002 [AC-6, SI-10; SOC2 CC6.6; ASVS v5.0.0-V8.2.1] rejects a same-named administrator ClusterRole before deployment and preserves it on failed-install uninstall", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		foreign := resources.ClusterRoles(kn, kn)[0]
		foreign.Labels = nil
		foreign.Rules = []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		markMigrationJobComplete(ctx)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		current := fetchKnV2(ctx)
		Expect(meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionRBACProvisioned).Status).To(Equal(metav1.ConditionFalse))
		Expect(k8sClient.Delete(ctx, current)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		live := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-003 [CM-3, CM-6; SOC2 CC8.1] creates owned resources, upgrades them and reconciles idempotently", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)
		kn := fetchKnV2(ctx)
		kn.Spec.Image.Overrides = map[string]string{"gateway": "example.com/gateway:upgrade"}
		Expect(k8sClient.Update(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		dep := &appsv1.Deployment{}
		key := types.NamespacedName{Name: "gateway", Namespace: testNamespace}
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/gateway:upgrade"))
		Expect(metav1.IsControlledBy(dep, kn)).To(BeTrue())
		Expect(dep.Annotations).To(HaveKeyWithValue("kubernaut.ai/owner-namespace", kn.Namespace))
		Expect(dep.Annotations).To(HaveKeyWithValue("kubernaut.ai/owner-uid", string(kn.UID)))
		version := dep.ResourceVersion
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		Expect(dep.ResourceVersion).To(Equal(version))
	})

	It("IT-OWN-514-004 [AC-6, CM-3; SOC2 CC8.1] repairs fully marked reinstall leftovers only for the same Kubernaut identity", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)
		old := fetchKnV2(ctx)
		Expect(k8sClient.Delete(ctx, old)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, singletonKey(), &kubernautv1alpha2.Kubernaut{})).To(MatchError(ContainSubstring("not found")))
		// Envtest deliberately leaves owned children behind (no GC): exercise a
		// new UID against those marked leftovers, not fabricated desired objects.
		fresh := newCRWithRouteDisabled()
		Expect(k8sClient.Create(ctx, fresh)).To(Succeed())
		Expect(fresh.UID).NotTo(Equal(old.UID))
		for range 3 {
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		live := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gateway-config", Namespace: testNamespace}, live)).To(Succeed())
		Expect(metav1.IsControlledBy(live, fresh)).To(BeTrue())
		Expect(live.Annotations).To(HaveKeyWithValue("kubernaut.ai/owner-uid", string(fresh.UID)))
	})

	It("IT-OWN-514-005 [AC-3, AC-6; SOC2 CC6.6] preserves an unmanaged stale exposure object during deploy pruning", func() {
		createBYOSecrets(ctx)
		foreign := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-ingress", Namespace: testNamespace},
			Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: "administrator-service", Port: networkingv1.ServiceBackendPort{Number: 443},
			}}},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)
		live := &networkingv1.Ingress{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-006 [AC-6; SOC2 CC6.1] does not treat partial pruning labels as deletion authorization", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)
		foreign := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "administrator-partial-marker", Labels: map[string]string{
			resources.LabelCoreClusterRBAC: "true", "app.kubernetes.io/instance": "kubernaut", "app.kubernetes.io/managed-by": "platform",
		}}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		kn := fetchKnV2(ctx)
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		live := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-007 [AC-6, IA-5; ASVS v5.0.0-V13.3.1] rejects unowned development CA material before reusing it", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeDevelopmentSelfSigned
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		secrets, err := resources.DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now().UTC())
		Expect(err).NotTo(HaveOccurred())
		foreign := secrets[0]
		foreign.Labels = nil
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		live := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
		Expect(fetchKnV2(ctx).Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
	})

	It("IT-OWN-514-010 [AC-6; SOC2 CC6.6] does not cascade-delete a preserved administrator runner by deleting its workflow namespace", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.WorkflowExecution.WorkflowNamespace = "ownership-514-workflows"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := reconcileToRunning(ctx)
		runner := resources.WorkflowRunnerServiceAccount(kn)
		Expect(k8sClient.Delete(ctx, runner)).To(Succeed())
		runner.Labels = nil
		Expect(k8sClient.Create(ctx, runner)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, runner))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: runner.Namespace}}))).To(Succeed())
		})
		kn = fetchKnV2(ctx)
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: runner.Namespace}, namespace)).To(Succeed())
		Expect(namespace.DeletionTimestamp).To(BeNil(), "deleting the namespace would destroy the protected foreign ServiceAccount on a live cluster")
	})

	It("IT-OWN-514-012 [AC-6, CM-6] requires administrator preparation of an unmarked workflow namespace instead of modifying it", func() {
		createBYOSecrets(ctx)
		foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ownership-514-admin", Labels: map[string]string{"team": "platform"}}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		kn := newCRWithRouteDisabled()
		kn.Spec.WorkflowExecution.WorkflowNamespace = foreign.Name
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		markMigrationJobComplete(ctx)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		live := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-013 [AC-6, AU-3, CM-3; SOC2 CC6.6, CC7.2] refuses a shared CRD ownership conflict through migration even when the hash matches", func() {
		createBYOSecrets(ctx)
		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())
		foreign := &apiextensionsv1.CustomResourceDefinition{}
		key := client.ObjectKey{Name: "remediationrequests.kubernaut.ai"}
		Expect(k8sClient.Get(ctx, key, foreign)).To(Succeed())
		original := foreign.DeepCopy()
		DeferCleanup(func() {
			live := &apiextensionsv1.CustomResourceDefinition{}
			Expect(k8sClient.Get(ctx, key, live)).To(Succeed())
			live.Labels = original.Labels
			Expect(k8sClient.Update(ctx, live)).To(Succeed())
		})
		foreign.Labels = map[string]string{"app.kubernetes.io/managed-by": "Helm"}
		Expect(k8sClient.Update(ctx, foreign)).To(Succeed())
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		live := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, key, live)).To(Succeed())
		Expect(live).To(Equal(before))
		condition := meta.FindStatusCondition(fetchKnV2(ctx).Status.Conditions, kubernautv1alpha2.ConditionCRDsInstalled)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Message).To(ContainSubstring("CustomResourceDefinition"))
		Expect(condition.Message).To(ContainSubstring("Helm"))
		Eventually(r.Recorder.(*events.FakeRecorder).Events, timeout, interval).Should(Receive(ContainSubstring("OwnershipConflict")))
	})

	It("IT-OWN-514-014 [AC-6, SI-10; ASVS v5.0.0-V8.3.1] reauthorizes existing resources before a matching-hash no-op", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)
		foreign := &corev1.ConfigMap{}
		key := client.ObjectKey{Namespace: testNamespace, Name: "gateway-config"}
		Expect(k8sClient.Get(ctx, key, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		foreign.Labels = nil
		foreign.OwnerReferences = nil
		delete(foreign.Annotations, resources.AnnotationOwnerUID)
		delete(foreign.Annotations, resources.AnnotationOwnerNamespace)
		Expect(k8sClient.Update(ctx, foreign)).To(Succeed())
		before := foreign.DeepCopy()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		live := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, key, live)).To(Succeed())
		Expect(live).To(Equal(before))
		kn := fetchKnV2(ctx)
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		Expect(meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionServicesDeployed).Status).To(Equal(metav1.ConditionFalse))
	})

	DescribeTable("IT-OWN-514-011 [AC-6, CM-3; ASVS v5.0.0-V8.3.1] refuses deletion after the authorized observation changes",
		func(changeOwnership bool) {
			createBYOSecrets(ctx)
			kn := newCRWithRouteDisabled()
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())
			r := reconcileToRunning(ctx)
			owned := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "gateway-ingress", Namespace: testNamespace, Labels: resources.CommonLabels(kn)},
				Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
					Name: "owned", Port: networkingv1.ServiceBackendPort{Number: 443},
				}}},
			}
			Expect(k8sClient.Create(ctx, owned)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: owned.Name, Namespace: owned.Namespace}}))).To(Succeed())
			})
			r.Client = &ownershipDeleteRaceClient{Client: k8sClient, key: client.ObjectKeyFromObject(owned), changeOwnership: changeOwnership}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "a failed precondition must stop a stale authorized delete: %v", err)
			live := &networkingv1.Ingress{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(owned), live)).To(Succeed())
			Expect(live.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "administrator"))
		},
		Entry("replacement with a new UID", false),
		Entry("same UID but changed ownership/version", true),
	)

	It("IT-OWN-514-016 [AC-6, CM-8; SOC2 CC6.6] rejects a workflow namespace claimed by another Kubernaut namespace even with restricted PSA", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.WorkflowExecution.WorkflowNamespace = "ownership-514-other-identity"
		foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: kn.Spec.WorkflowExecution.WorkflowNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":       "kubernaut-operator",
				"app.kubernetes.io/part-of":          "kubernaut",
				"app.kubernetes.io/instance":         "kubernaut",
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "restricted",
			},
			Annotations: map[string]string{"kubernaut.ai/owner-namespace": "other-kubernaut-system", "kubernaut.ai/owner-uid": "other-cr-uid"},
		}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		markMigrationJobComplete(ctx)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		live := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: foreign.Name, Name: "kubernaut-workflow-runner"}, &corev1.ServiceAccount{})).To(MatchError(ContainSubstring("not found")))
	})

	It("IT-OWN-514-017 [AC-6, CM-6; SOC2 CC6.6] reuses a prepared administrator workflow namespace without adopting, mutating or deleting it", func() {
		createBYOSecrets(ctx)
		foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "ownership-514-prepared-admin",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":       "administrator",
				"team":                               "platform",
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "restricted",
			},
		}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		before := foreign.DeepCopy()
		kn := newCRWithRouteDisabled()
		kn.Spec.WorkflowExecution.WorkflowNamespace = foreign.Name
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := reconcileToRunning(ctx)
		live := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before), "read-only namespace reuse must not stamp operator provenance")
		kn = fetchKnV2(ctx)
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-015 [AC-6; SOC2 CC6.6] preserves administrator content in an operator-created workflow namespace on uninstall", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.WorkflowExecution.WorkflowNamespace = "ownership-514-content"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := reconcileToRunning(ctx)
		foreign := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "administrator-config", Namespace: kn.Spec.WorkflowExecution.WorkflowNamespace},
			Data:       map[string]string{"setting": "preserve"},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: foreign.Namespace}}))).To(Succeed())
		})
		before := foreign.DeepCopy()
		kn = fetchKnV2(ctx)
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, singletonKey(), &kubernautv1alpha2.Kubernaut{})).To(MatchError(ContainSubstring("not found")))
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: foreign.Namespace}, namespace)).To(Succeed())
		Expect(namespace.DeletionTimestamp).To(BeNil(), "namespace deletion would cascade beyond the ownership contract on a live cluster")
		live := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(before))
	})

	It("IT-OWN-514-008 [AC-6, SI-10; ASVS v5.0.0-V8.3.1] refuses a completed administrator migration Job through Reconcile", func() {
		createBYOSecrets(ctx)
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-db-migration", Namespace: testNamespace},
			Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "administrator", Image: "busybox:stable", Command: []string{"true"}}},
			}}},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, job))).To(Succeed()) })
		markMigrationJobComplete(ctx)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
		before := job.DeepCopy()
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		live := &batchv1.Job{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), live)).To(Succeed())
		Expect(live).To(Equal(before), "administrator Job status is not evidence of the operator's migration")
		kn := fetchKnV2(ctx)
		Expect(kn.Status.LastMigrationHash).To(BeEmpty())
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionMigrationComplete)).To(BeFalse())
	})

	It("IT-OWN-514-008 [AC-6, SI-10; ASVS v5.0.0-V8.3.1] refuses an administrator cert-manager Certificate through Reconcile", func() {
		createBYOSecrets(ctx)
		// Only the external cert-manager API is simulated. Builders and the full
		// TLS/migration reconciliation path run against the envtest API server.
		for _, names := range []struct{ kind, plural, singular string }{
			{kind: "Issuer", plural: "issuers", singular: "issuer"},
			{kind: "Certificate", plural: "certificates", singular: "certificate"},
		} {
			crd := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: names.plural + ".cert-manager.io"},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Group: "cert-manager.io", Scope: apiextensionsv1.NamespaceScoped,
					Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: names.kind, Plural: names.plural, Singular: names.singular},
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
						Name: "v1", Served: true, Storage: true,
						Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object", XPreserveUnknownFields: new(true),
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, crd)).To(Succeed())
			DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, crd))).To(Succeed()) })
			Eventually(func(g Gomega) {
				live := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), live)).To(Succeed())
				g.Expect(live.Status.Conditions).To(ContainElement(And(
					HaveField("Type", apiextensionsv1.Established), HaveField("Status", apiextensionsv1.ConditionTrue),
				)))
			}, timeout, interval).Should(Succeed())
		}
		issuer := &unstructured.Unstructured{}
		Expect(issuer.UnmarshalJSON([]byte(`{"apiVersion":"cert-manager.io/v1","kind":"Issuer","metadata":{"name":"administrator-issuer"},"spec":{"selfSigned":{}}}`))).To(Succeed())
		issuer.SetNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, issuer)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, issuer))).To(Succeed()) })
		certificate := &unstructured.Unstructured{}
		Expect(certificate.UnmarshalJSON([]byte(`{"apiVersion":"cert-manager.io/v1","kind":"Certificate","metadata":{"name":"gateway-tls"},"spec":{"secretName":"administrator-tls","issuerRef":{"name":"administrator-issuer"}}}`))).To(Succeed())
		certificate.SetNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, certificate)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, certificate))).To(Succeed()) })
		before := certificate.DeepCopy()
		kn := newCRWithRouteDisabled()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeHelmCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{IssuerRef: kubernautv1alpha2.TLSIssuerRef{
				Name: issuer.GetName(), Kind: "Issuer", Group: "cert-manager.io",
			}},
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(certificate.GroupVersionKind())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(certificate), live)).To(Succeed())
		Expect(live.Object).To(Equal(before.Object))
		kn = fetchKnV2(ctx)
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionTLSReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Message).To(ContainSubstring("ownership conflict"))
		Expect(condition.Message).To(ContainSubstring("Certificate"))
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionTLSReady)).To(BeFalse())
		Eventually(r.Recorder.(*events.FakeRecorder).Events, timeout, interval).Should(Receive(ContainSubstring("OwnershipConflict")))
	})

	It("IT-OWN-514-009 [AC-6, SI-10] rejects a generic create race through Reconcile even with a forged matching spec hash", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		foreign := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-config", Namespace: testNamespace},
			Data:       map[string]string{"administrator": "preserve"},
		}
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
		r := newReconciler()
		race := &ownershipCreateRaceClient{Client: k8sClient, foreign: foreign}
		r.Client = race
		r.APIReader = k8sClient
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		markMigrationJobComplete(ctx)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		Expect(race.raced).To(BeTrue())
		live := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live).To(Equal(foreign), "the AlreadyExists reread must preserve all administrator content and metadata")
		Expect(live.Annotations[resources.AnnotationSpecHash]).NotTo(BeEmpty(), "even a matching forged hash is not authorization")
		Expect(fetchKnV2(ctx).Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
	})

	It("IT-OWN-514-018 [AC-6, IA-5; ASVS v5.0.0-V8.3.1] refuses foreign webhook trust before preservation even when a later read would be owned", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)
		key := client.ObjectKey{Name: testNamespace + "-authwebhook-mutating"}
		owned := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(k8sClient.Get(ctx, key, owned)).To(Succeed())
		foreign := owned.DeepCopy()
		foreign.Labels = nil
		foreign.Annotations = nil
		foreign.Webhooks[0].ClientConfig.CABundle = []byte("administrator-trust-must-not-be-reused")
		Expect(k8sClient.Update(ctx, foreign)).To(Succeed())
		// An administrator transfers ownership after this observation. Simulate
		// a stale cache returning the prior foreign CA on the first webhook read;
		// subsequent reads use the real envtest API and its newly owned object.
		owned.ResourceVersion = foreign.ResourceVersion
		Expect(k8sClient.Update(ctx, owned)).To(Succeed())
		before := owned.DeepCopy()
		race := &ownershipWebhookReadRaceClient{Client: k8sClient, foreign: foreign}
		r.Client = race
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(race.servedStale).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		live := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(k8sClient.Get(ctx, key, live)).To(Succeed())
		Expect(live).To(Equal(before), "foreign trust cannot be copied into an authorized replacement observation")
	})

	DescribeTable("IT-OWN-514-019 [AC-3, AC-6; SOC2 CC6.6; ASVS v5.0.0-V8.3.1] enforces provider policy owner identity despite matching markers and hashes",
		func(operation string) {
			createBYOSecrets(ctx)
			waitForCiliumPolicyCRDDeletion(ctx)
			crd, daemonSet := ciliumPolicyCRD(), ciliumProviderDaemonSet()
			Expect(k8sClient.Create(ctx, crd)).To(Succeed())
			Expect(k8sClient.Create(ctx, daemonSet)).To(Succeed())
			daemonSet.Status.NumberReady = 1
			Expect(k8sClient.Status().Update(ctx, daemonSet)).To(Succeed())
			DeferCleanup(func() {
				Expect(newReconciler().deleteProviderPolicies(ctx, testNamespace, kubernautv1alpha2.SingletonName)).To(BeEmpty())
				cleanupProviderFixture(ctx, daemonSet, crd)
			})
			kn := newCRWithRouteDisabled()
			kn.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())
			r := reconcileToRunning(ctx)
			foreign := &unstructured.Unstructured{}
			foreign.SetAPIVersion("cilium.io/v2")
			foreign.SetKind("CiliumNetworkPolicy")
			key := client.ObjectKey{Name: agentPolicyName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, key, foreign)).To(Succeed())
			foreign.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "platform-policy-owner", UID: types.UID("platform-uid")}})
			if operation == "repair" {
				foreign.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "kubernaut.ai/v1alpha2", Kind: "Kubernaut", Name: kn.Name, UID: types.UID("old-install-uid"), Controller: new(true), BlockOwnerDeletion: new(true)}})
			}
			if operation == "prune" {
				foreign.SetName(agentPolicyName + "-stale")
				foreign.SetUID("")
				foreign.SetResourceVersion("")
				foreign.SetManagedFields(nil)
				Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
			} else {
				Expect(k8sClient.Update(ctx, foreign)).To(Succeed())
			}
			DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, foreign))).To(Succeed()) })
			before := foreign.DeepCopy()
			if operation == "finalize" {
				Expect(k8sClient.Delete(ctx, fetchKnV2(ctx))).To(Succeed())
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			if operation == "update" {
				Expect(err).To(MatchError(ContainSubstring("owner")))
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
			live := &unstructured.Unstructured{}
			live.SetGroupVersionKind(foreign.GroupVersionKind())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
			if operation == "repair" {
				Expect(metav1.IsControlledBy(live, fetchKnV2(ctx))).To(BeTrue())
				Expect(live.GetLabels()).To(Equal(before.GetLabels()))
				Expect(live.Object["spec"]).To(Equal(before.Object["spec"]))
				Expect(live.GetAnnotations()[resources.AnnotationSpecHash]).To(Equal(before.GetAnnotations()[resources.AnnotationSpecHash]))
			} else {
				Expect(live).To(Equal(before))
			}
			if operation == "finalize" {
				Expect(k8sClient.Get(ctx, singletonKey(), &kubernautv1alpha2.Kubernaut{})).To(MatchError(ContainSubstring("not found")))
			}
		},
		Entry("reconciliation", "update"),
		Entry("stale policy pruning", "prune"),
		Entry("finalizer cleanup", "finalize"),
		Entry("same-identity reinstall owner repair", "repair"),
	)
})

type ownershipWebhookReadRaceClient struct {
	client.Client
	foreign     *admissionregistrationv1.MutatingWebhookConfiguration
	servedStale bool
}

func (c *ownershipWebhookReadRaceClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if webhook, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration); ok &&
		!c.servedStale && key == client.ObjectKeyFromObject(c.foreign) {
		c.servedStale = true
		*webhook = *c.foreign.DeepCopy()
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type ownershipCreateRaceClient struct {
	client.Client
	foreign client.Object
	raced   bool
}

func (c *ownershipCreateRaceClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*corev1.ConfigMap); !ok || c.raced || client.ObjectKeyFromObject(obj) != client.ObjectKeyFromObject(c.foreign) {
		return c.Client.Create(ctx, obj, opts...)
	}
	c.raced = true
	c.foreign.SetAnnotations(map[string]string{resources.AnnotationSpecHash: obj.GetAnnotations()[resources.AnnotationSpecHash]})
	if err := c.Client.Create(ctx, c.foreign); err != nil {
		return err
	}
	return apierrors.NewAlreadyExists(corev1.Resource("configmaps"), obj.GetName())
}

type ownershipDeleteRaceClient struct {
	client.Client
	key             client.ObjectKey
	changeOwnership bool
	raced           bool
}

func (c *ownershipDeleteRaceClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if !c.raced && client.ObjectKeyFromObject(obj) == c.key {
		c.raced = true
		foreign := obj.DeepCopyObject().(client.Object)
		foreign.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "administrator"})
		if c.changeOwnership {
			if err := c.Update(ctx, foreign); err != nil {
				return err
			}
		} else {
			if err := c.Client.Delete(ctx, obj); err != nil {
				return err
			}
			foreign.SetUID("")
			foreign.SetResourceVersion("")
			foreign.SetManagedFields(nil)
			if err := c.Create(ctx, foreign); err != nil {
				return err
			}
		}
	}
	return c.Client.Delete(ctx, obj, opts...)
}
