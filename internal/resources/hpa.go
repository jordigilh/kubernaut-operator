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
	"k8s.io/utils/ptr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// HPASpec configures the common HPA parameters.
type HPASpec struct {
	Component           string
	MinReplicas         int32
	MaxReplicas         int32
	CPUTargetPercent    int32
	MemoryTargetPercent int32
}

func buildHPA(kn *kubernautv1alpha1.Kubernaut, spec HPASpec) *autoscalingv2.HorizontalPodAutoscaler {
	metrics := []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "cpu",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: ptr.To(spec.CPUTargetPercent),
				},
			},
		},
	}

	if spec.MemoryTargetPercent > 0 {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: "memory",
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: ptr.To(spec.MemoryTargetPercent),
				},
			},
		})
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: ObjectMeta(kn, spec.Component+"-hpa", spec.Component),
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       DeploymentName(spec.Component),
			},
			MinReplicas: ptr.To(spec.MinReplicas),
			MaxReplicas: spec.MaxReplicas,
			Metrics:     metrics,
		},
	}
}

// DataStorageHPA builds the HPA for the DataStorage component.
func DataStorageHPA(kn *kubernautv1alpha1.Kubernaut) *autoscalingv2.HorizontalPodAutoscaler {
	return buildHPA(kn, HPASpec{
		Component:           ComponentDataStorage,
		MinReplicas:         1,
		MaxReplicas:         5,
		CPUTargetPercent:    75,
		MemoryTargetPercent: 80,
	})
}

// APIFrontendHPA builds the HPA for the APIFrontend component.
func APIFrontendHPA(kn *kubernautv1alpha1.Kubernaut) *autoscalingv2.HorizontalPodAutoscaler {
	return buildHPA(kn, HPASpec{
		Component:           ComponentAPIFrontend,
		MinReplicas:         1,
		MaxReplicas:         5,
		CPUTargetPercent:    75,
		MemoryTargetPercent: 80,
	})
}
