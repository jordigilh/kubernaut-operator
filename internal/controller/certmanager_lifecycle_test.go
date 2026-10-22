package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("cert-manager independent lifecycle decisions", Label("unit"), func() {
	for _, issuerKind := range []string{"Issuer", "ClusterIssuer"} {
		It("UT-AUDIT-515-CERTMANAGER-001: drives provisioning, output readiness and activation with "+issuerKind, func() {
			kn := certManagerLifecycleCR(issuerKind)
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			installCertManagerLifecycleInputs(r.Client, kn)
			exerciseCertManagerLifecycle(r, kn)
		})
	}

	It("UT-AUDIT-515-CERTMANAGER-002: rejects absent, ambiguous and unreadable automatic issuers", func() {
		kn := certManagerLifecycleCR("Issuer")
		kn.Spec.TLS.CertManager.IssuerRef.Name = ""
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		for _, crd := range certManagerLifecycleCRDs() {
			Expect(r.Create(ctx, crd)).To(Succeed())
		}
		_, err := r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("no cert-manager")))
		for _, name := range []string{"first", "second"} {
			issuer := certManagerExternalLifecycleIssuer(kn)
			issuer.SetName(name)
			Expect(r.Create(ctx, issuer)).To(Succeed())
			if name == "first" {
				selected, selectErr := r.resolveCertManagerIssuer(ctx, kn)
				Expect(selectErr).NotTo(HaveOccurred())
				Expect(selected.name).To(Equal(name))
			}
		}
		_, err = r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("multiple cert-manager")))
		kn.Spec.TLS.CertManager.IssuerRef.Name = "absent"
		_, err = r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("reading cert-manager")))
		kn.Spec.TLS.CertManager.IssuerRef.Kind = "Unsupported"
		_, err = r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("unsupported")))
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeCertManager
		kn.Spec.TLS.CertManager = nil
		_, err = r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(HaveOccurred())
		r = businessUnitReconciler(kn, interceptor.Funcs{List: func(context.Context, client.WithWatch,
			client.ObjectList, ...client.ListOption) error {
			return fmt.Errorf("issuer API denied")
		}})
		_, err = r.selectSingleCertManagerIssuer(ctx, kn, "cert-manager.io", "Issuer")
		Expect(err).To(MatchError(ContainSubstring("issuer API denied")))
	})

	It("UT-AUDIT-515-CERTMANAGER-003: rejects missing APIs, invalid output configuration and resource-write failures", func() {
		kn := certManagerLifecycleCR("Issuer")
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(HaveOccurred())
		installCertManagerLifecycleInputs(r.Client, kn)
		Expect(r.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{
			Name: "certificates.cert-manager.io",
		}})).To(Succeed())
		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(MatchError(ContainSubstring("api is not installed: certificates")))
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(Succeed())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeCertManager
		kn.Spec.TLS.CertManager = nil
		_, err := r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			IssuerRef: kubernautv1alpha2.TLSIssuerRef{Kind: "Issuer"},
		}
		Expect(r.Create(ctx, certManagerLifecycleCRDs()[0])).To(Succeed())
		// Default issuer selection must be resolved without mutating the CR.
		kn.Spec.TLS.CertManager = nil
		Expect(r.Create(ctx, &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
			"metadata": map[string]interface{}{"name": "sole-cluster-issuer"},
		}})).To(Succeed())
		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(Succeed())
		Expect(kn.Spec.TLS.CertManager).To(BeNil())
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{IssuerRef: kubernautv1alpha2.TLSIssuerRef{
			Kind: "Issuer", Name: "external-customer-issuer",
		}, Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(true)}}
		installCertManagerLifecycleInputs(r.Client, kn)
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Create: deniedUnitCreate})
		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		Expect(r.validateCertManagerTLS(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		Expect(r.ensureCertManagerDesiredTLSResources(ctx, kn, nil)).To(MatchError(ContainSubstring("building cert-manager TLS resources")))
	})

	It("UT-AUDIT-515-CERTMANAGER-004: rejects unreadable or malformed readiness conditions instead of assuming issuance", func() {
		kn := certManagerLifecycleCR("Issuer")
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateCertManagerCertificatesReady(ctx, kn)).To(MatchError(ContainSubstring("reading cert-manager Certificate")))
		objects, err := resources.CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, object := range objects {
			if object.GetKind() != unitCertificateKind {
				continue
			}
			object.Object["status"] = map[string]interface{}{"conditions": "not-a-list"}
			Expect(r.Create(ctx, object)).To(Succeed())
		}
		Expect(r.validateCertManagerCertificatesReady(ctx, kn)).To(MatchError(ContainSubstring("conditions")))
		ready, reason, message := certManagerReadyCondition([]interface{}{"not-a-map", map[string]interface{}{"type": "Issuing"}})
		Expect(ready).To(BeFalse())
		Expect(reason).To(Equal("Ready condition is absent"))
		Expect(message).To(BeEmpty())
		Expect(r.validateCertManagerCertificatesReady(ctx, nil)).To(HaveOccurred())
	})

	It("UT-AUDIT-515-CERTMANAGER-005: preserves resource API errors and rejects conflicting controller identities", func() {
		kn := certManagerLifecycleCR("Issuer")
		kn.UID = "issuer-owner"
		objects, err := resources.CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		for index, faults := range []interceptor.Funcs{{Get: deniedUnitGet}, {Create: deniedUnitCreate},
			{Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
				return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "certificates"}, "raced")
			}}} {
			r := businessUnitReconciler(kn, faults)
			err = r.ensureCertManagerResource(ctx, kn, objects[0].DeepCopy())
			if index == 2 {
				Expect(err).To(MatchError(ContainSubstring("after AlreadyExists")))
			} else {
				Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		r.Scheme = runtime.NewScheme()
		Expect(r.ensureCertManagerResource(ctx, kn, objects[0].DeepCopy())).To(HaveOccurred())
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		live := objects[0].DeepCopy()
		live.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: kubernautv1alpha2.GroupVersion.String(),
			Kind: "Kubernaut", Name: "different-kubernaut", UID: "different-owner", Controller: ptr.To(true)}})
		// Same-name UID replacement is a distinct safety contract supplied by #517.
		Expect(r.Create(ctx, live)).To(Succeed())
		Expect(r.ensureCertManagerResource(ctx, kn, objects[0].DeepCopy())).To(HaveOccurred())
	})

	It("UT-AUDIT-515-CERTMANAGER-006: preserves each webhook bootstrap error for manual and cert-manager sources", func() {
		for _, mode := range []kubernautv1alpha2.TLSMode{kubernautv1alpha2.TLSModeManual, kubernautv1alpha2.TLSModeCertManager} {
			for _, kind := range []string{"*v1.MutatingWebhookConfiguration", "*v1.ValidatingWebhookConfiguration"} {
				kn := certManagerLifecycleCR("Issuer")
				kn.Spec.TLS.Mode = mode
				r := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
					object client.Object, options ...client.CreateOption) error {
					if fmt.Sprintf("%T", object) == kind {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.Create(ctx, object, options...)
				}})
				material, err := resources.ResolveTLSMaterial(kn)
				Expect(err).NotTo(HaveOccurred())
				Expect(r.validateTLSWebhookReadiness(ctx, kn, material)).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
		}
	})

	It("UT-AUDIT-515-CERTMANAGER-007: never treats absent webhook entries or absent cainjector bundles as ready", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateWebhookCABundles(ctx, kn)).To(MatchError(ContainSubstring("reading AuthWebhook Mutating")))
		mutating := resources.MutatingWebhookConfigurationWithCABundle(kn, telemetryCAPEM())
		Expect(r.Create(ctx, mutating)).To(Succeed())
		Expect(r.validateWebhookCABundles(ctx, kn)).To(MatchError(ContainSubstring("reading AuthWebhook Validating")))
		validating := resources.ValidatingWebhookConfigurationWithCABundle(kn, nil)
		Expect(r.Create(ctx, validating)).To(Succeed())
		Expect(r.validateWebhookCABundles(ctx, kn)).To(MatchError(ContainSubstring("ValidatingWebhookConfiguration CA bundle is not ready")))
		Expect(webhookCABundleReadyMutating(nil)).To(HaveOccurred())
		Expect(webhookCABundleReadyValidating(nil)).To(HaveOccurred())
	})

	It("UT-AUDIT-515-CERTMANAGER-008: keeps automatic external Issuer selection stable after creating its own bootstrap Issuers", func() {
		kn := certManagerLifecycleCR("Issuer")
		kn.UID = "automatic-issuer-holder"
		kn.Spec.TLS.CertManager.IssuerRef.Name = ""
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		installCertManagerLifecycleInputs(r.Client, kn)
		exerciseCertManagerLifecycle(r, kn)
		selected, err := r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(selected.name).To(Equal("external-customer-issuer"))
		objects, err := resources.CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, object := range objects {
			if object.GetKind() != "Issuer" {
				continue
			}
			Expect(r.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
			object.SetOwnerReferences(nil)
			Expect(r.Update(ctx, object)).To(Succeed())
			_, err = r.resolveCertManagerIssuer(ctx, kn)
			Expect(err).To(MatchError(ContainSubstring("multiple cert-manager")), "names or labels alone cannot exclude an external Issuer")
			Expect(resources.SetOwnerReference(kn, object, r.Scheme)).To(Succeed())
			Expect(r.Update(ctx, object)).To(Succeed())
			owner := object.GetOwnerReferences()[0]
			owner.UID = "previous-holder"
			object.SetOwnerReferences([]metav1.OwnerReference{owner})
			Expect(r.Update(ctx, object)).To(Succeed())
			_, err = r.resolveCertManagerIssuer(ctx, kn)
			Expect(err).To(MatchError(ContainSubstring("multiple cert-manager")), "an Issuer controlled by another UID remains a candidate")
			owner.UID = kn.UID
			object.SetOwnerReferences([]metav1.OwnerReference{owner})
			Expect(r.Update(ctx, object)).To(Succeed())
		}
		additional := certManagerExternalLifecycleIssuer(kn)
		additional.SetName("second-external-issuer")
		Expect(r.Create(ctx, additional)).To(Succeed())
		_, err = r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("multiple cert-manager")))
	})
})

