package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/zapr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Only external API observations/failures are simulated. The predicates, write
// gates and conditional-delete production paths remain real and independent of
// the separate IT-OWN-514 Reconcile suite.
var _ = Describe("integrated ownership API boundaries", Label("unit"), func() {
	It("UT-AUDIT-515-OWNERSHIP-001: reports denied migration prerequisites and preserves the original conflict when status recording fails", func() {
		kn := newMinimalCR()
		kn.UID = "unit-ownership"
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		foreign := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-migrations", Namespace: kn.Namespace}}
		Expect(r.Create(ctx, foreign)).To(Succeed())
		_, err := r.phaseMigrate(ctx, kn)
		Expect(err).NotTo(HaveOccurred()) // failed prerequisites use the existing condition/requeue contract
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		Expect(r.Recorder.(*events.FakeRecorder).Events).To(Receive(ContainSubstring("OwnershipConflict")))
		before := foreign.DeepCopy()
		Expect(r.Get(ctx, client.ObjectKeyFromObject(foreign), foreign)).To(Succeed())
		Expect(foreign).To(Equal(before))

		r = businessUnitReconciler(kn, interceptor.Funcs{SubResourcePatch: deniedUnitStatus})
		refreshBusinessUnitCR(r, kn)
		core, logs := observer.New(zap.InfoLevel)
		tracedCtx := logf.IntoContext(ctx, zapr.NewLogger(zap.New(core)))
		Expect(r.checkResourceOwnership(tracedCtx, kn, foreign, true)).To(MatchError(ContainSubstring("ownership conflict")))
		Expect(logs.FilterMessage("recording ownership conflict status failed").Len()).To(Equal(1))
	})

	It("UT-AUDIT-515-OWNERSHIP-002: reports missing or unreadable workflow namespaces after a create race", func() {
		for _, denyRacedRead := range []bool{false, true} {
			kn := newMinimalCR()
			r := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, opts ...client.CreateOption) error {
				if _, namespace := object.(*corev1.Namespace); namespace {
					return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "namespaces"}, object.GetName())
				}
				return api.Create(ctx, object, opts...)
			}})
			err := r.deployWorkflowNamespace(ctx, kn)
			Expect(err).To(MatchError(ContainSubstring("reading raced workflow namespace")))
			if denyRacedRead {
				reads := 0
				r.APIReader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
					reads++
					if reads == 2 {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.Get(ctx, key, object, opts...)
				}})
				Expect(r.deployWorkflowNamespace(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
		}
	})

	It("UT-AUDIT-515-OWNERSHIP-003: diagnoses every owned cleanup failure instead of attempting deletion of absent or foreign resources", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		role, binding := resources.AnsibleRBAC(kn)
		bindingNames := resources.MonitoringCRBNames(kn)
		roleNames := append(resources.MonitoringClusterRoleNames(kn), resources.ToolClusterRoleNames(kn)...)
		objects := make([]client.Object, 0, 11+len(bindingNames)+len(roleNames))
		objects = append(objects, role, binding, resources.MutatingWebhookConfiguration(kn), resources.ValidatingWebhookConfiguration(kn),
			&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "data-storage-client-gateway", Namespace: kn.Namespace}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gateway-service", Namespace: kn.Namespace}},
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(resources.ComponentGateway), Namespace: kn.Namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "gateway-config", Namespace: kn.Namespace}},
			&monitoringv1alpha1.AlertmanagerConfig{ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-gateway-alerts", Namespace: kn.Namespace}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "fleetmetadatacache-service", Namespace: kn.Namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "fleetmetadatacache-config", Namespace: kn.Namespace}},
		)
		for _, name := range bindingNames {
			objects = append(objects, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		for _, name := range roleNames {
			objects = append(objects, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		seedBusinessOwnedResources(r, kn, objects...)
		Expect(r.pruneStaleGatewayRBAC(ctx, kn)).NotTo(BeEmpty())
		Expect(r.cleanupDisabledGateway(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		Expect(r.cleanupDisabledFleetMetadataCache(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		for _, cleanup := range []func(context.Context, *kubernautv1alpha2.Kubernaut) []error{
			r.deleteAnsibleClusterRBAC, r.deleteMonitoringClusterRBAC, r.deleteAdditionalAgentAndToolRBAC, r.deleteWebhookResources,
		} {
			Expect(cleanup(ctx, kn)).To(ContainElement(MatchError(ContainSubstring(unitAPIDenial))))
		}
	})

	It("UT-OWN-555-001 [AC-6, IA-5; SOC2 CC6.6; ASVS V4.1] rejects foreign webhook, service, and TLS material", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		webhook := resources.MutatingWebhookConfiguration(kn)
		webhook.Labels = nil
		Expect(r.Create(ctx, webhook)).To(Succeed())
		Expect(r.ensureWebhookConfiguration(ctx, kn, resources.MutatingWebhookConfiguration(kn))).To(MatchError(ContainSubstring("ownership conflict")))

		service := resources.Services(kn, kn)[0]
		service.Labels = nil
		service.Annotations = map[string]string{resources.OCPServingCertAnnotation: "foreign-serving-key"}
		Expect(r.Create(ctx, service)).To(Succeed())
		Expect(r.clearStaleServingCertErrors(ctx, kn, service)).To(MatchError(ContainSubstring("ownership conflict")))

		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{InternalCASecretName: "foreign-ca"}
		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		foreignSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: material.InternalCASecretName, Namespace: kn.Namespace}}
		Expect(r.Create(ctx, foreignSecret)).To(Succeed())
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("missing ca.crt")))
	})

	It("UT-AUDIT-515-OWNERSHIP-005: authorizes migration metadata repair and surfaces failed writes for migration and cert-manager objects", func() {
		kn := certManagerLifecycleCR("Issuer")
		kn.UID = "unit-migration-owner"
		r := businessUnitReconciler(kn, interceptor.Funcs{Update: deniedUnitUpdate})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		legacy := job.DeepCopy()
		legacy.Labels, legacy.Annotations = nil, nil
		Expect(r.Create(ctx, legacy)).To(Succeed())
		_, err = r.createIfNotFound(ctx, kn, job.DeepCopy(), job.DeepCopy())
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		Expect(r.Delete(ctx, legacy)).To(Succeed())
		legacy.ResourceVersion = ""
		resources.StampOwnership(kn, legacy)
		Expect(r.Create(ctx, legacy)).To(Succeed())
		_, err = r.createIfNotFound(ctx, kn, job.DeepCopy(), job.DeepCopy())
		Expect(err).To(MatchError(ContainSubstring("repairing migration ownership")))
		objects, err := resources.CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		live := objects[0].DeepCopy()
		Expect(resources.SetOwnerReference(kn, live, r.Scheme)).To(Succeed())
		seedBusinessOwnedResources(r, kn, live)
		Expect(r.ensureCertManagerResource(ctx, kn, objects[0].DeepCopy())).To(MatchError(ContainSubstring(unitAPIDenial)))
		Expect(mergeStringSlice(nil, nil)).To(BeNil())
		Expect(mergeStringSlice([]string{"external"}, []string{"external", "operator"})).To(Equal([]string{"external", "operator"}))
	})

	It("UT-AUDIT-515-OWNERSHIP-006: handles deletion disappearance and metadata/kind fallbacks without bypassing authorization", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "disappeared")
		}})
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "disappeared", Namespace: kn.Namespace}}
		seedBusinessOwnedResources(r, kn, object)
		Expect(r.deleteIfExists(ctx, kn, object)).To(Succeed())
		desired := object.DeepCopy()
		desired.Labels = map[string]string{"app.kubernetes.io/instance": "other"}
		Expect(ownershipMetadataMatches(desired, object)).To(BeFalse())
		r.Scheme = runtime.NewScheme()
		Expect(r.resourceKind(ctx, object)).To(Equal("*v1.ConfigMap"))
		r.Scheme = nil
		Expect(r.resourceKind(ctx, object)).To(Equal("*v1.ConfigMap"))
	})
})
