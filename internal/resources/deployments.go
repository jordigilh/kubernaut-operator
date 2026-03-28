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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// GatewayDeployment builds the gateway Deployment.
func GatewayDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	return deployment(kn, ComponentGateway, "gateway", kn.Spec.Gateway.Resources,
		[]corev1.VolumeMount{{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"}},
		[]corev1.Volume{configMapVolume("config", "gateway-config")},
		nil, nil,
	)
}

// DataStorageDeployment builds the data-storage Deployment with init container
// for database readiness and projected secrets volume.
func DataStorageDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	pgPort := kn.Spec.PostgreSQL.Port
	if pgPort == 0 {
		pgPort = 5432
	}

	initContainer := corev1.Container{
		Name:            "wait-for-postgres",
		Image:           Image(&kn.Spec.Image, "postgresql"),
		ImagePullPolicy: kn.Spec.Image.PullPolicy,
		Command: []string{"sh", "-c",
			fmt.Sprintf("until pg_isready -h %s -p %d; do echo waiting for postgres; sleep 2; done",
				kn.Spec.PostgreSQL.Host, pgPort),
		},
		SecurityContext: ContainerSecurityContext(),
	}

	volumes := []corev1.Volume{
		configMapVolume("config", "datastorage-config"),
		{
			Name: "secrets",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "datastorage-db-secret"},
							Items:                []corev1.KeyToPath{{Key: "db-secrets.yaml", Path: "db-secrets.yaml"}},
						}},
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: kn.Spec.Valkey.SecretName},
							Items:                []corev1.KeyToPath{{Key: "valkey-secrets.yaml", Path: "valkey-secrets.yaml"}},
						}},
					},
				},
			},
		},
	}

	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "secrets", MountPath: "/etc/kubernaut/secrets", ReadOnly: true},
	}

	return deployment(kn, ComponentDataStorage, "data-storage", kn.Spec.DataStorage.Resources,
		mounts, volumes, []corev1.Container{initContainer}, nil)
}

// AIAnalysisDeployment builds the aianalysis Deployment.
func AIAnalysisDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	policyName := kn.Spec.AIAnalysis.Policy.ConfigMapName
	volumes := []corev1.Volume{
		configMapVolume("config", "aianalysis-config"),
		configMapVolume("policies", policyName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "policies", MountPath: "/etc/kubernaut/policies"},
	}
	return deployment(kn, ComponentAIAnalysis, "aianalysis", kn.Spec.AIAnalysis.Resources,
		mounts, volumes, nil, nil)
}

// SignalProcessingDeployment builds the signalprocessing Deployment.
func SignalProcessingDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	policyName := kn.Spec.SignalProcessing.Policy.ConfigMapName
	volumes := []corev1.Volume{
		configMapVolume("config", "signalprocessing-config"),
		configMapVolume("policy", policyName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "policy", MountPath: "/etc/kubernaut/policy/policy.rego", SubPath: "policy.rego"},
	}

	if kn.Spec.SignalProcessing.ProactiveSignalMappings != nil {
		volumes = append(volumes, configMapVolume("proactive-mappings", kn.Spec.SignalProcessing.ProactiveSignalMappings.ConfigMapName))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "proactive-mappings", MountPath: "/etc/kubernaut/proactive-signal-mappings.yaml", SubPath: "proactive-signal-mappings.yaml",
		})
	}

	return deployment(kn, ComponentSignalProcessing, "signalprocessing", kn.Spec.SignalProcessing.Resources,
		mounts, volumes, nil, nil)
}

// RemediationOrchestratorDeployment builds the remediationorchestrator Deployment.
func RemediationOrchestratorDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	return deployment(kn, ComponentRemediationOrchestrator, "remediationorchestrator",
		kn.Spec.RemediationOrchestrator.Resources,
		[]corev1.VolumeMount{{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"}},
		[]corev1.Volume{configMapVolume("config", "remediationorchestrator-config")},
		nil, nil,
	)
}

// WorkflowExecutionDeployment builds the workflowexecution Deployment.
func WorkflowExecutionDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	return deployment(kn, ComponentWorkflowExecution, "workflowexecution",
		kn.Spec.WorkflowExecution.Resources,
		[]corev1.VolumeMount{{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"}},
		[]corev1.Volume{configMapVolume("config", "workflowexecution-config")},
		nil, nil,
	)
}

// EffectivenessMonitorDeployment builds the effectivenessmonitor Deployment.
func EffectivenessMonitorDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	volumes := []corev1.Volume{
		configMapVolume("config", "effectivenessmonitor-config"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
	}

	if kn.Spec.Monitoring.MonitoringEnabled() {
		volumes = append(volumes, configMapVolume("service-ca", "effectivenessmonitor-service-ca"))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "service-ca", MountPath: "/etc/ssl/effectivenessmonitor", ReadOnly: true,
		})
	}

	return deployment(kn, ComponentEffectivenessMonitor, "effectivenessmonitor",
		kn.Spec.EffectivenessMonitor.Resources, mounts, volumes, nil, nil)
}

