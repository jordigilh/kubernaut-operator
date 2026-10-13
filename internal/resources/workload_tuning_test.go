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

package resources

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("shared workload tuning", func() {
	It("UT-TUNE-512-DEPLOY-001: applies replicas, scheduling, topology, and security overrides while retaining secure defaults", func() {
		kn := testKubernaut()
		replicas := int32(3)
		kn.Spec.Gateway.Workload = &kubernautv1alpha2.WorkloadSpec{
			Replicas: &replicas,
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/worker": "true",
			},
			Tolerations: []corev1.Toleration{{
				Key:      "workload",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key:      "topology.kubernetes.io/zone",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"zone-a"},
							}},
						}},
					},
				},
			},
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
				MaxSkew:           1,
				TopologyKey:       "topology.kubernetes.io/zone",
				WhenUnsatisfiable: corev1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentGateway)},
			}},
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To(int64(1001)),
			},
			ContainerSecurityContext: &corev1.SecurityContext{
				RunAsUser: ptr.To(int64(1001)),
			},
		}

		dep, err := GatewayDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		Expect(*dep.Spec.Replicas).To(Equal(replicas))
		Expect(dep.Spec.Template.Spec.NodeSelector).To(Equal(kn.Spec.Gateway.Workload.NodeSelector))
		Expect(dep.Spec.Template.Spec.Tolerations).To(Equal(kn.Spec.Gateway.Workload.Tolerations))
		Expect(dep.Spec.Template.Spec.Affinity.NodeAffinity).To(Equal(kn.Spec.Gateway.Workload.Affinity.NodeAffinity))
		Expect(dep.Spec.Template.Spec.TopologySpreadConstraints).To(Equal(kn.Spec.Gateway.Workload.TopologySpreadConstraints))

		podSecurity := dep.Spec.Template.Spec.SecurityContext
		Expect(podSecurity).NotTo(BeNil())
		Expect(podSecurity.RunAsNonRoot).NotTo(BeNil())
		Expect(*podSecurity.RunAsNonRoot).To(BeTrue())
		Expect(podSecurity.SeccompProfile).NotTo(BeNil())
		Expect(podSecurity.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		Expect(*podSecurity.RunAsUser).To(Equal(int64(1001)))

		containerSecurity := dep.Spec.Template.Spec.Containers[0].SecurityContext
		Expect(containerSecurity).NotTo(BeNil())
		Expect(containerSecurity.AllowPrivilegeEscalation).NotTo(BeNil())
		Expect(*containerSecurity.AllowPrivilegeEscalation).To(BeFalse())
		Expect(containerSecurity.ReadOnlyRootFilesystem).NotTo(BeNil())
		Expect(*containerSecurity.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(containerSecurity.Capabilities).NotTo(BeNil())
		Expect(containerSecurity.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		Expect(*containerSecurity.RunAsUser).To(Equal(int64(1001)))
	})

	Describe("PDB policy", func() {
		It("UT-TUNE-512-PDB-001: keeps the Helm-compatible maxUnavailable default", func() {
			kn := testKubernaut()
			pdbs := PodDisruptionBudgets(kn, testKnV2(kn))
			Expect(pdbs).NotTo(BeEmpty())
			Expect(pdbs[0].Spec.MaxUnavailable).NotTo(BeNil())
			Expect(pdbs[0].Spec.MaxUnavailable.IntValue()).To(Equal(1))
		})

		It("UT-TUNE-512-PDB-002: honors an explicit disablement and minAvailable threshold", func() {
			kn := testKubernaut()
			minAvailable := intstr.FromString("50%")
			kn.Spec.Gateway.Workload = &kubernautv1alpha2.WorkloadSpec{
				PDB: &kubernautv1alpha2.PodDisruptionBudgetSpec{
					Enabled:      ptr.To(false),
					MinAvailable: &minAvailable,
				},
			}
			kn.Spec.DataStorage.Workload = &kubernautv1alpha2.WorkloadSpec{
				PDB: &kubernautv1alpha2.PodDisruptionBudgetSpec{
					MinAvailable: &minAvailable,
				},
			}

			pdbs := PodDisruptionBudgets(kn, testKnV2(kn))
			Expect(pdbs).NotTo(ContainElement(HaveField("Name", ComponentGateway)))
			dataStoragePDB := pdbs[0]
			Expect(dataStoragePDB.Name).To(Equal(ComponentDataStorage))
			Expect(dataStoragePDB.Spec.MinAvailable).NotTo(BeNil())
			Expect(dataStoragePDB.Spec.MinAvailable.String()).To(Equal(minAvailable.String()))
			Expect(dataStoragePDB.Spec.MaxUnavailable).To(BeNil())
		})

		It("UT-TUNE-512-PDB-003: honors an explicit maxUnavailable percentage", func() {
			kn := testKubernaut()
			maxUnavailable := intstr.FromString("25%")
			kn.Spec.AIAnalysis.Workload = &kubernautv1alpha2.WorkloadSpec{
				PDB: &kubernautv1alpha2.PodDisruptionBudgetSpec{MaxUnavailable: &maxUnavailable},
			}

			var aiPDB *policyv1.PodDisruptionBudget
			for _, pdb := range PodDisruptionBudgets(kn, testKnV2(kn)) {
				if pdb.Name == ComponentAIAnalysis {
					aiPDB = pdb
					break
				}
			}
			Expect(aiPDB).NotTo(BeNil())
			Expect(aiPDB.Spec.MaxUnavailable.String()).To(Equal(maxUnavailable.String()))
			Expect(aiPDB.Spec.MinAvailable).To(BeNil())
		})
	})

	Describe("HPA policy", func() {
		It("UT-TUNE-512-HPA-001: disables DataStorage and APIFrontend HPAs by default", func() {
			kn := testKubernaut()
			Expect(DataStorageHPA(kn)).To(BeNil())
			Expect(APIFrontendHPA(kn)).To(BeNil())
		})

		It("UT-TUNE-512-HPA-002: renders explicitly enabled HPA tuning and omits disabled memory metrics", func() {
			kn := testKubernaut()
			kn.Spec.DataStorage.Workload = &kubernautv1alpha2.WorkloadSpec{
				Autoscaling: &kubernautv1alpha2.AutoscalingSpec{
					Enabled:      ptr.To(true),
					MinReplicas:  ptr.To(int32(2)),
					MaxReplicas:  ptr.To(int32(8)),
					CPUTarget:    ptr.To(int32(60)),
					MemoryTarget: ptr.To(int32(0)),
				},
			}

			hpa := DataStorageHPA(kn)
			Expect(hpa).NotTo(BeNil())
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(2)))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(8)))
			Expect(hpa.Spec.Metrics).To(HaveLen(1))
			Expect(*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization).To(Equal(int32(60)))
		})
	})
})