var _ = Describe("cert-manager real-API lifecycle wiring", func() {
	BeforeEach(func() {
		createBYOSecrets(ctx)
		deleteExternalTLSFixtureSecrets(ctx)
		for _, crd := range certManagerLifecycleCRDs() {
			Expect(k8sClient.Create(ctx, crd)).To(Succeed())
			Eventually(func(g Gomega) {
				live := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), live)).To(Succeed())
				g.Expect(live.Status.Conditions).To(ContainElement(And(
					HaveField("Type", apiextensionsv1.Established), HaveField("Status", apiextensionsv1.ConditionTrue))))
			}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		}
	})
	AfterEach(func() {
		deleteExternalTLSFixtureSecrets(ctx)
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
		for _, crd := range certManagerLifecycleCRDs() {
			Expect(k8sClient.Delete(ctx, crd)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), crd))
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
		}
	})
	for _, issuerKind := range []string{"Issuer", "ClusterIssuer"} {
		It("IT-TLS-AUDIT-515-003: exercises cert-manager provisioning and activation through envtest with "+issuerKind, func() {
			kn := certManagerLifecycleCR(issuerKind)
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())
			issuer := certManagerExternalLifecycleIssuer(kn)
			Expect(k8sClient.Create(ctx, issuer)).To(Succeed())
			exerciseCertManagerLifecycle(newReconciler(), kn)
		})
	}
	It("IT-TLS-AUDIT-515-004: completes automatic Issuer provisioning despite its own persisted bootstrap Issuers", func() {
		kn := certManagerLifecycleCR("Issuer")
		kn.Spec.TLS.CertManager.IssuerRef.Name = ""
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		Expect(k8sClient.Create(ctx, certManagerExternalLifecycleIssuer(kn))).To(Succeed())
		exerciseCertManagerLifecycle(newReconciler(), kn)
	})
})

