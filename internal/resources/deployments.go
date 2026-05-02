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
// Issue #753: Gateway no longer terminates TLS at the application layer —
// the OCP Route handles TLS termination for external traffic. The pod only
// mounts the inter-service CA for outbound client trust (TLS_CA_FILE).
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
		{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile},
	}

	volumes := []corev1.Volume{
		configMapVolume("config", "gateway-config"),
		optionalConfigMapVolume("tls-ca", InterServiceCAConfigMapName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/gateway", ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentGateway, ImageName: "gateway",
		Resources: kn.Spec.Gateway.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env:       env,
		Args:      []string{"--config=/etc/gateway/config.yaml"},
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: PortHTTP, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
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

	volumes := make([]corev1.Volume, 0, 4)
	volumes = append(volumes,
		configMapVolume("config", "datastorage-config"),
		corev1.Volume{
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
	)

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
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: PortHTTP, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
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
	env := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/aianalysis/config.yaml"},
	}
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentAIAnalysis, ImageName: "aianalysis",
		Resources: kn.Spec.AIAnalysis.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args: []string{"-config", "/etc/aianalysis/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: PortHTTP, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
	})
}

// SignalProcessingDeployment builds the signalprocessing Deployment.
func SignalProcessingDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	policyName := SignalProcessingPolicyName(kn)
	volumes := make([]corev1.Volume, 0, 3)
	volumes = append(volumes,
		configMapVolume("config", "signalprocessing-config"),
		configMapVolume("policy", policyName),
	)
	mounts := make([]corev1.VolumeMount, 0, 3)
	mounts = append(mounts,
		corev1.VolumeMount{Name: "config", MountPath: "/etc/signalprocessing/config.yaml", SubPath: "config.yaml"},
		corev1.VolumeMount{Name: "policy", MountPath: "/etc/signalprocessing/policies", ReadOnly: true},
	)

	proactiveCMName := "signalprocessing-proactive-signal-mappings"
	if kn.Spec.SignalProcessing.ProactiveSignalMappings != nil {
		proactiveCMName = kn.Spec.SignalProcessing.ProactiveSignalMappings.ConfigMapName
	}
	volumes = append(volumes, configMapVolume("proactive-mappings", proactiveCMName))
	mounts = append(mounts, corev1.VolumeMount{
		Name: "proactive-mappings", MountPath: "/etc/signalprocessing/proactive-signal-mappings.yaml", SubPath: "proactive-signal-mappings.yaml",
	})

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentSignalProcessing, ImageName: "signalprocessing",
		Resources: kn.Spec.SignalProcessing.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args: []string{"--config=/etc/signalprocessing/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
	})
}

// RemediationOrchestratorDeployment builds the remediationorchestrator Deployment.
func RemediationOrchestratorDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "remediationorchestrator-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/config", ReadOnly: true}}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentRemediationOrchestrator, ImageName: "remediationorchestrator",
		Resources: kn.Spec.RemediationOrchestrator.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
		Args:      []string{"--config=/etc/config/remediationorchestrator.yaml"},
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
	})
}