var _ = Describe("workload tuning validation", func() {
	It("UT-TUNE-512-VALIDATE-001: rejects invalid HPA bounds and conflicting PDB thresholds", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.Workload = &kubernautv1alpha2.WorkloadSpec{
			PDB: &kubernautv1alpha2.PodDisruptionBudgetSpec{
				MinAvailable:   ptr.To(intstr.FromInt(1)),
				MaxUnavailable: ptr.To(intstr.FromInt(1)),
			},
			Autoscaling: &kubernautv1alpha2.AutoscalingSpec{
				Enabled:     ptr.To(true),
				MinReplicas: ptr.To(int32(8)),
				MaxReplicas: ptr.To(int32(2)),
			},
		}

		errs := ValidateKubernaut(kn)
		Expect(errs).To(ConsistOf(
			MatchError(ContainSubstring("dataStorage.workload.pdb")),
			MatchError(ContainSubstring("dataStorage.workload.autoscaling")),
		))
	})

	It("UT-TUNE-512-VALIDATE-002: rejects security overrides that remove the restricted baseline", func() {
		kn := testKubernaut()
		kn.Spec.Gateway.Workload = &kubernautv1alpha2.WorkloadSpec{
			ContainerSecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(true),
			},
		}

		errs := ValidateKubernaut(kn)
		Expect(errs).To(ContainElement(MatchError(ContainSubstring("allowPrivilegeEscalation must remain false"))))
	})
})

var _ = Describe("workload tuning component coverage", func() {
	It("UT-TUNE-512-API-001: exposes a shared workload block for every managed workload spec", func() {
		kn := testKubernaut()
		Expect(kn.Spec.Gateway.Workload).To(BeNil())
		Expect(kn.Spec.DataStorage.Workload).To(BeNil())
		Expect(kn.Spec.AIAnalysis.Workload).To(BeNil())
		Expect(kn.Spec.SignalProcessing.Workload).To(BeNil())
		Expect(kn.Spec.RemediationOrchestrator.Workload).To(BeNil())
		Expect(kn.Spec.WorkflowExecution.Workload).To(BeNil())
		Expect(kn.Spec.EffectivenessMonitor.Workload).To(BeNil())
		Expect(kn.Spec.Notification.Workload).To(BeNil())
		Expect(kn.Spec.KubernautAgent.Workload).To(BeNil())
		Expect(kn.Spec.AuthWebhook.Workload).To(BeNil())
		Expect(kn.Spec.APIFrontend.Workload).To(BeNil())
		Expect(kn.Spec.Console.Workload).To(BeNil())
		Expect(kn.Spec.FleetMetadataCache.Workload).To(BeNil())
	})

	It("UT-TUNE-512-CONSOLE-001: applies workload tuning to the separately assembled Console deployment and PDB", func() {
		kn := testKubernautWithConsole()
		replicas := int32(2)
		kn.Spec.Console.Workload = &kubernautv1alpha2.WorkloadSpec{Replicas: &replicas}

		dep, err := ConsoleDeployment(kn, "apps.example.test")
		Expect(err).NotTo(HaveOccurred())
		Expect(*dep.Spec.Replicas).To(Equal(replicas))

		pdbs := PodDisruptionBudgets(kn, testKnV2(kn))
		Expect(pdbs).To(ContainElement(HaveField("Name", ComponentConsole)))
	})
})