func deleteExternalTLSFixtureSecrets(ctx context.Context) {
	secrets := &corev1.SecretList{}
	if err := k8sClient.List(ctx, secrets, client.InNamespace(testNamespace), client.MatchingLabels{"external-issuer": "true"}); err != nil {
		return
	}
	for index := range secrets.Items {
		_ = k8sClient.Delete(ctx, &secrets.Items[index])
	}
}

const unitCertificateKind = "Certificate"

func certManagerLifecycleCR(kind string) *kubernautv1alpha2.Kubernaut {
	kn := newCRWithRouteDisabled()
	kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeCertManager,
		CertManager: &kubernautv1alpha2.CertManagerTLSConfig{IssuerRef: kubernautv1alpha2.TLSIssuerRef{
			Kind: kind, Name: "external-customer-issuer",
		}, Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(true)},
		}}
	return kn
}

func certManagerLifecycleCRDs() []*apiextensionsv1.CustomResourceDefinition {
	definitions := make([]*apiextensionsv1.CustomResourceDefinition, 0, 3)
	for _, kind := range []string{unitCertificateKind, "Issuer", "ClusterIssuer"} {
		plural := strings.ToLower(kind) + "s"
		scope := apiextensionsv1.NamespaceScoped
		if kind == "ClusterIssuer" {
			scope = apiextensionsv1.ClusterScoped
		}
		definitions = append(definitions, &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: plural + ".cert-manager.io"},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{Group: "cert-manager.io", Scope: scope,
				Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: kind, Plural: plural, Singular: strings.ToLower(kind)},
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true,
					Subresources: &apiextensionsv1.CustomResourceSubresources{Status: &apiextensionsv1.CustomResourceSubresourceStatus{}},
					Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec":   {Type: "object", XPreserveUnknownFields: ptr.To(true)},
							"status": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
						},
					}},
				}},
			},
		})
	}
	return definitions
}

