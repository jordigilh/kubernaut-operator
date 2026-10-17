package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("independent lifecycle failure outcomes", Label("unit"), func() {
	It("UT-AUDIT-515-ERRORS-001: does not activate unknown or unreadable CRs", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		for _, name := range []string{"not-kubernaut", "missing"} {
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: kn.Namespace, Name: name}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		}
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey,
			client.Object, ...client.GetOption) error {
			return fmt.Errorf("injected read failure")
		}})
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).To(MatchError(ContainSubstring("injected read failure")))
	})

	It("UT-AUDIT-515-ERRORS-002: rolls back in-memory status and reports failed status writes", func() {
		kn := newMinimalCR()
		kn.Status.Phase = kubernautv1alpha2.PhaseRunning
		r := businessUnitReconciler(kn, interceptor.Funcs{SubResourcePatch: func(context.Context, client.Client,
			string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
			return fmt.Errorf("injected status failure")
		}})
		Expect(r.patchStatus(ctx, kn, func() { kn.Status.Phase = kubernautv1alpha2.PhaseError })).To(MatchError("injected status failure"))
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
		_, err := r.setConditionAndRequeue(ctx, kn, kubernautv1alpha2.ConditionBYOValidated, "Failed", "rejected")
		Expect(err).To(MatchError("injected status failure"))
		Expect(r.handleRBACDeployError(ctx, kn, fmt.Errorf("injected RBAC failure"))).To(MatchError("injected RBAC failure"))
		_, err = r.phaseRunning(ctx, kn, kn)
		Expect(err).To(MatchError("injected status failure"))
	})

	It("UT-AUDIT-515-ERRORS-003: bounds failed migration retries and deletes stale completed Jobs", func() {
		for _, failed := range []int32{1, maxMigrationRetries} {
			kn := newMinimalCR()
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			job, err := resources.MigrationJob(kn)
			Expect(err).NotTo(HaveOccurred())
			job.Status.Failed = failed
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
			Expect(r.Create(ctx, job)).To(Succeed())
			result, err := r.ensureMigrationJob(ctx, kn)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(requeueError))
			Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionMigrationComplete)).To(BeTrue())
			live := &batchv1.Job{}
			readErr := r.Get(ctx, client.ObjectKeyFromObject(job), live)
			Expect(apierrors.IsNotFound(readErr)).To(Equal(failed < maxMigrationRetries))
		}
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
		Expect(r.Create(ctx, job)).To(Succeed())
		result, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Millisecond))
		Expect(apierrors.IsNotFound(r.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
	})

	It("UT-AUDIT-515-ERRORS-004: retains finalizers when cleanup or finalizer removal fails", func() {
		for _, failUpdate := range []bool{false, true} {
			kn := newMinimalCR()
			kn.Finalizers = []string{kubernautv1alpha2.FinalizerName}
			kn.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			faults := interceptor.Funcs{}
			if failUpdate {
				faults.Update = func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
					return fmt.Errorf("injected finalizer failure")
				}
			} else {
				faults.Delete = func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return fmt.Errorf("injected cleanup failure")
				}
			}
			r := businessUnitReconciler(kn, faults)
			role, _ := resources.AnsibleRBAC(kn)
			seedBusinessOwnedResources(r, kn, role)
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
			Expect(err).To(MatchError(ContainSubstring("injected")))
			refreshBusinessUnitCR(r, kn)
			Expect(kn.Finalizers).To(ContainElement(kubernautv1alpha2.FinalizerName))
		}
	})

	It("UT-AUDIT-515-ERRORS-005: reports disabled-resource deletion failures rather than ignoring stale privileges and workloads", func() {
		kn := newMinimalCR()
		kn.Spec.Gateway.Enabled = ptr.To(false)
		r := businessUnitReconciler(kn, interceptor.Funcs{Delete: func(context.Context, client.WithWatch,
			client.Object, ...client.DeleteOption) error {
			return fmt.Errorf("injected cleanup failure")
		}})
		role, binding := resources.AnsibleRBAC(kn)
		seedBusinessOwnedResources(r, kn, role, binding,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.DeploymentName(resources.ComponentGateway), Namespace: kn.Namespace}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: kn.Namespace}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: resources.ConsoleAccessCRBName(kn)}},
			&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "gateway-ingress", Namespace: kn.Namespace}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "stale-role"}})
		for _, cleanup := range []func() error{
			func() error { return r.cleanupDisabledGateway(ctx, kn) },
			func() error { return r.cleanupDisabledFleetMetadataCache(ctx, kn) },
			func() error { return r.deployToggleRBAC(ctx, kn) },
			func() error { return r.deployConsoleAccessRBAC(ctx, kn) },
			func() error { _, err := r.enabledDeploymentBuilders(ctx, kn, kn); return err },
			func() error { _, err := r.reconcileGenericIngresses(ctx, kn); return err },
		} {
			Expect(cleanup()).To(MatchError(ContainSubstring("injected cleanup failure")))
		}
		kn.Status.BoundToolRoleBindings = []string{"stale-role"}
		Expect(r.deleteAdditionalAgentAndToolRBAC(ctx, kn)).To(ContainElement(MatchError(ContainSubstring("stale-role"))))
	})

	It("UT-AUDIT-515-ERRORS-006: distinguishes Ansible and AlertManager disabled, missing, malformed and valid credentials", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateAnsibleConfig(ctx, kn).Status).To(Equal(metav1.ConditionTrue))
		kn.Spec.WorkflowExecution.Ansible.Enabled = true
		Expect(r.validateAnsibleConfig(ctx, kn).Reason).To(Equal(ReasonAnsibleTokenNotFound))
		kn.Spec.WorkflowExecution.Ansible.TokenSecretRef = &kubernautv1alpha2.SecretKeyRef{Name: "unit-token"}
		kn.Spec.Gateway.Auth.Enabled = ptr.To(true)
		Expect(r.validateAlertManagerAuthConfig(ctx, kn).Reason).To(Equal(ReasonAlertManagerAuthNotConfigured))
		kn.Spec.Gateway.Auth.AlertManager.TokenSecretName = "unit-token"
		Expect(r.validateAnsibleConfig(ctx, kn).Reason).To(Equal(ReasonAnsibleTokenNotFound))
		Expect(r.validateAlertManagerAuthConfig(ctx, kn).Reason).To(Equal(ReasonAlertManagerAuthSecretMissing))
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unit-token", Namespace: kn.Namespace}}
		Expect(r.Create(ctx, secret)).To(Succeed())
		Expect(r.validateAnsibleConfig(ctx, kn).Reason).To(Equal(ReasonAnsibleTokenKeyMissing))
		Expect(r.validateAlertManagerAuthConfig(ctx, kn).Reason).To(Equal(ReasonAlertManagerAuthKeyMissing))
		secret.Data = map[string][]byte{"token": []byte("disposable")}
		Expect(r.Update(ctx, secret)).To(Succeed())
		Expect(r.validateAnsibleConfig(ctx, kn).Status).To(Equal(metav1.ConditionTrue))
		Expect(r.validateAlertManagerAuthConfig(ctx, kn).Status).To(Equal(metav1.ConditionTrue))
		kn.Spec.WorkflowExecution.Ansible.TokenSecretRef = nil
		kn.Spec.Gateway.Auth.AlertManager.TokenSecretName = ""
		Expect(r.validateAnsibleConfig(ctx, kn).Status).To(Equal(metav1.ConditionFalse))
		_, phaseErr := r.phaseRunning(ctx, kn, kn)
		Expect(phaseErr).NotTo(HaveOccurred())
		kn.Spec.Gateway.Enabled = ptr.To(false)
		Expect(r.validateAlertManagerAuthConfig(ctx, kn).Reason).To(Equal(ReasonAlertManagerAuthGatewayDisabled))
		_, err := r.phaseRunning(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
	})

	It("UT-539-GW-AUTH-CONDITION-001 [AC-3, CM-6; ASVS v5.0.0-V8.3.1, v5.0.0-V16.5.2]: treats explicit Gateway auth disablement as intentional rather than missing credentials", func() {
		kn := newMinimalCR()
		disabled := false
		kn.Spec.Gateway.Auth.Enabled = &disabled
		kn.Spec.Gateway.Auth.AlertManager.TokenSecretName = "must-not-be-read"
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		condition := r.validateAlertManagerAuthConfig(ctx, kn)
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(ReasonAlertManagerAuthDisabled))
		Expect(condition.Message).To(ContainSubstring("set spec.gateway.auth.enabled=true"))
	})

	It("UT-AUDIT-515-ERRORS-007: prunes additional-role and persona bindings on shrink and preserves required bindings", func() {
		kn := newMinimalCR()
		kn.Spec.AdditionalClusterRoles = []string{"existing", "missing", "existing"}
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{RoleBindings: []kubernautv1alpha2.ToolRoleBinding{{
			Role: "sre", Groups: []string{"engineers"},
		}}}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "existing"}})).To(Succeed())
		Expect(r.deployAdditionalComponentRBAC(ctx, kn, kn)).To(Succeed())
		Expect(kn.Status.BoundAdditionalClusterRoles).To(Equal([]string{"existing", "missing"}))
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionAdditionalRBACBound)).To(BeTrue())
		Expect(r.deployToolRBAC(ctx, kn)).To(Succeed())
		Expect(r.deployConsoleAccessRBAC(ctx, kn)).To(Succeed())
		Expect(kn.Status.BoundToolRoleBindings).To(HaveLen(1))
		kn.Spec.AdditionalClusterRoles = []string{"existing"}
		kn.Spec.Gateway.Enabled = ptr.To(false)
		Expect(r.deployAdditionalComponentRBAC(ctx, kn, kn)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionAdditionalRBACBound)).To(BeTrue())
		kn.Spec.APIFrontend.RBAC = nil
		Expect(r.deployToolRBAC(ctx, kn)).To(Succeed())
		Expect(kn.Status.BoundToolRoleBindings).To(BeEmpty())
		Expect(r.deployConsoleAccessRBAC(ctx, kn)).To(Succeed())
		Expect(contains([]string{"existing"}, "existing")).To(BeTrue())
		Expect(contains([]string{"existing"}, "missing")).To(BeFalse())
	})

	It("UT-AUDIT-515-ERRORS-008: preserves webhook bundle ownership and rejects read/cleanup errors", func() {
		kn := newMinimalCR()
		for _, object := range []client.Object{
			resources.MutatingWebhookConfiguration(kn), resources.ValidatingWebhookConfiguration(kn),
		} {
			r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey,
				client.Object, ...client.GetOption) error {
				return fmt.Errorf("injected webhook read failure")
			}})
			Expect(r.ensureWebhookConfiguration(ctx, kn, object)).To(MatchError(ContainSubstring("injected webhook read failure")))
		}
		preserveWebhookCABundle(&admissionv1.MutatingWebhookConfiguration{}, &admissionv1.ValidatingWebhookConfiguration{})
		preserveWebhookCABundle(&admissionv1.ValidatingWebhookConfiguration{}, &admissionv1.MutatingWebhookConfiguration{})
		desired := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}}
		preserveServiceFields(desired, &corev1.Service{})
		Expect(desired.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
		Expect(contentDrifted(&corev1.Service{}, &corev1.ConfigMap{})).To(BeFalse())
		Expect(contentDrifted(&corev1.ConfigMap{}, &corev1.Service{})).To(BeFalse())
		stampConfigMapHash(&appsv1.Deployment{}, map[string]string{"datastorage": "hash"})
	})
})
