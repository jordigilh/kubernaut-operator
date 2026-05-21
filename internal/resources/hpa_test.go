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
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HPA Builders", func() {
	var kn = testKubernaut

	Describe("DataStorageHPA", func() {
		It("targets the datastorage Deployment", func() {
			hpa := DataStorageHPA(kn())
			Expect(hpa.Spec.ScaleTargetRef).To(Equal(autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       DeploymentName(ComponentDataStorage),
			}))
		})

		It("sets MinReplicas=1 and MaxReplicas=5", func() {
			hpa := DataStorageHPA(kn())
			Expect(hpa.Spec.MinReplicas).NotTo(BeNil())
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(1)))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(5)))
		})

		It("includes both CPU and memory metrics", func() {
			hpa := DataStorageHPA(kn())
			Expect(hpa.Spec.Metrics).To(HaveLen(2))

			cpuMetric := hpa.Spec.Metrics[0]
			Expect(cpuMetric.Type).To(Equal(autoscalingv2.ResourceMetricSourceType))
			Expect(string(cpuMetric.Resource.Name)).To(Equal("cpu"))
			Expect(*cpuMetric.Resource.Target.AverageUtilization).To(Equal(int32(75)))

			memMetric := hpa.Spec.Metrics[1]
			Expect(memMetric.Type).To(Equal(autoscalingv2.ResourceMetricSourceType))
			Expect(string(memMetric.Resource.Name)).To(Equal("memory"))
			Expect(*memMetric.Resource.Target.AverageUtilization).To(Equal(int32(80)))
		})

		It("sets standard labels and namespace", func() {
			hpa := DataStorageHPA(kn())
			Expect(hpa.Namespace).To(Equal(testSystemNamespace))
			Expect(hpa.Labels["app.kubernetes.io/managed-by"]).To(Equal("kubernaut-operator"))
			Expect(hpa.Name).To(Equal(ComponentDataStorage + "-hpa"))
		})
	})

	Describe("APIFrontendHPA", func() {
		It("targets the apifrontend Deployment", func() {
			hpa := APIFrontendHPA(kn())
			Expect(hpa.Spec.ScaleTargetRef.Name).To(Equal(DeploymentName(ComponentAPIFrontend)))
		})

		It("sets MinReplicas=1 and MaxReplicas=5", func() {
			hpa := APIFrontendHPA(kn())
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(1)))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(5)))
		})

		It("includes CPU at 75% and memory at 80%", func() {
			hpa := APIFrontendHPA(kn())
			Expect(hpa.Spec.Metrics).To(HaveLen(2))
			Expect(*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization).To(Equal(int32(75)))
			Expect(*hpa.Spec.Metrics[1].Resource.Target.AverageUtilization).To(Equal(int32(80)))
		})
	})

	Describe("buildHPA without memory metric", func() {
		It("omits memory metric when MemoryTargetPercent is 0", func() {
			hpa := buildHPA(kn(), HPASpec{
				Component:           ComponentDataStorage,
				MinReplicas:         2,
				MaxReplicas:         10,
				CPUTargetPercent:    60,
				MemoryTargetPercent: 0,
			})
			Expect(hpa.Spec.Metrics).To(HaveLen(1))
			Expect(string(hpa.Spec.Metrics[0].Resource.Name)).To(Equal("cpu"))
			Expect(*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization).To(Equal(int32(60)))
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(2)))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(10)))
		})
	})
})
