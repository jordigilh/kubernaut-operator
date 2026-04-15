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
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// GatewayDeployment builds the gateway Deployment.
func GatewayDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	corsOrigin := kn.Spec.Gateway.Config.CORSAllowedOrigins
	if corsOrigin == "" {
		corsOrigin = "https://no-browser-clients.invalid"
	}

	env := []corev1.EnvVar{
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
		{Name: "CORS_ALLOWED_ORIGINS", Value: corsOrigin},
	}

	volumes := []corev1.Volume{
		configMapVolume("config", "gateway-config"),
		secretVolume("tls-certs", GatewayTLSSecretName),
		configMapVolume("tls-ca", InterServiceCAConfigMapName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/gateway", ReadOnly: true},
		{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
	}

	env = append(env, corev1.EnvVar{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile})

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentGateway, ImageName: "gateway",
		Resources: kn.Spec.Gateway.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env,
	})
}

// DataStorageDeployment builds the data-storage Deployment with init container
// for database readiness and projected secrets volume.
func DataStorageDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	pgPort := PostgreSQLPort(kn)

	initContainer := corev1.Container{
		Name:            "wait-for-postgres",
		Image:           DefaultPostgreSQLImage,
		ImagePullPolicy: kn.Spec.Image.PullPolicy,
		Command: []string{"sh", "-c",
			"until pg_isready -h \"$PGHOST\" -p \"$PGPORT\"; do echo waiting for postgres; sleep 2; done",
		},
		Env: []corev1.EnvVar{
			{Name: "PGHOST", Value: kn.Spec.PostgreSQL.Host},
			{Name: "PGPORT", Value: strconv.FormatInt(int64(pgPort), 10)},
		},
		SecurityContext: ContainerSecurityContext(),
		Resources:       DefaultResources(),
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

	volumes = append(volumes,
		secretVolume("tls-certs", DataStorageTLSSecretName),
		configMapVolume("tls-ca", InterServiceCAConfigMapName),
	)

	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/datastorage", ReadOnly: true},
		{Name: "secrets", MountPath: "/etc/datastorage/secrets", ReadOnly: true},
		{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
	}

	env := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/datastorage/config.yaml"},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
		{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile},
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentDataStorage, ImageName: "datastorage",
		Resources: kn.Spec.DataStorage.Resources, VolumeMounts: mounts, Volumes: volumes,
		InitContainers: []corev1.Container{initContainer}, Env: env,
	})
}

// AIAnalysisDeployment builds the aianalysis Deployment.
func AIAnalysisDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	policyName := AIAnalysisPolicyName(kn)
	volumes := []corev1.Volume{
		configMapVolume("config", "aianalysis-config"),
		configMapVolume("rego-policies", policyName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/aianalysis", ReadOnly: true},
		{Name: "rego-policies", MountPath: "/etc/aianalysis/policies", ReadOnly: true},
	}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentAIAnalysis, ImageName: "aianalysis",
		Resources: kn.Spec.AIAnalysis.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
	})
}

// SignalProcessingDeployment builds the signalprocessing Deployment.
func SignalProcessingDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	policyName := SignalProcessingPolicyName(kn)
	volumes := []corev1.Volume{
		configMapVolume("config", "signalprocessing-config"),
		configMapVolume("policy", policyName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/signalprocessing/config.yaml", SubPath: "config.yaml"},
		{Name: "policy", MountPath: "/etc/signalprocessing/policies", ReadOnly: true},
	}

	if kn.Spec.SignalProcessing.ProactiveSignalMappings != nil {
		volumes = append(volumes, configMapVolume("proactive-mappings", kn.Spec.SignalProcessing.ProactiveSignalMappings.ConfigMapName))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "proactive-mappings", MountPath: "/etc/signalprocessing/proactive-signal-mappings.yaml", SubPath: "proactive-signal-mappings.yaml",
		})
	}

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentSignalProcessing, ImageName: "signalprocessing",
		Resources: kn.Spec.SignalProcessing.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
	})
}

// RemediationOrchestratorDeployment builds the remediationorchestrator Deployment.
func RemediationOrchestratorDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "remediationorchestrator-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/remediationorchestrator", ReadOnly: true}}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentRemediationOrchestrator, ImageName: "remediationorchestrator",
		Resources: kn.Spec.RemediationOrchestrator.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
	})
}

// WorkflowExecutionDeployment builds the workflowexecution Deployment.
func WorkflowExecutionDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "workflowexecution-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/workflowexecution", ReadOnly: true}}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentWorkflowExecution, ImageName: "workflowexecution",
		Resources: kn.Spec.WorkflowExecution.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
	})
}

// EffectivenessMonitorDeployment builds the effectivenessmonitor Deployment.
func EffectivenessMonitorDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{
		configMapVolume("config", "effectivenessmonitor-config"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/effectivenessmonitor", ReadOnly: true},
	}

	if kn.Spec.Monitoring.MonitoringEnabled() {
		volumes = append(volumes, configMapVolume("service-ca", "effectivenessmonitor-service-ca"))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "service-ca", MountPath: "/etc/ssl/effectivenessmonitor", ReadOnly: true,
		})
	}

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentEffectivenessMonitor, ImageName: "effectivenessmonitor",
		Resources: kn.Spec.EffectivenessMonitor.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
	})
}