// WorkflowExecutionDeployment builds the workflowexecution Deployment.
func WorkflowExecutionDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "workflowexecution-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/config", ReadOnly: true}}
	var env []corev1.EnvVar
	var initContainers []corev1.Container

	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)

	if ref := kn.Spec.Ansible.CACertSecretRef; ref != nil {
		key := ref.Key
		if key == "" {
			key = "ca.crt"
		}
		volumes = append(volumes,
			corev1.Volume{
				Name: "aap-ca",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ref.Name,
						Items:      []corev1.KeyToPath{{Key: key, Path: "aap-ca.crt"}},
					},
				},
			},
			corev1.Volume{
				Name:         "combined-ca",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			},
		)
		mounts = append(mounts, corev1.VolumeMount{
			Name: "combined-ca", MountPath: "/etc/combined-ca", ReadOnly: true,
		})
		initContainers = append(initContainers, corev1.Container{
			Name:    "build-ca-bundle",
			Image:   "registry.access.redhat.com/ubi10/ubi-minimal@sha256:2a4785f399dc7ae2f3ca85f68bac0ccac47f3e73464a47c21e4f7ae46b55a053",
			Command: []string{"sh", "-c"},
			Args:    []string{"cat /etc/tls-ca/service-ca.crt /aap-ca/aap-ca.crt > /combined/ca-bundle.crt"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
				{Name: "aap-ca", MountPath: "/aap-ca", ReadOnly: true},
				{Name: "combined-ca", MountPath: "/combined"},
			},
			SecurityContext: ContainerSecurityContext(),
		})
		env = overrideTLSCAFile(env, "/etc/combined-ca/ca-bundle.crt")
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentWorkflowExecution, ImageName: "workflowexecution",
		Resources: kn.Spec.WorkflowExecution.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
		InitContainers: initContainers,
		Args:           []string{"--config=/etc/config/workflowexecution.yaml"},
		ProbePort:      PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
	})
}

// EffectivenessMonitorDeployment builds the effectivenessmonitor Deployment.
// When OCP monitoring is enabled, a wait-for-service-ca init container is
// included to block startup until the service-CA ConfigMap is populated,
// preventing CrashLoopBackOff on fresh installs where the CA injection is
// asynchronous.
func EffectivenessMonitorDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{
		configMapVolume("config", "effectivenessmonitor-config"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/effectivenessmonitor", ReadOnly: true},
	}

	var initContainers []corev1.Container
	if kn.Spec.Monitoring.MonitoringEnabled() {
		volumes = append(volumes, configMapVolume("service-ca", "effectivenessmonitor-service-ca"))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "service-ca", MountPath: "/etc/ssl/em", ReadOnly: true,
		})
		initContainers = append(initContainers, corev1.Container{
			Name:            "wait-for-service-ca",
			Image:           "registry.access.redhat.com/ubi10/ubi-minimal@sha256:2a4785f399dc7ae2f3ca85f68bac0ccac47f3e73464a47c21e4f7ae46b55a053",
			ImagePullPolicy: kn.Spec.Image.PullPolicy,
			Command:         []string{"sh", "-c"},
			Args: []string{
				`while [ ! -s /etc/ssl/em/service-ca.crt ]; do echo "waiting for service-ca.crt..."; sleep 2; done`,
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "service-ca", MountPath: "/etc/ssl/em", ReadOnly: true},
			},
			SecurityContext: ContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("16Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
			},
		})
	}

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentEffectivenessMonitor, ImageName: "effectivenessmonitor",
		Resources: kn.Spec.EffectivenessMonitor.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, InitContainers: initContainers,
		Args:      []string{"--config=/etc/effectivenessmonitor/effectivenessmonitor.yaml"},
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
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

	env := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/notification/config.yaml"},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
	}
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentNotification, ImageName: "notification",
		Resources: kn.Spec.Notification.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args: []string{"-config", "/etc/notification/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
	})
}

