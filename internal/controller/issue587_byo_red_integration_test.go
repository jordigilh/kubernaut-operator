package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Issue 587 BYO ownership through real reconciliation", func() {
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-587-ROLL-001 [AC-3, AC-6, CM-3, CM-6, SI-4; SOC2 CC6.1, CC6.6, CC7.2, CC8.1; ASVS v5.0.0-V8.3.1, V13.3.1, V14.2.4, V16.5.2] leaves BYO dependencies and DataStorage unchanged after a Gateway-only patch", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		cr.Spec.Gateway.Enabled = ptr.To(false)
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		r := reconcileToRunning(ctx)

		beforeDataStorage := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data-storage", Namespace: testNamespace}, beforeDataStorage)).To(Succeed())
		beforeDataStorageTemplate := beforeDataStorage.Spec.Template.DeepCopy()
		beforeDataStorageRV := beforeDataStorage.ResourceVersion
		beforeDataStorageConfig := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "datastorage-config", Namespace: testNamespace}, beforeDataStorageConfig)).To(Succeed())
		beforeDataStorageConfigRV := beforeDataStorageConfig.ResourceVersion

		beforePostgreSQLSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: pgSecretName, Namespace: testNamespace}, beforePostgreSQLSecret)).To(Succeed())
		beforeValkeySecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: vkSecretName, Namespace: testNamespace}, beforeValkeySecret)).To(Succeed())
		beforePostgreSQLService := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "postgresql", Namespace: testNamespace}, beforePostgreSQLService)).To(Succeed())
		beforeValkeyService := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "valkey", Namespace: testNamespace}, beforeValkeyService)).To(Succeed())
		beforeReplicaSets := &appsv1.ReplicaSetList{}
		Expect(k8sClient.List(ctx, beforeReplicaSets, client.InNamespace(testNamespace), client.MatchingLabels{"app": "datastorage"})).To(Succeed())

		live := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), live)).To(Succeed())
		live.Spec.Gateway.Enabled = ptr.To(true)
		Expect(k8sClient.Update(ctx, live)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		afterDataStorage := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "data-storage", Namespace: testNamespace}, afterDataStorage)).To(Succeed())
		Expect(afterDataStorage.ResourceVersion).To(Equal(beforeDataStorageRV))
		Expect(afterDataStorage.Spec.Template).To(Equal(*beforeDataStorageTemplate))
		afterDataStorageConfig := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "datastorage-config", Namespace: testNamespace}, afterDataStorageConfig)).To(Succeed())
		Expect(afterDataStorageConfig.ResourceVersion).To(Equal(beforeDataStorageConfigRV))
		afterPostgreSQLSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: pgSecretName, Namespace: testNamespace}, afterPostgreSQLSecret)).To(Succeed())
		Expect(afterPostgreSQLSecret).To(Equal(beforePostgreSQLSecret))
		afterValkeySecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: vkSecretName, Namespace: testNamespace}, afterValkeySecret)).To(Succeed())
		Expect(afterValkeySecret).To(Equal(beforeValkeySecret))
		afterPostgreSQLService := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "postgresql", Namespace: testNamespace}, afterPostgreSQLService)).To(Succeed())
		Expect(afterPostgreSQLService).To(Equal(beforePostgreSQLService))
		afterValkeyService := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "valkey", Namespace: testNamespace}, afterValkeyService)).To(Succeed())
		Expect(afterValkeyService).To(Equal(beforeValkeyService))
		afterReplicaSets := &appsv1.ReplicaSetList{}
		Expect(k8sClient.List(ctx, afterReplicaSets, client.InNamespace(testNamespace), client.MatchingLabels{"app": "datastorage"})).To(Succeed())
		Expect(afterReplicaSets.Items).To(Equal(beforeReplicaSets.Items))
	})

	It("IT-587-BYO-001 [AC-3, AC-6, IA-5; SOC2 CC6.1, CC6.6; ASVS v5.0.0-V8.3.1, V13.3.1] does not create the legacy derived credential Secret", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToRunning(ctx)

		legacy := &corev1.Secret{}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "datastorage-db-secret", Namespace: testNamespace}, legacy))).To(BeTrue())
	})
})
