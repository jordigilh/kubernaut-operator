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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("DataStorage signing material rollout", Label("unit"), func() {
	It("UT-DS-550-ROT-001 [AU-9, AC-6, SI-4, SOC2 CC7.2; ASVS V13.3.1, V16.5.2] hashes Secret metadata and stamps only DataStorage", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.Audit = &kubernautv1alpha2.DataStorageAuditSpec{
			SigningCertificate: &kubernautv1alpha2.SigningCertSpec{SecretName: "customer-signing"},
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-signing", Namespace: kn.Namespace, ResourceVersion: "7"},
			Data:       map[string][]byte{corev1.TLSCertKey: []byte("private material")},
		}
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()}

		first, err := reconciler.dataStorageSigningMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(64))
		Expect(first).NotTo(ContainSubstring("private material"))

		rotatedSecret := secret.DeepCopy()
		rotatedSecret.ResourceVersion = "8"
		rotatedReconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(rotatedSecret).Build()}
		second, err := rotatedReconciler.dataStorageSigningMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).NotTo(Equal(first))

		missingReconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		missing, err := missingReconciler.dataStorageSigningMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(HaveLen(64))

		deniedReconciler := &KubernautReconciler{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return errors.New("secret read denied")
			}}).Build()}
		_, err = deniedReconciler.dataStorageSigningMaterialRevision(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("reading DataStorage signing Secret")))

		dataStorage := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentDataStorage}},
		}}}
		other := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentGateway}},
		}}}
		stampDataStorageSigningMaterialRevision(dataStorage, telemetryMaterialRevisionSet{DataStorageSigning: second})
		stampDataStorageSigningMaterialRevision(other, telemetryMaterialRevisionSet{DataStorageSigning: second})
		Expect(dataStorage.Spec.Template.Annotations).To(HaveKeyWithValue(resources.AnnotationDataStorageSigningMaterialRevision, second))
		Expect(other.Spec.Template.Annotations).NotTo(HaveKey(resources.AnnotationDataStorageSigningMaterialRevision))
	})

	It("UT-DS-550-ROT-002 [SI-4, ASVS V16.1.1] maps the effective signing Secret to the singleton", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.Audit = &kubernautv1alpha2.DataStorageAuditSpec{
			SigningCertificate: &kubernautv1alpha2.SigningCertSpec{SecretName: "customer-signing"},
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "customer-signing", Namespace: kn.Namespace}}
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, secret).Build()}

		Expect(reconciler.dataStorageSigningSecretToKubernaut(context.Background(), secret)).To(ConsistOf(
			reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)},
		))
		Expect(reconciler.dataStorageSigningSecretToKubernaut(context.Background(), &corev1.ConfigMap{})).To(BeNil())
		unrelated := secret.DeepCopy()
		unrelated.Name = "unrelated"
		Expect(reconciler.dataStorageSigningSecretToKubernaut(context.Background(), unrelated)).To(BeEmpty())

		listDenied := &KubernautReconciler{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(kn, secret).
			WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list denied")
			}}).Build()}
		Expect(listDenied.dataStorageSigningSecretToKubernaut(context.Background(), secret)).To(BeNil())
	})

	It("UT-587-REV-001 [CM-3, CM-6, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.5.2] uses dependency Secret metadata for DataStorage rollout and watches only referenced Secrets", func() {
		kn := newMinimalCR()
		postgres := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.PostgreSQL.SecretName, Namespace: kn.Namespace, ResourceVersion: "7"}}
		valkey := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.Valkey.SecretName, Namespace: kn.Namespace, ResourceVersion: "11"}}
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, postgres, valkey).Build()}

		postgresRevision, err := reconciler.postgresqlCredentialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		dependencyRevision, err := reconciler.dataStorageDependencyRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(postgresRevision).To(HaveLen(64))
		Expect(dependencyRevision).To(HaveLen(64))

		rotated := valkey.DeepCopy()
		rotated.ResourceVersion = "12"
		rotatedReconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, postgres, rotated).Build()}
		rotatedRevision, err := rotatedReconciler.dataStorageDependencyRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(rotatedRevision).NotTo(Equal(dependencyRevision))

		missingPostgres := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, valkey).Build()}
		_, err = missingPostgres.postgresqlCredentialRevision(context.Background(), kn)
		Expect(err).To(HaveOccurred())
		_, err = missingPostgres.dataStorageDependencyRevision(context.Background(), kn)
		Expect(err).To(HaveOccurred())
		missingValkey := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, postgres).Build()}
		_, err = missingValkey.dataStorageDependencyRevision(context.Background(), kn)
		Expect(err).To(HaveOccurred())

		dataStorage := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentDataStorage}},
		}}}
		other := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentGateway}},
		}}}
		stampDataStorageDependencyRevision(dataStorage, telemetryMaterialRevisionSet{DataStorageDependency: dependencyRevision})
		stampDataStorageDependencyRevision(other, telemetryMaterialRevisionSet{DataStorageDependency: dependencyRevision})
		Expect(dataStorage.Spec.Template.Annotations).To(HaveKeyWithValue(resources.AnnotationDataStorageDependencyRevision, dependencyRevision))
		Expect(other.Spec.Template.Annotations).NotTo(HaveKey(resources.AnnotationDataStorageDependencyRevision))

		requests := reconciler.byoDependencySecretToKubernaut(context.Background(), postgres)
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))
		Expect(reconciler.byoDependencySecretToKubernaut(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: kn.Namespace}})).To(BeEmpty())
		Expect(reconciler.byoDependencySecretToKubernaut(context.Background(), &corev1.ConfigMap{})).To(BeNil())
		listDenied := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn).WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list denied")
			},
		}).Build()}
		Expect(listDenied.byoDependencySecretToKubernaut(context.Background(), postgres)).To(BeNil())
	})
})