// KubernautAgentDeployment builds the kubernaut-agent Deployment.
// Issue #753: kubernaut-agent now serves TLS on port 8080 with certs from
// kubernautagent-tls (provisioned by OCP service-ca). Health and metrics
// are on dedicated plain HTTP ports 8081 and 9090.
func KubernautAgentDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	if kn.Spec.KubernautAgent.LLM.CredentialsSecretName == "" {
		return nil, fmt.Errorf("spec.kubernautAgent.llm.credentialsSecretName must not be empty")
	}
	volumes := []corev1.Volume{
		configMapVolume("config", "kubernaut-agent-config"),
		configMapVolume("llm-runtime", KubernautAgentLLMRuntimeConfigName(kn)),
		secretVolume("llm-credentials", kn.Spec.KubernautAgent.LLM.CredentialsSecretName),
		secretVolume("tls-certs", KubernautAgentTLSSecretName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/kubernaut-agent", ReadOnly: true},
		{Name: "llm-runtime", MountPath: "/etc/kubernaut-agent/llm-runtime", ReadOnly: true},
		{Name: "llm-credentials", MountPath: "/etc/kubernaut-agent/credentials", ReadOnly: true},
		{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
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

		volumes = append(volumes, corev1.Volume{
			Name:         "combined-ca",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name: "combined-ca", MountPath: "/etc/ssl/combined", ReadOnly: true,
		})
		envVars = append(envVars, corev1.EnvVar{
			Name: "SSL_CERT_FILE", Value: "/etc/ssl/combined/ca-bundle.crt",
		})
	}

	volumes, mounts, envVars = appendInterServiceTLSCA(volumes, mounts, envVars)

	if kn.Spec.KubernautAgent.LLM.OAuth2.Enabled {
		volumes = append(volumes, secretVolume("oauth2-credentials", kn.Spec.KubernautAgent.LLM.OAuth2.CredentialsSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "oauth2-credentials", MountPath: "/etc/kubernaut-agent/oauth2", ReadOnly: true,
		})
	}

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

	var initContainers []corev1.Container
	if kn.Spec.Monitoring.MonitoringEnabled() {
		initContainers = append(initContainers, corev1.Container{
			Name:  "build-ca-bundle",
			Image: "registry.access.redhat.com/ubi10/ubi-minimal@sha256:2a4785f399dc7ae2f3ca85f68bac0ccac47f3e73464a47c21e4f7ae46b55a053",
			Command: []string{"sh", "-c",
				"cat /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /service-ca/service-ca.crt > /combined/ca-bundle.crt",
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "service-ca", MountPath: "/service-ca", ReadOnly: true},
				{Name: "combined-ca", MountPath: "/combined"},
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsNonRoot:             ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
		})
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentKubernautAgent, ImageName: "kubernautagent",
		Resources: res, VolumeMounts: mounts, Volumes: volumes, Env: envVars,
		InitContainers: initContainers,
		ProbePort:      PortHealthProbe,
		Args: []string{
			"-config", "/etc/kubernaut-agent/config.yaml",
			"-llm-runtime", "/etc/kubernaut-agent/llm-runtime/llm-runtime.yaml",
		},
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: PortHTTP, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
	})
}

// AuthWebhookDeployment builds the authwebhook Deployment.
//
// OPERATIONAL NOTE — Admission blackout during rollout:
// The deployment uses the Recreate strategy (matching the Helm chart) to
// avoid TLS certificate routing conflicts between old and new pods. This
// means that during a rollout the old pod is terminated before the new one
// starts, creating a brief window (~15-30 s, depending on readiness probe
// timing) where admission requests to the authwebhook will fail. Because
// the webhook FailurePolicy is set to Fail, any Kubernaut CRD mutations
// (WorkflowExecution status, RemediationApprovalRequest status,
// RemediationRequest status, NotificationRequest deletions,
// RemediationWorkflow CUD, ActionType CUD) will be rejected during this
// window. SREs should plan operator upgrades during low-activity windows.
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
		Env:  env,
		Args: []string{"-config=/etc/authwebhook/authwebhook.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "webhook", ContainerPort: PortWebhookServer, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		},
		ProbePort: PortHealthProbe,
		Strategy:  &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
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
	Strategy       *appsv1.DeploymentStrategy
	LivenessPath   string
	ReadinessPath  string
	PodAnnotations map[string]string
}