// NotificationDeployment builds the notification Deployment.
func NotificationDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{
		configMapVolume("config", "notification-controller-config"),
		configMapVolume("routing-config", "notification-routing-config"),
		{Name: "notification-output", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/notification", ReadOnly: true},
		{Name: "routing-config", MountPath: "/etc/notification-routing", ReadOnly: true},
		{Name: "notification-output", MountPath: "/tmp/notifications"},
	}

	if kn.Spec.Notification.Slack.SecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "credentials",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: kn.Spec.Notification.Slack.SecretName},
							Items:                []corev1.KeyToPath{{Key: "webhook-url", Path: "slack-webhook"}},
						}},
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name: "credentials", MountPath: "/etc/notification/credentials", ReadOnly: true,
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name:         "credentials",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name: "credentials", MountPath: "/etc/notification/credentials", ReadOnly: true,
		})
	}

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentNotification, ImageName: "notification",
		Resources: kn.Spec.Notification.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
	})
}

// KubernautAgentDeployment builds the kubernaut-agent Deployment.
func KubernautAgentDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	if kn.Spec.KubernautAgent.LLM.CredentialsSecretName == "" {
		return nil, fmt.Errorf("spec.kubernautAgent.llm.credentialsSecretName must not be empty")
	}
	volumes := []corev1.Volume{
		configMapVolume("config", "kubernaut-agent-config"),
		configMapVolume("sdk-config", KubernautAgentSDKConfigName(kn)),
		secretVolume("llm-credentials", kn.Spec.KubernautAgent.LLM.CredentialsSecretName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut-agent", ReadOnly: true},
		{Name: "sdk-config", MountPath: "/etc/kubernaut-agent/sdk", ReadOnly: true},
		{Name: "llm-credentials", MountPath: "/etc/kubernaut-agent/credentials", ReadOnly: true},
	}

	envVars := []corev1.EnvVar{
		{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/etc/kubernaut-agent/credentials/credentials.json"},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		volumes = append(volumes, configMapVolume("service-ca", "kubernaut-agent-service-ca"))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "service-ca", MountPath: "/etc/ssl/ka", ReadOnly: true,
		})
		envVars = append(envVars, corev1.EnvVar{Name: "IS_OPENSHIFT", Value: "True"})
	}

	volumes, mounts, envVars = appendInterServiceTLSCA(volumes, mounts, envVars)

	res := kn.Spec.KubernautAgent.Resources
	if len(res.Requests) == 0 && len(res.Limits) == 0 {
		res = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentKubernautAgent, ImageName: "kubernaut-agent",
		Resources: res, VolumeMounts: mounts, Volumes: volumes, Env: envVars,
		Args: []string{
			"-config", "/etc/kubernaut-agent/config.yaml",
			"-sdk-config", "/etc/kubernaut-agent/sdk/sdk-config.yaml",
		},
	})
}

// AuthWebhookDeployment builds the authwebhook Deployment.
func AuthWebhookDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{
		configMapVolume("config", "authwebhook-config"),
		secretVolume("webhook-certs", "authwebhook-tls"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/authwebhook", ReadOnly: true},
		{Name: "webhook-certs", MountPath: "/tmp/k8s-webhook-server/serving-certs", ReadOnly: true},
	}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentAuthWebhook, ImageName: "authwebhook",
		Resources: kn.Spec.AuthWebhook.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env,
		Ports: []corev1.ContainerPort{
			{Name: "webhook", ContainerPort: PortWebhookServer, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
		ProbePort: PortHealthProbe,
	})
}

// --- internal helpers ---

// DeploymentParams collects the parameters for building a workload Deployment,
// replacing what was previously a 9-argument function signature.
type DeploymentParams struct {
	Component      string
	ImageName      string
	Resources      corev1.ResourceRequirements
	VolumeMounts   []corev1.VolumeMount
	Volumes        []corev1.Volume
	InitContainers []corev1.Container
	Ports          []corev1.ContainerPort
	Env            []corev1.EnvVar
	Args           []string
	ProbePort      int32
}

func buildDeployment(kn *kubernautv1alpha1.Kubernaut, p DeploymentParams) (*appsv1.Deployment, error) {
	img, err := Image(&kn.Spec.Image, p.ImageName)
	if err != nil {
		return nil, err
	}

	ports := p.Ports
	if len(ports) == 0 {
		ports = []corev1.ContainerPort{{Name: "http", ContainerPort: PortHTTP, Protocol: corev1.ProtocolTCP}}
	}

	probePort := p.ProbePort
	if probePort == 0 {
		probePort = ports[0].ContainerPort
	}
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(probePort),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}

	return &appsv1.Deployment{
		ObjectMeta: ObjectMeta(kn, p.Component+"-deployment", p.Component),
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(p.Component)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: ComponentLabels(kn, p.Component)},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountName(p.Component),
					SecurityContext:    PodSecurityContext(),
					ImagePullSecrets:   kn.Spec.Image.PullSecrets,
					InitContainers:     p.InitContainers,
					Containers: []corev1.Container{{
						Name:            p.Component,
						Image:           img,
						ImagePullPolicy: kn.Spec.Image.PullPolicy,
						Ports:           ports,
						Env:             p.Env,
						Args:            p.Args,
						Resources:       MergeResources(p.Resources),
						SecurityContext: ContainerSecurityContext(),
						VolumeMounts:    p.VolumeMounts,
						LivenessProbe:   probe,
						ReadinessProbe:  probe,
					}},
					Volumes: p.Volumes,
				},
			},
		},
	}, nil
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

func appendInterServiceTLSCA(volumes []corev1.Volume, mounts []corev1.VolumeMount, env []corev1.EnvVar) ([]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	volumes = append(volumes, configMapVolume("tls-ca", InterServiceCAConfigMapName))
	mounts = append(mounts, corev1.VolumeMount{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true})
	env = append(env, corev1.EnvVar{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile})
	return volumes, mounts, env
}
