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

package contract

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("DataStorage audit HMAC operator contract", func() {
	It("CONTRACT-AUDIT-209-001 [AU-9, AC-6, SOC2 CC6.6/CC8.1; "+
		"ASVS v5.0.0-V11.1.1/V13.3.1/V16.5.2] renders the protected key journey", func() {
		kn := &kubernautv1alpha2.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha2.SingletonName, Namespace: "contract-audit"},
			Spec: kubernautv1alpha2.KubernautSpec{
				Image: kubernautv1alpha2.ImageSpec{Overrides: map[string]string{
					"datastorage": "kind.local/datastorage:test",
					"db-migrate":  "kind.local/db-migrate:test",
				}},
				PostgreSQL: kubernautv1alpha2.PostgreSQLSpec{Host: "postgres", SecretName: "postgres-secret"},
				Valkey:     kubernautv1alpha2.ValkeySpec{Host: "valkey", SecretName: "valkey-secret"},
			},
		}
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		secret, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		Expect(resources.ValidateDataStorageAuditHashKeySecret(secret, ref.SecretKey)).To(Succeed())
		Expect(secret.Immutable).NotTo(BeNil())
		Expect(*secret.Immutable).To(BeTrue())
		Expect(secret.Finalizers).To(ContainElement(resources.DataStorageAuditHashKeyFinalizer))

		configMap, err := resources.DataStorageConfigMap(kn, kn.DeepCopy(), "kubernaut", "kubernaut")
		Expect(err).NotTo(HaveOccurred())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("audit:"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring(
			"hashKeySecretsFile: /etc/datastorage/secrets/audit-hmac-key.yaml"))

		deployment, err := resources.DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		var projection *corev1.SecretProjection
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Name != "secrets" || volume.Projected == nil {
				continue
			}
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil && source.Secret.Name == resources.DataStorageAuditHashKeySecretName {
					projection = source.Secret
				}
			}
		}
		Expect(projection).NotTo(BeNil())
		Expect(projection.Items).To(ConsistOf([]corev1.KeyToPath{{
			Key:  resources.DataStorageAuditHashKeySecretDataKey,
			Path: resources.DataStorageAuditHashKeySecretDataKey,
		}}))
	})
})