func buildDeployment(kn *kubernautv1alpha1.Kubernaut, p DeploymentParams) (*appsv1.Deployment, error) {
	img, err := ResolveImage(kn, p.ImageName)
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

	pc := probeConfigForComponent(p.Component)
	if p.LivenessPath != "" {
		pc.LivenessPath = p.LivenessPath
	}
	if p.ReadinessPath != "" {
		pc.ReadinessPath = p.ReadinessPath
	}

	liveness := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: pc.LivenessPath,
				Port: intstr.FromInt32(probePort),
			},
		},
		InitialDelaySeconds: pc.LivenessInitialDelay,
		PeriodSeconds:       pc.LivenessPeriod,
		TimeoutSeconds:      pc.LivenessTimeout,
		FailureThreshold:    pc.LivenessFailureThreshold,
	}
	readiness := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: pc.ReadinessPath,
				Port: intstr.FromInt32(probePort),
			},
		},
		InitialDelaySeconds: pc.ReadinessInitialDelay,
		PeriodSeconds:       pc.ReadinessPeriod,
		TimeoutSeconds:      pc.ReadinessTimeout,
		FailureThreshold:    pc.ReadinessFailureThreshold,
	}

	dep := &appsv1.Deployment{
		ObjectMeta: ObjectMeta(kn, DeploymentName(p.Component), p.Component),
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(p.Component)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      ComponentLabels(kn, p.Component),
					Annotations: p.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountName(p.Component),
					SecurityContext:    PodSecurityContext(),
					ImagePullSecrets:   kn.Spec.Image.PullSecrets,
					Affinity:           preferredPodAntiAffinity(p.Component),
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
						LivenessProbe:   liveness,
						ReadinessProbe:  readiness,
					}},
					Volumes: p.Volumes,
				},
			},
		},
	}

	if p.Strategy != nil {
		dep.Spec.Strategy = *p.Strategy
	}

	return dep, nil
}

func preferredPodAntiAffinity(component string) *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: SelectorLabels(component),
						},
						TopologyKey: "kubernetes.io/hostname",
					},
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

func optionalConfigMapVolume(name, cmName string) corev1.Volume {
	optional := true
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
				Optional:             &optional,
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

func overrideTLSCAFile(env []corev1.EnvVar, path string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == "TLS_CA_FILE" {
			env[i].Value = path
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: "TLS_CA_FILE", Value: path})
}

// ProbeConfig holds per-component HTTP GET probe paths and timing, mirroring
// the Helm chart values exactly so that cold-start and resource-constrained
// nodes don't see premature restarts.
type ProbeConfig struct {
	LivenessPath              string
	LivenessInitialDelay      int32
	LivenessPeriod            int32
	LivenessTimeout           int32
	LivenessFailureThreshold  int32
	ReadinessPath             string
	ReadinessInitialDelay     int32
	ReadinessPeriod           int32
	ReadinessTimeout          int32
	ReadinessFailureThreshold int32
}

// probeConfigForComponent returns the probe configuration for a given
// component, matching the Helm chart (v1.3.0-rc11) exactly.
// Issue #753: Gateway, DataStorage, and KubernautAgent probes moved to the
// dedicated health port (8081) using /healthz and /readyz paths.
func probeConfigForComponent(component string) ProbeConfig {
	switch component {
	case ComponentGateway:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 10, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 30, ReadinessPeriod: 5, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
	case ComponentDataStorage:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 30, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 30, ReadinessPeriod: 5, ReadinessTimeout: 3, ReadinessFailureThreshold: 3,
		}
	case ComponentAIAnalysis:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 30, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/healthz", ReadinessInitialDelay: 30, ReadinessPeriod: 5, ReadinessTimeout: 3, ReadinessFailureThreshold: 3,
		}
	case ComponentKubernautAgent:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 10, ReadinessPeriod: 10, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
	case ComponentEffectivenessMonitor:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 10, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 5, ReadinessPeriod: 5, ReadinessTimeout: 5, ReadinessFailureThreshold: 3,
		}
	case ComponentNotification:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 15, ReadinessPeriod: 10, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
	case ComponentAuthWebhook:
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 15, ReadinessPeriod: 10, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
	default:
		// SignalProcessing, RemediationOrchestrator, WorkflowExecution
		return ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 5, ReadinessPeriod: 10, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
	}
}