func certManagerExternalLifecycleIssuer(kn *kubernautv1alpha2.Kubernaut) *unstructured.Unstructured {
	issuer := &unstructured.Unstructured{}
	issuer.SetAPIVersion("cert-manager.io/v1")
	issuer.SetKind(kn.Spec.TLS.CertManager.IssuerRef.Kind)
	issuer.SetName("external-customer-issuer")
	if issuer.GetKind() == "Issuer" {
		issuer.SetNamespace(kn.Namespace)
	}
	issuer.Object["spec"] = map[string]interface{}{"selfSigned": map[string]interface{}{}}
	return issuer
}

func installCertManagerLifecycleInputs(api client.Client, kn *kubernautv1alpha2.Kubernaut) {
	for _, crd := range certManagerLifecycleCRDs() {
		ExpectWithOffset(1, api.Create(ctx, crd)).To(Succeed())
	}
	ExpectWithOffset(1, api.Create(ctx, certManagerExternalLifecycleIssuer(kn))).To(Succeed())
}

// Both tiers call the same production reconciliation journey using different
// external APIs. The unit tier passes a fake API; the integration tier passes
// envtest. Only issuer output and cainjector writes are simulated: the operator
// never issues cert-manager's certificates or claims that qualification here.
func exerciseCertManagerLifecycle(r *KubernautReconciler, kn *kubernautv1alpha2.Kubernaut) {
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}
	for range 2 {
		_, err := r.Reconcile(ctx, request)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	}
	ExpectWithOffset(1, r.Get(ctx, request.NamespacedName, kn)).To(Succeed())
	ExpectWithOffset(1, kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
	ExpectWithOffset(1, meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionTLSReady)).To(BeTrue())
	ExpectWithOffset(1, meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionTLSReady).Message).
		To(ContainSubstring("is not ready"))
	jobs := &batchv1.JobList{}
	ExpectWithOffset(1, r.List(ctx, jobs, client.InNamespace(kn.Namespace))).To(Succeed())
	ExpectWithOffset(1, jobs.Items).To(BeEmpty(), "unissued TLS must block migration")
	objects, err := resources.CertManagerTLSResources(kn)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	for _, object := range objects {
		if object.GetKind() != unitCertificateKind {
			continue
		}
		live := object.DeepCopy()
		ExpectWithOffset(1, r.Get(ctx, client.ObjectKeyFromObject(object), live)).To(Succeed())
		ExpectWithOffset(1, live.GetOwnerReferences()).To(ContainElement(HaveField("UID", kn.UID)))
		live.Object["status"] = map[string]interface{}{"conditions": []interface{}{map[string]interface{}{
			"type": "Ready", "status": "True",
		}}}
		ExpectWithOffset(1, r.Status().Update(ctx, live)).To(Succeed())
	}
	generator := kn.DeepCopy()
	generator.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}
	secrets, err := testRuntimeTLSSecrets(generator, nil, time.Now().Add(-time.Hour))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	material, err := resources.ResolveTLSMaterial(kn)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	secrets[0].Name = material.InternalCASecretName
	secrets[0].Data[corev1.TLSCertKey] = append([]byte(nil), secrets[0].Data["ca.crt"]...)
	for _, secret := range secrets {
		secret.Labels, secret.OwnerReferences = map[string]string{"external-issuer": "true"}, nil
		ExpectWithOffset(1, r.Create(ctx, secret)).To(Succeed())
	}
	_, err = r.Reconcile(ctx, request)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	caPEM := secrets[0].Data["ca.crt"]
	mutating := &admissionv1.MutatingWebhookConfiguration{}
	ExpectWithOffset(1, r.Get(ctx, client.ObjectKey{Name: kn.Namespace + "-authwebhook-mutating"}, mutating)).To(Succeed())
	for index := range mutating.Webhooks {
		mutating.Webhooks[index].ClientConfig.CABundle = caPEM
	}
	ExpectWithOffset(1, r.Update(ctx, mutating)).To(Succeed())
	validating := &admissionv1.ValidatingWebhookConfiguration{}
	ExpectWithOffset(1, r.Get(ctx, client.ObjectKey{Name: kn.Namespace + "-authwebhook-validating"}, validating)).To(Succeed())
	for index := range validating.Webhooks {
		validating.Webhooks[index].ClientConfig.CABundle = caPEM
	}
	ExpectWithOffset(1, r.Update(ctx, validating)).To(Succeed())
	_, err = r.Reconcile(ctx, request)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, r.List(ctx, jobs, client.InNamespace(kn.Namespace))).To(Succeed())
	ExpectWithOffset(1, jobs.Items).To(HaveLen(1))
	now := metav1.Now()
	jobs.Items[0].Status.StartTime, jobs.Items[0].Status.CompletionTime = &now, &now
	jobs.Items[0].Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
	}
	ExpectWithOffset(1, r.Status().Update(ctx, &jobs.Items[0])).To(Succeed())
	_, err = r.Reconcile(ctx, request)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	deployments := &appsv1.DeploymentList{}
	ExpectWithOffset(1, r.List(ctx, deployments, client.InNamespace(kn.Namespace))).To(Succeed())
	ExpectWithOffset(1, deployments.Items).To(HaveLen(len(resources.ActiveComponents(kn, kn))))
	for index := range deployments.Items {
		deployment := &deployments.Items[index]
		deployment.Status.ReadyReplicas = ptr.Deref(deployment.Spec.Replicas, 1)
		deployment.Status.Replicas = deployment.Status.ReadyReplicas
		ExpectWithOffset(1, r.Status().Update(ctx, deployment)).To(Succeed())
	}
	_, err = r.Reconcile(ctx, request)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, r.Get(ctx, request.NamespacedName, kn)).To(Succeed())
	ExpectWithOffset(1, kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
	for _, secret := range secrets {
		live := &corev1.Secret{}
		ExpectWithOffset(1, r.Get(ctx, client.ObjectKeyFromObject(secret), live)).To(Succeed())
		ExpectWithOffset(1, live.Data).To(Equal(secret.Data))
		ExpectWithOffset(1, live.OwnerReferences).To(BeEmpty(), "runtime output must not be adopted")
	}
}