// NotificationDeployment builds the notification Deployment.
func NotificationDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	volumes := []corev1.Volume{
		configMapVolume("config", "notification-controller-config"),
		configMapVolume("routing", "notification-routing-config"),
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "routing", MountPath: "/etc/kubernaut/routing.yaml", SubPath: "routing.yaml"},
		{Name: "tmp", MountPath: "/tmp"},
	}

	if kn.Spec.Notification.Slack.SecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "credentials",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: kn.Spec.Notification.Slack.SecretName},
						}},
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name: "credentials", MountPath: "/etc/kubernaut/credentials", ReadOnly: true,
		})
	}

	return deployment(kn, ComponentNotification, "notification",
		kn.Spec.Notification.Resources, mounts, volumes, nil, nil)
}

// HolmesGPTAPIDeployment builds the holmesgpt-api Deployment.
func HolmesGPTAPIDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	sdkCMName := kn.Spec.HolmesGPTAPI.LLM.SdkConfigMapName
	if sdkCMName == "" {
		sdkCMName = "holmesgpt-sdk-config"
	}

	volumes := []corev1.Volume{
		configMapVolume("config", "holmesgpt-api-config"),
		configMapVolume("sdk-config", sdkCMName),
		secretVolume("llm-credentials", kn.Spec.HolmesGPTAPI.LLM.CredentialsSecretName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "sdk-config", MountPath: "/etc/kubernaut/sdk-config.yaml", SubPath: "sdk-config.yaml"},
		{Name: "llm-credentials", MountPath: "/etc/kubernaut/credentials", ReadOnly: true},
	}

	if kn.Spec.Monitoring.MonitoringEnabled() {
		volumes = append(volumes, configMapVolume("service-ca", "holmesgpt-api-service-ca"))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "service-ca", MountPath: "/etc/ssl/hapi", ReadOnly: true,
		})
	}

	return deployment(kn, ComponentHolmesGPTAPI, "holmesgpt-api",
		kn.Spec.HolmesGPTAPI.Resources, mounts, volumes, nil, nil)
}

// AuthWebhookDeployment builds the authwebhook Deployment.
func AuthWebhookDeployment(kn *kubernautv1alpha1.Kubernaut) *appsv1.Deployment {
	volumes := []corev1.Volume{
		configMapVolume("config", "authwebhook-config"),
		secretVolume("tls", "authwebhook-tls"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut/config.yaml", SubPath: "config.yaml"},
		{Name: "tls", MountPath: "/etc/kubernaut/tls", ReadOnly: true},
	}

	return deployment(kn, ComponentAuthWebhook, "authwebhook",
		kn.Spec.AuthWebhook.Resources, mounts, volumes, nil,
		[]corev1.ContainerPort{{Name: "https", ContainerPort: 8443, Protocol: corev1.ProtocolTCP}},
	)
}

// --- internal helpers ---

func deployment(
	kn *kubernautv1alpha1.Kubernaut,
	component, imageName string,
	resources corev1.ResourceRequirements,
	volumeMounts []corev1.VolumeMount,
	volumes []corev1.Volume,
	initContainers []corev1.Container,
	ports []corev1.ContainerPort,
) *appsv1.Deployment {
	if len(ports) == 0 {
		ports = []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}}
	}

	return &appsv1.Deployment{
		ObjectMeta: ObjectMeta(kn, component+"-deployment", component),
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: ComponentLabels(kn, component)},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountName(component),
					SecurityContext:    PodSecurityContext(),
					ImagePullSecrets:   kn.Spec.Image.PullSecrets,
					InitContainers:     initContainers,
					Containers: []corev1.Container{{
						Name:            component,
						Image:           Image(&kn.Spec.Image, imageName),
						ImagePullPolicy: kn.Spec.Image.PullPolicy,
						Ports:           ports,
						Resources:       MergeResources(resources),
						SecurityContext: ContainerSecurityContext(),
						VolumeMounts:    volumeMounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

func configMapVolume(name, cmName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
			},
		},
	}
}

func secretVolume(name, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: secretName},
		},
	}
}
