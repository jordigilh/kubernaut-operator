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
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// GatewayDeployment builds the gateway Deployment.
// Issue #126: Gateway serves TLS on 8443 using the OCP service-ca provisioned
// gateway-tls Secret (FedRAMP SC-8 — encryption in transit for inter-service traffic).
func GatewayDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	env := []corev1.EnvVar{
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
		{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile},
		{Name: "SSL_CERT_FILE", Value: InterServiceTLSCAFile},
	}

	volumes := []corev1.Volume{
		configMapVolume("config", "gateway-config"),
		secretVolume("tls-certs", GatewayTLSSecretName),
		optionalConfigMapVolume("tls-ca", TrustBundleConfigMapName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/gateway", ReadOnly: true},
		{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
	}
	volumes, mounts = appendFleetSecretMounts(volumes, mounts, knV2, "/etc/gateway", effectiveFleetOAuth2SecretRef(knV2.Spec.Gateway.Fleet, ""))

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentGateway, ImageName: "gateway",
		Resources: kn.Spec.Gateway.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env:       env,
		Args:      []string{"--config=/etc/gateway/config.yaml"},
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "https", ContainerPort: PortHTTPS, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
	})
}

// DataStorageDeployment builds the data-storage Deployment with init container
// for database readiness and projected secrets volume.
func DataStorageDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	initContainer, err := dataStorageInitContainer(kn)
	if err != nil {
		return nil, err
	}
	volumes, mounts := dataStorageVolumesAndMounts(kn)

	sslMode := withDefault(kn.Spec.PostgreSQL.SSLMode, DefaultSSLMode)
	env := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/datastorage/config.yaml"},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
		{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile},
		{Name: "SSL_CERT_FILE", Value: InterServiceTLSCAFile},
	}
	if sslMode == DefaultSSLMode {
		env = append(env, corev1.EnvVar{Name: "PGSSLROOTCERT", Value: InterServiceTLSCAFile})
	}

	var gracePeriod int64 = 60
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentDataStorage, ImageName: "datastorage",
		Resources: kn.Spec.DataStorage.Resources, VolumeMounts: mounts, Volumes: volumes,
		InitContainers: []corev1.Container{initContainer}, Env: env,
		ProbePort: PortHealthProbe,
		Ports: []corev1.ContainerPort{
			{Name: "https", ContainerPort: PortHTTPS, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
		TerminationGracePeriodSeconds: &gracePeriod,
	})
}

// dataStorageInitContainer builds the wait-for-postgres init container that
// blocks pod startup until the PostgreSQL endpoint accepts TCP connections.
func dataStorageInitContainer(kn *kubernautv1alpha1.Kubernaut) (corev1.Container, error) {
	pgPort := PostgreSQLPort(kn)

	// Resolve PostgreSQL hostname to a ClusterIP so the init container can
	// connect without DNS — glibc's resolver is broken under OVN-Kubernetes
	// NetworkPolicies. Go's built-in resolver works because it queries DNS
	// natively over UDP.
	pgHost := kn.Spec.PostgreSQL.Host
	// Best-effort, bounded lookup with no natural deadline of its own (any
	// error, including a cancelled context, falls back to the configured
	// host below), so context.Background() is used rather than threading
	// ctx through the resource-builder call chain.
	if addrs, err := net.DefaultResolver.LookupHost(context.Background(), pgHost); err == nil && len(addrs) > 0 {
		pgHost = addrs[0]
	}

	migrateImage, err := ResolveImage(kn, "db-migrate")
	if err != nil {
		return corev1.Container{}, err
	}
	return corev1.Container{
		Name:            "wait-for-postgres",
		Image:           migrateImage,
		ImagePullPolicy: kn.Spec.Image.PullPolicy,
		Command: []string{"bash", "-c",
			`until bash -c "echo >/dev/tcp/$PGHOST/$PGPORT" 2>/dev/null; do echo "waiting for postgres at $PGHOST:$PGPORT"; sleep 2; done`,
		},
		Env: []corev1.EnvVar{
			{Name: "PGHOST", Value: pgHost},
			{Name: "PGPORT", Value: strconv.FormatInt(int64(pgPort), 10)},
		},
		SecurityContext: ContainerSecurityContext(),
		Resources:       DefaultResources(),
	}, nil
}

// dataStorageVolumesAndMounts builds the config/secrets/TLS/scratch volumes
// for the data-storage Deployment, plus the optional Valkey-TLS and
// signing-cert volumes gated on spec fields.
func dataStorageVolumesAndMounts(kn *kubernautv1alpha1.Kubernaut) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := make([]corev1.Volume, 0, 6)
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
		configMapVolume("tls-ca", TrustBundleConfigMapName),
		corev1.Volume{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	)

	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/datastorage", ReadOnly: true},
		{Name: "secrets", MountPath: "/etc/datastorage/secrets", ReadOnly: true},
		{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "data", MountPath: "/data"},
	}

	if kn.Spec.Valkey.ValkeyTLSEnabled() {
		if kn.Spec.Valkey.TLS.CASecretName != "" {
			volumes = append(volumes, secretVolume("valkey-ca", kn.Spec.Valkey.TLS.CASecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "valkey-ca", MountPath: "/etc/valkey-tls/ca", ReadOnly: true})
		}
		if kn.Spec.Valkey.TLS.ClientCertSecretName != "" {
			volumes = append(volumes, secretVolume("valkey-client-cert", kn.Spec.Valkey.TLS.ClientCertSecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "valkey-client-cert", MountPath: "/etc/valkey-tls/client", ReadOnly: true})
		}
	}

	if sc := kn.Spec.DataStorage.SigningCert; sc != nil {
		mountPath := sc.MountPath
		if mountPath == "" {
			mountPath = "/etc/certs"
		}
		volumes = append(volumes, secretVolume("signing-cert", sc.SecretName))
		mounts = append(mounts, corev1.VolumeMount{Name: "signing-cert", MountPath: mountPath, ReadOnly: true})
	} else {
		// When no explicit signing cert is configured, reuse the service-ca
		// serving cert so the data-storage binary finds a cert at /etc/certs.
		volumes = append(volumes, secretVolume("signing-cert", DataStorageTLSSecretName))
		mounts = append(mounts, corev1.VolumeMount{Name: "signing-cert", MountPath: "/etc/certs", ReadOnly: true})
	}

	return volumes, mounts
}

// AIAnalysisDeployment builds the aianalysis Deployment.
func AIAnalysisDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
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
	ports := make([]corev1.ContainerPort, 0, 4)
	ports = append(ports,
		corev1.ContainerPort{Name: "https", ContainerPort: PortHTTPS, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentAIAnalysis, ImageName: "aianalysis",
		Resources: kn.Spec.AIAnalysis.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args:  []string{"-config", "/etc/aianalysis/config.yaml"},
		Ports: ports,
	})
}

// SignalProcessingDeployment builds the signalprocessing Deployment.
func SignalProcessingDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
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
	volumes, mounts = appendMCPGatewayOnlyFleetSecretMount(volumes, mounts, knV2, "/etc/signalprocessing", effectiveFleetOAuth2SecretRef(knV2.Spec.SignalProcessing.Fleet, ""))
	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentSignalProcessing, ImageName: "signalprocessing",
		Resources: kn.Spec.SignalProcessing.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args:  []string{"--config=/etc/signalprocessing/config.yaml"},
		Ports: ports,
	})
}

// RemediationOrchestratorDeployment builds the remediationorchestrator Deployment.
func RemediationOrchestratorDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "remediationorchestrator-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/config", ReadOnly: true}}
	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	volumes, mounts = appendFleetSecretMounts(volumes, mounts, knV2, "/etc/remediationorchestrator", effectiveFleetOAuth2SecretRef(knV2.Spec.RemediationOrchestrator.Fleet, ""))
	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentRemediationOrchestrator, ImageName: "remediationorchestrator",
		Resources: kn.Spec.RemediationOrchestrator.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
		Args:      []string{"--config=/etc/config/remediationorchestrator.yaml"},
		ProbePort: PortHealthProbe,
		Ports:     ports,
	})
}

// WorkflowExecutionDeployment builds the workflowexecution Deployment.
func WorkflowExecutionDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{configMapVolume("config", "workflowexecution-config")}
	mounts := []corev1.VolumeMount{{Name: "config", MountPath: "/etc/config", ReadOnly: true}}
	var env []corev1.EnvVar
	var initContainers []corev1.Container

	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	volumes, mounts = appendWorkflowExecutionFleetSecretMount(volumes, mounts, knV2, "/etc/workflowexecution")

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
		ubiImage, ubiErr := ResolveImage(kn, "init-ubi-minimal")
		if ubiErr != nil {
			return nil, ubiErr
		}
		initContainers = append(initContainers, corev1.Container{
			Name:            "build-ca-bundle",
			Image:           ubiImage,
			ImagePullPolicy: kn.Spec.Image.PullPolicy,
			Command:         []string{"sh", "-c"},
			Args:            []string{"cat /etc/tls-ca/service-ca.crt /aap-ca/aap-ca.crt > /combined/ca-bundle.crt"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
				{Name: "aap-ca", MountPath: "/aap-ca", ReadOnly: true},
				{Name: "combined-ca", MountPath: "/combined"},
			},
			SecurityContext: ContainerSecurityContext(),
		})
		env = overrideTLSCAFile(env, "/etc/combined-ca/ca-bundle.crt")
	}

	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentWorkflowExecution, ImageName: "workflowexecution",
		Resources: kn.Spec.WorkflowExecution.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
		InitContainers: initContainers,
		Args:           []string{"--config=/etc/config/workflowexecution.yaml"},
		ProbePort:      PortHealthProbe,
		Ports:          ports,
	})
}

// EffectivenessMonitorDeployment builds the effectivenessmonitor Deployment.
// When OCP monitoring is enabled, a wait-for-service-ca init container is
// included to block startup until the service-CA ConfigMap is populated,
// preventing CrashLoopBackOff on fresh installs where the CA injection is
// asynchronous.
func EffectivenessMonitorDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	volumes := []corev1.Volume{
		configMapVolume("config", "effectivenessmonitor-config"),
		configMapVolume("service-ca", "effectivenessmonitor-service-ca"),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/effectivenessmonitor", ReadOnly: true},
		{Name: "service-ca", MountPath: "/etc/ssl/em", ReadOnly: true},
	}

	emUbiImage, emErr := ResolveImage(kn, "init-ubi-minimal")
	if emErr != nil {
		return nil, emErr
	}
	initContainers := []corev1.Container{{
		Name:            "wait-for-service-ca",
		Image:           emUbiImage,
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
	}}

	var env []corev1.EnvVar
	volumes, mounts, env = appendInterServiceTLSCA(volumes, mounts, env)
	volumes, mounts = appendMCPGatewayOnlyFleetSecretMount(volumes, mounts, knV2, "/etc/effectivenessmonitor", effectiveFleetOAuth2SecretRef(knV2.Spec.EffectivenessMonitor.Fleet, ""))
	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentEffectivenessMonitor, ImageName: "effectivenessmonitor",
		Resources: kn.Spec.EffectivenessMonitor.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, InitContainers: initContainers,
		Args:      []string{"--config=/etc/effectivenessmonitor/effectivenessmonitor.yaml"},
		ProbePort: PortHealthProbe,
		Ports:     ports,
	})
}

// NotificationDeployment builds the notification Deployment.
func NotificationDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	routingCMName := "notification-routing-config"
	if kn.Spec.Notification.Routing != nil && kn.Spec.Notification.Routing.ConfigMapName != "" {
		routingCMName = kn.Spec.Notification.Routing.ConfigMapName
	}
	volumes := []corev1.Volume{
		configMapVolume("config", "notification-controller-config"),
		configMapVolume("routing-config", routingCMName),
		{Name: "notification-output", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/notification", ReadOnly: true},
		{Name: "routing-config", MountPath: "/etc/notification-routing", ReadOnly: true},
		{Name: "notification-output", MountPath: "/tmp/notifications"},
	}

	if kn.Spec.Notification.Slack.SecretName != "" {
		optional := true
		volumes = append(volumes, corev1.Volume{
			Name: "credentials",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: kn.Spec.Notification.Slack.SecretName},
							Items:                []corev1.KeyToPath{{Key: "webhook-url", Path: "slack-webhook"}},
							Optional:             &optional,
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
	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentNotification, ImageName: "notification",
		Resources: kn.Spec.Notification.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env: env, ProbePort: PortHealthProbe,
		Args:  []string{"-config", "/etc/notification/config.yaml"},
		Ports: ports,
	})
}

// KubernautAgentDeployment builds the kubernaut-agent Deployment.
// kubernaut-agent serves TLS on port 8443 with certs from
// kubernautagent-tls (provisioned by OCP service-ca). Health and metrics
// are on dedicated plain HTTP ports 8081 and 9090.
func KubernautAgentDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	kaProfile, _ := ResolveLLMProfile(kn, EffectiveKALLMProfileRef(kn))
	if kaProfile.CredentialsSecretName == "" {
		return nil, fmt.Errorf("spec.kubernautAgent.llmProfileRef's profile must have a non-empty credentialsSecretName")
	}

	volumes, mounts, envVars := kaCoreVolumesMountsEnv(kn, kaProfile)
	volumes, mounts, envVars = appendInterServiceTLSCA(volumes, mounts, envVars)
	volumes, mounts = kaCredentialVolumesAndMounts(kn, kaProfile, volumes, mounts)
	// #204: componentEtcDir is deliberately "/etc/kubernautagent"
	// (unhyphenated), NOT "/etc/kubernaut-agent" like every other mount
	// above. Upstream's registerFleetTools() hardcodes the fleet-oauth2
	// credentials lookup to "/etc/kubernautagent/<credentialsSecretRef>"
	// literally -- it is not derived from KA's -config flag directory, so
	// the mount path here must match that hardcoded string exactly.
	volumes, mounts = appendMCPGatewayOnlyFleetSecretMount(volumes, mounts, knV2, "/etc/kubernautagent", effectiveFleetOAuth2SecretRef(knV2.Spec.KubernautAgent.Fleet, ""))

	initContainers, err := kaInitContainers(kn)
	if err != nil {
		return nil, err
	}

	saTokenVolume, saTokenMount := kaServiceAccountTokenVolume()
	volumes = append(volumes, saTokenVolume)
	mounts = append(mounts, saTokenMount)

	drainSec := int64(30)
	if kn.Spec.KubernautAgent.Shutdown.DrainSeconds != nil {
		drainSec = int64(*kn.Spec.KubernautAgent.Shutdown.DrainSeconds)
	}
	gracePeriod := drainSec + 5

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentKubernautAgent, ImageName: "kubernautagent",
		Resources: kaResources(kn), VolumeMounts: mounts, Volumes: volumes, Env: envVars,
		InitContainers: initContainers,
		ProbePort:      PortHealthProbe,
		Args: []string{
			"-config", "/etc/kubernaut-agent/config.yaml",
			"-llm-runtime", "/etc/kubernaut-agent/llm-runtime/llm-runtime.yaml",
		},
		Ports: []corev1.ContainerPort{
			{Name: "https", ContainerPort: PortHTTPS, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
		PodAnnotations: map[string]string{
			"prometheus.io/scrape": "true",
			"prometheus.io/port":   "9090",
			"prometheus.io/path":   "/metrics",
		},
		TerminationGracePeriodSeconds: &gracePeriod,
	})
}

// kaCoreVolumesMountsEnv builds kubernaut-agent's baseline config/LLM-
// credentials/TLS volumes and mounts, plus the extra service-ca/combined-CA
// volumes and IS_OPENSHIFT/SSL_CERT_FILE env vars needed when cluster
// monitoring is enabled (KA's Prometheus/Alertmanager tools need OpenShift's
// service-ca-injected trust bundle).
func kaCoreVolumesMountsEnv(kn *kubernautv1alpha1.Kubernaut, kaProfile kubernautv1alpha1.LLMProfileSpec) (
	[]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	volumes := make([]corev1.Volume, 0, 7)
	volumes = append(volumes,
		corev1.Volume{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		configMapVolume("config", "kubernaut-agent-config"),
		configMapVolume("llm-runtime", KubernautAgentLLMRuntimeConfigName(kn)),
		secretVolume("llm-credentials", kaProfile.CredentialsSecretName),
		secretVolume("tls-certs", KubernautAgentTLSSecretName),
	)
	mounts := make([]corev1.VolumeMount, 0, 7)
	mounts = append(mounts,
		corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"},
		corev1.VolumeMount{Name: "config", MountPath: "/etc/kubernaut-agent", ReadOnly: true},
		corev1.VolumeMount{Name: "llm-runtime", MountPath: "/etc/kubernaut-agent/llm-runtime", ReadOnly: true},
		corev1.VolumeMount{Name: "llm-credentials", MountPath: "/etc/kubernaut-agent/credentials", ReadOnly: true},
		corev1.VolumeMount{Name: "tls-certs", MountPath: InterServiceTLSCertDir, ReadOnly: true},
	)
	envVars := make([]corev1.EnvVar, 0, 3)
	envVars = append(envVars,
		corev1.EnvVar{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/etc/kubernaut-agent/credentials/credentials.json"},
	)

	volumes = append(volumes,
		configMapVolume("service-ca", "kubernaut-agent-service-ca"),
		corev1.Volume{Name: "combined-ca", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	)
	mounts = append(mounts,
		corev1.VolumeMount{Name: "service-ca", MountPath: "/etc/ssl/ka", ReadOnly: true},
		corev1.VolumeMount{Name: "combined-ca", MountPath: "/etc/ssl/combined", ReadOnly: true},
	)
	envVars = append(envVars,
		corev1.EnvVar{Name: "IS_OPENSHIFT", Value: "True"},
		corev1.EnvVar{Name: "SSL_CERT_FILE", Value: "/etc/ssl/combined/ca-bundle.crt"},
	)
	return volumes, mounts, envVars
}

// kaCredentialVolumesAndMounts appends the optional OAuth2, LLM-mTLS-client,
// and per-phase cross-credential Secret volumes to volumes/mounts.
func kaCredentialVolumesAndMounts(kn *kubernautv1alpha1.Kubernaut, kaProfile kubernautv1alpha1.LLMProfileSpec,
	volumes []corev1.Volume, mounts []corev1.VolumeMount) ([]corev1.Volume, []corev1.VolumeMount) {
	if kaProfile.OAuth2.Enabled {
		volumes = append(volumes, secretVolume("oauth2-credentials", kaProfile.OAuth2.CredentialsSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "oauth2-credentials", MountPath: "/etc/kubernaut-agent/oauth2", ReadOnly: true,
		})
	}

	if kaProfile.TLSClientSecretRef != "" {
		volumes = append(volumes, secretVolume("llm-tls-client", kaProfile.TLSClientSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "llm-tls-client", MountPath: "/etc/kubernaut-agent/llm-tls-client", ReadOnly: true,
		})
	}

	// #233: mount a dedicated Secret volume per phase override whose own
	// profile names a credentialsSecretName different from KA's base
	// profile -- KA resolves each phase's own apiKeyFile independently
	// (kubernaut#1728), so cross-credential phases need their own mount
	// rather than reusing KA's "llm-credentials" volume. Iterate phases in
	// sorted order so the Volumes/VolumeMounts slices -- and therefore the
	// rendered Deployment -- are deterministic regardless of Go's
	// randomized map iteration order.
	phases := make([]string, 0, len(kn.Spec.KubernautAgent.PhaseModels))
	for phase := range kn.Spec.KubernautAgent.PhaseModels {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	for _, phase := range phases {
		phaseProfile, _ := ResolveLLMProfile(kn, kn.Spec.KubernautAgent.PhaseModels[phase])
		if phaseProfile.CredentialsSecretName == "" || phaseProfile.CredentialsSecretName == kaProfile.CredentialsSecretName {
			continue
		}
		volumeName := phaseCredentialsVolumeName(phase)
		volumes = append(volumes, secretVolume(volumeName, phaseProfile.CredentialsSecretName))
		mounts = append(mounts, corev1.VolumeMount{
			Name: volumeName, MountPath: phaseCredentialsMountPath(phase), ReadOnly: true,
		})
	}
	return volumes, mounts
}

// kaResources returns the administrator-configured resource requirements
// for kubernaut-agent, or documented defaults when unset.
func kaResources(kn *kubernautv1alpha1.Kubernaut) corev1.ResourceRequirements {
	res := kn.Spec.KubernautAgent.Resources
	if len(res.Requests) > 0 || len(res.Limits) > 0 {
		return res
	}
	return corev1.ResourceRequirements{
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

// kaInitContainers returns the build-ca-bundle init container, which
// concatenates the base image's system trust bundle with the router-
// inclusive inter-service trust bundle (tls-ca, sourced from
// TrustBundleConfigMapName) into one file so KA's single SSL_CERT_FILE
// declaration is a superset covering both public-CA LLM providers and the
// fleet MCP client's Route-based endpoints (#404). Reads from tls-ca rather
// than kubernaut-agent-service-ca's own narrower service-ca-only bundle,
// since tls-ca's content is a strict superset (service-ca + router CA).
func kaInitContainers(kn *kubernautv1alpha1.Kubernaut) ([]corev1.Container, error) {
	kaUbiImage, err := ResolveImage(kn, "init-ubi-minimal")
	if err != nil {
		return nil, err
	}
	return []corev1.Container{{
		Name:            "build-ca-bundle",
		Image:           kaUbiImage,
		ImagePullPolicy: kn.Spec.Image.PullPolicy,
		Command: []string{"sh", "-c",
			"cat /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /etc/tls-ca/service-ca.crt > /combined/ca-bundle.crt",
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
			{Name: "combined-ca", MountPath: "/combined"},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}}, nil
}

// kaServiceAccountTokenVolume builds the projected short-TTL ServiceAccount
// token volume that replaces kubernaut-agent's default automounted token.
// Requires AutomountServiceAccountToken=false on the kubernaut-agent-sa
// ServiceAccount (see serviceaccounts.go).
func kaServiceAccountTokenVolume() (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: "sa-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: ptr.To[int64](3600),
							Audience:          "https://kubernetes.default.svc",
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
							Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
							},
						},
					},
				},
			},
		},
	}
	mount := corev1.VolumeMount{
		Name: "sa-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true,
	}
	return volume, mount
}

// AuthWebhookDeployment builds the authwebhook Deployment.
//
// OPERATIONAL NOTE — Admission blackout during rollout:
// The deployment uses the Recreate strategy to
// avoid TLS certificate routing conflicts between old and new pods. This
// means that during a rollout the old pod is terminated before the new one
// starts, creating a brief window (~15-30 s, depending on readiness probe
// timing) where admission requests to the authwebhook will fail. Because
// the webhook FailurePolicy is set to Fail, any Kubernaut CRD mutations
// (WorkflowExecution status, RemediationApprovalRequest status,
// RemediationRequest status, NotificationRequest deletions,
// RemediationWorkflow CUD, ActionType CUD) will be rejected during this
// window. SREs should plan operator upgrades during low-activity windows.
func AuthWebhookDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
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

	ports := make([]corev1.ContainerPort, 0, 3)
	ports = append(ports,
		corev1.ContainerPort{Name: "webhook", ContainerPort: PortWebhookServer, Protocol: corev1.ProtocolTCP},
		corev1.ContainerPort{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
	)
	ports = append(ports, pprofContainerPort(knV2.Spec.Debug.PprofEnabled)...)
	return buildDeployment(kn, DeploymentParams{
		Component: ComponentAuthWebhook, ImageName: "authwebhook",
		Resources: kn.Spec.AuthWebhook.Resources, VolumeMounts: mounts, Volumes: volumes,
		Env:       env,
		Args:      []string{"-config=/etc/authwebhook/authwebhook.yaml"},
		Ports:     ports,
		ProbePort: PortHealthProbe,
		Strategy:  &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
	})
}

// APIFrontendDeployment builds the apifrontend Deployment.
// The AF service exposes HTTPS (8443), health (8081), and metrics (9090) ports.
// It mounts projected config (config.yaml + rbac_roles.yaml), TLS server cert,
// and CA cert for inter-service trust.
// apifrontendVolumesMountsEnv builds all of API Frontend's config/TLS/
// credential volumes, mounts, and env vars: the sidecar-aware NO_PROXY
// setting, monitoring's service-ca mount, AF's own resolved LLM profile's
// credentials/mTLS-client/OAuth2 mounts, severity-triage's independently
// resolved cross-credential mount, and Valkey secrets.
func apifrontendVolumesMountsEnv(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) (
	[]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	volumes, mounts, env := apifrontendBaseVolumesMountsEnv(kn, sidecar)
	return apifrontendCredentialVolumesMountsEnv(kn, volumes, mounts, env)
}

// apifrontendBaseVolumesMountsEnv builds API Frontend's non-credential
// config/TLS-server/TLS-CA volumes and mounts, the sidecar-aware NO_PROXY
// env var, and monitoring's service-ca mount.
func apifrontendBaseVolumesMountsEnv(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) (
	[]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	ns := kn.Namespace
	env := []corev1.EnvVar{
		{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}},
		{Name: "TLS_CA_FILE", Value: "/etc/apifrontend/tls-ca/ca.crt"},
		// SSL_CERT_FILE points at AF's own merged (system+inter-service)
		// bundle, not the narrower /etc/apifrontend/tls-ca/ca.crt above
		// (#404) -- Go's SSL_CERT_FILE replaces rather than extends the
		// system trust store, and AF's severityTriage/LLM profile can point
		// at a public-CA provider (Vertex AI/Anthropic/OpenAI), which that
		// narrower, router+service-ca-only file doesn't cover. Built by the
		// build-ca-bundle init container below (mirrors KA's pattern).
		{Name: "SSL_CERT_FILE", Value: "/etc/ssl/combined/ca-bundle.crt"},
	}
	if sidecar != KagentiSidecarNone {
		noProxy := fmt.Sprintf("127.0.0.1,localhost,kubernaut-agent.%s.svc.cluster.local,data-storage-service.%s.svc.cluster.local", ns, ns)
		env = append(env, corev1.EnvVar{Name: "NO_PROXY", Value: noProxy})
	}

	volumes := []corev1.Volume{
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		configMapVolume("config", "apifrontend-config"),
		secretVolume("tls-server", APIFrontendTLSSecretName),
		{Name: "tls-ca", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: TrustBundleConfigMapName},
				Items:                []corev1.KeyToPath{{Key: "service-ca.crt", Path: "ca.crt"}},
				Optional:             ptr.To(true),
			},
		}},
		configMapVolume("service-ca", "apifrontend-service-ca"),
		{Name: "combined-ca", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "config", MountPath: "/etc/apifrontend", ReadOnly: true},
		{Name: "tls-server", MountPath: "/etc/apifrontend/tls", ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/apifrontend/tls-ca", ReadOnly: true},
		{Name: "service-ca", MountPath: "/etc/ssl/af", ReadOnly: true},
		{Name: "combined-ca", MountPath: "/etc/ssl/combined", ReadOnly: true},
	}
	return volumes, mounts, env
}

// afInitContainers returns the build-ca-bundle init container, which
// concatenates the base image's system trust bundle with AF's own
// inter-service trust bundle (mounted at /etc/apifrontend/tls-ca/ca.crt)
// into one file, mirroring KubernautAgent's pattern (#404). AF's own
// SSL_CERT_FILE (apifrontendBaseVolumesMountsEnv) points at this merged
// bundle rather than the narrower, router+service-ca-only tls-ca file.
func afInitContainers(kn *kubernautv1alpha1.Kubernaut) ([]corev1.Container, error) {
	afUbiImage, err := ResolveImage(kn, "init-ubi-minimal")
	if err != nil {
		return nil, err
	}
	return []corev1.Container{{
		Name:            "build-ca-bundle",
		Image:           afUbiImage,
		ImagePullPolicy: kn.Spec.Image.PullPolicy,
		Command: []string{"sh", "-c",
			"cat /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /etc/apifrontend/tls-ca/ca.crt > /combined/ca-bundle.crt",
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "tls-ca", MountPath: "/etc/apifrontend/tls-ca", ReadOnly: true},
			{Name: "combined-ca", MountPath: "/combined"},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}}, nil
}

// apifrontendCredentialVolumesMountsEnv appends AF's own resolved LLM
// profile's credentials/mTLS-client/OAuth2 mounts, severity-triage's
// independently resolved cross-credential mount, and Valkey secrets onto
// volumes/mounts/env.
func apifrontendCredentialVolumesMountsEnv(kn *kubernautv1alpha1.Kubernaut,
	volumes []corev1.Volume, mounts []corev1.VolumeMount, env []corev1.EnvVar) (
	[]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	// AF resolves its own LLM profile (AFLLMProfileRef defaults to KA's ref
	// when AF doesn't set its own), rather than reaching into KA's profile
	// unconditionally -- fixes the pre-refactor bug where AF always mounted
	// KA's llm-credentials/llm-tls-client Secrets regardless of which
	// profile AF's ConfigMap was actually configured to use.
	afProfile, _ := ResolveLLMProfile(kn, AFLLMProfileRef(kn))

	if secretName := afProfile.CredentialsSecretName; secretName != "" {
		volumes = append(volumes, secretVolume("llm-credentials", secretName))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "llm-credentials", MountPath: "/etc/apifrontend/llm-credentials", ReadOnly: true,
		})
		env = append(env,
			corev1.EnvVar{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/etc/apifrontend/llm-credentials/credentials.json"},
		)
	}

	// #234: severityTriage.llmProfileRef resolves independently of AF's own
	// profile (resolveLLMKey() in pkg/apifrontend/config/config.go), so
	// when its resolved profile names a different credentialsSecretName
	// than AF's own, mount a dedicated Secret rather than reusing AF's
	// "llm-credentials" volume. #279: this used to also redirect the
	// process-wide GOOGLE_APPLICATION_CREDENTIALS env var when triage's
	// profile was vertex_ai (kubernaut#1731's ambient-ADC gap); now that
	// AF's Vertex AI client honors a profile's own APIKey/APIKeyFile, the
	// ConfigMap's apiKeyFile (afSeverityTriageConfig, configmaps.go) reads
	// straight from this mount and the redirect is no longer needed.
	if st := kn.Spec.APIFrontend.SeverityTriage; st != nil && st.LLMProfileRef != "" {
		if stProfile, ok := ResolveLLMProfile(kn, st.LLMProfileRef); ok &&
			stProfile.CredentialsSecretName != "" && stProfile.CredentialsSecretName != afProfile.CredentialsSecretName {
			volumes = append(volumes, secretVolume(severityTriageCredentialsVolumeName(), stProfile.CredentialsSecretName))
			mounts = append(mounts, corev1.VolumeMount{
				Name: severityTriageCredentialsVolumeName(), MountPath: severityTriageCredentialsMountPath(), ReadOnly: true,
			})
		}
	}

	if kn.Spec.Valkey.SecretName != "" {
		volumes = append(volumes, secretVolume("valkey-secrets", kn.Spec.Valkey.SecretName))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "valkey-secrets", MountPath: "/etc/apifrontend/valkey", ReadOnly: true,
		})
	}

	if afProfile.TLSClientSecretRef != "" {
		volumes = append(volumes, secretVolume("llm-tls-client", afProfile.TLSClientSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "llm-tls-client", MountPath: "/etc/apifrontend/llm-tls-client", ReadOnly: true,
		})
	}

	// Fixes a pre-existing crash-loop bug: afAgentLLMConfig() has always
	// rendered agent.llm.oauth2.credentialsDir into AF's ConfigMap whenever
	// OAuth2 is enabled, and upstream AF hard-fails at startup if that
	// directory's client-id/client-secret files are missing, but no volume
	// was ever mounted there. Mirrors KA's existing oauth2-credentials
	// volume pattern.
	if afProfile.OAuth2.Enabled {
		volumes = append(volumes, secretVolume("oauth2-credentials", afProfile.OAuth2.CredentialsSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name: "oauth2-credentials", MountPath: "/etc/apifrontend/oauth2", ReadOnly: true,
		})
	}

	return volumes, mounts, env
}

// apifrontendPorts resolves the listen/metrics/health ports, applying the
// sidecar's shifted defaults (when the kagenti sidecar occupies AF's normal
// ports) and then any administrator-configured overrides.
func apifrontendPorts(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) (listenPort, metricsPort, healthPort int32) {
	listenPort = sidecar.AFListenPort()
	metricsPort = PortMetrics
	healthPort = PortHealthProbe
	if sidecar.ShiftsPorts() {
		metricsPort = 9092
		healthPort = 8082
	}
	if kn.Spec.APIFrontend.MetricsPort != nil {
		metricsPort = *kn.Spec.APIFrontend.MetricsPort
	}
	if kn.Spec.APIFrontend.HealthPort != nil {
		healthPort = *kn.Spec.APIFrontend.HealthPort
	}
	return listenPort, metricsPort, healthPort
}

func APIFrontendDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar KagentiSidecarMode) (*appsv1.Deployment, error) {
	volumes, mounts, env := apifrontendVolumesMountsEnv(kn, sidecar)
	volumes, mounts = appendFleetSecretMounts(volumes, mounts, knV2, "/etc/apifrontend", effectiveFleetOAuth2SecretRef(knV2.Spec.APIFrontend.Fleet, ""))

	initContainers, err := afInitContainers(kn)
	if err != nil {
		return nil, err
	}

	drainSec := int64(15)
	if kn.Spec.APIFrontend.Shutdown.DrainSeconds != nil {
		drainSec = int64(*kn.Spec.APIFrontend.Shutdown.DrainSeconds)
	}
	gracePeriod := drainSec + 5

	listenPort, metricsPort, healthPort := apifrontendPorts(kn, sidecar)

	dep, err := buildDeployment(kn, DeploymentParams{
		Component: ComponentAPIFrontend, ImageName: "apifrontend",
		Resources: kn.Spec.APIFrontend.Resources, VolumeMounts: mounts, Volumes: volumes,
		InitContainers: initContainers,
		Env:            env,
		Args:           []string{"--config=/etc/apifrontend/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "https", ContainerPort: listenPort, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: healthPort, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: metricsPort, Protocol: corev1.ProtocolTCP},
		},
		ProbePort: healthPort,
		PodAnnotations: map[string]string{
			"prometheus.io/scrape": "true",
			"prometheus.io/port":   fmt.Sprintf("%d", metricsPort),
			"prometheus.io/path":   "/metrics",
		},
		TerminationGracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		return nil, err
	}

	dep.Labels[KagentiAgentTypeLabel] = "agent"
	dep.Labels[KagentiA2AProtocolLabel] = ""
	dep.Spec.Template.Labels[KagentiAgentTypeLabel] = "agent"
	dep.Spec.Template.Labels[KagentiA2AProtocolLabel] = ""

	return dep, nil
}

// --- internal helpers ---

// DeploymentParams collects the parameters for building a workload Deployment,
// replacing what was previously a 9-argument function signature.
type DeploymentParams struct {
	Component                     string
	ImageName                     string
	Resources                     corev1.ResourceRequirements
	VolumeMounts                  []corev1.VolumeMount
	Volumes                       []corev1.Volume
	InitContainers                []corev1.Container
	Ports                         []corev1.ContainerPort
	Env                           []corev1.EnvVar
	Args                          []string
	ProbePort                     int32
	Strategy                      *appsv1.DeploymentStrategy
	LivenessPath                  string
	ReadinessPath                 string
	PodAnnotations                map[string]string
	TerminationGracePeriodSeconds *int64
}

// buildDeploymentProbes derives the liveness/readiness/startup probe port
// (from ProbePort, defaulting to the first container port) and builds the
// probes from the component's default probe config, overridden by any
// LivenessPath/ReadinessPath set in p. startup is nil for components with
// no StartupPath configured (see ProbeConfig).
func buildDeploymentProbes(p DeploymentParams, ports []corev1.ContainerPort) (liveness, readiness, startup *corev1.Probe) {
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

	liveness = &corev1.Probe{
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
	readiness = &corev1.Probe{
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
	if pc.StartupPath != "" {
		startup = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: pc.StartupPath,
					Port: intstr.FromInt32(probePort),
				},
			},
			InitialDelaySeconds: pc.StartupInitialDelay,
			PeriodSeconds:       pc.StartupPeriod,
			TimeoutSeconds:      pc.StartupTimeout,
			FailureThreshold:    pc.StartupFailureThreshold,
		}
	}
	return liveness, readiness, startup
}

func buildDeployment(kn *kubernautv1alpha1.Kubernaut, p DeploymentParams) (*appsv1.Deployment, error) {
	img, err := ResolveImage(kn, p.ImageName)
	if err != nil {
		return nil, err
	}

	ports := p.Ports
	if len(ports) == 0 {
		ports = []corev1.ContainerPort{{Name: "https", ContainerPort: PortHTTPS, Protocol: corev1.ProtocolTCP}}
	}
	liveness, readiness, startup := buildDeploymentProbes(p, ports)

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
						Env:             append(p.Env, corev1.EnvVar{Name: "GODEBUG", Value: "netdns=go"}),
						Args:            p.Args,
						Resources:       MergeResources(p.Resources),
						SecurityContext: ContainerSecurityContext(),
						VolumeMounts:    p.VolumeMounts,
						LivenessProbe:   liveness,
						ReadinessProbe:  readiness,
						StartupProbe:    startup,
					}},
					Volumes:                       p.Volumes,
					TerminationGracePeriodSeconds: p.TerminationGracePeriodSeconds,
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

// appendInterServiceTLSCA mounts the inter-service trust bundle and points
// TLS_CA_FILE/SSL_CERT_FILE at it. SSL_CERT_FILE is only appended when the
// caller hasn't already declared one (#404) -- Go's SSL_CERT_FILE replaces
// rather than extends the system trust store, so a caller that already set
// it to its own merged (system+inter-service) bundle -- e.g. KubernautAgent,
// which also verifies public-CA LLM providers via this same global var --
// must keep its own, broader value rather than have it silently shadowed by
// this narrower, inter-service-only one. TLS_CA_FILE has no such conflict
// (no caller pre-sets it) so it keeps unconditional-append semantics.
func appendInterServiceTLSCA(volumes []corev1.Volume, mounts []corev1.VolumeMount, env []corev1.EnvVar) ([]corev1.Volume, []corev1.VolumeMount, []corev1.EnvVar) {
	volumes = append(volumes, configMapVolume("tls-ca", TrustBundleConfigMapName))
	mounts = append(mounts, corev1.VolumeMount{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true})
	env = append(env, corev1.EnvVar{Name: "TLS_CA_FILE", Value: InterServiceTLSCAFile})
	env = appendEnvIfAbsent(env, "SSL_CERT_FILE", InterServiceTLSCAFile)
	return volumes, mounts, env
}

// appendEnvIfAbsent appends name=value only when name isn't already present
// in env, preserving whatever value an earlier caller set (#404) -- the
// inverse of setOrAppendEnv, which always overwrites.
func appendEnvIfAbsent(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

// appendFleetSecretMounts adds the fleet-ca / fleet-token / fleet-oauth2
// volume mounts shared by Gateway and RemediationOrchestrator when
// spec.fleet.enabled is true and the corresponding BYO secret name is set.
// Mount paths must stay in sync with fleetCAMountPath / fleetTokenMountPath
// in configmaps.go. componentEtcDir is the component's own config directory
// (e.g. "/etc/gateway") — upstream GW/RO each read OAuth2 client-id/secret
// files from "<componentEtcDir>/<credentialsSecretRef>/{client-id,client-secret}",
// so the mount path must be built per-component, not shared.
// appendFleetSecretMounts mounts the Secrets backing spec.fleet's TLS CA,
// ACM bearer token, and OAuth2 client credentials. credentialsSecretRefOverride,
// when non-empty, overrides spec.fleet.oauth2.credentialsSecretRef for this
// component only — see resolveFleetConfig for why (per-service OAuth2 client
// registrations against a shared token endpoint). Used by GW/RO and AF (#464
// added AF -- kubernaut#2025/#2022 gave AF's own checkRRScope path a
// Backend/Endpoint scope-check adapter call), the components that read the
// Backend/Endpoint scope-check adapter.
func appendFleetSecretMounts(volumes []corev1.Volume, mounts []corev1.VolumeMount, knV2 *kubernautv1alpha2.Kubernaut, componentEtcDir, credentialsSecretRefOverride string) ([]corev1.Volume, []corev1.VolumeMount) {
	return appendFleetSecretMountsVariant(volumes, mounts, knV2, componentEtcDir, credentialsSecretRefOverride, true)
}

// appendMCPGatewayOnlyFleetSecretMount mounts only the fleet-oauth2
// credentials Secret, never fleet-ca/fleet-token (#224). Used by
// SP/EM/KA, which only ever consume MCP Gateway remote reads and never
// read the Backend/Endpoint scope-check adapter -- their upstream config
// schemas have no field for the Backend/Endpoint TLS CA or ACM bearer
// token at all (see resolveMCPGatewayOnlyFleetConfig /
// resolveSignalProcessingFleetConfig / resolveKAFleetConfig), so mounting
// those Secrets would be dead weight. AF used to be in this list too, but
// #464 moved it to appendFleetSecretMounts above (kubernaut#2025/#2022).
func appendMCPGatewayOnlyFleetSecretMount(volumes []corev1.Volume, mounts []corev1.VolumeMount, knV2 *kubernautv1alpha2.Kubernaut, componentEtcDir, credentialsSecretRefOverride string) ([]corev1.Volume, []corev1.VolumeMount) {
	return appendFleetSecretMountsVariant(volumes, mounts, knV2, componentEtcDir, credentialsSecretRefOverride, false)
}

// appendWorkflowExecutionFleetSecretMount mounts WE's own write-scoped
// fleet-oauth2 credentials Secret (#235, DD-235). Unlike
// appendMCPGatewayOnlyFleetSecretMount (SP/AF/EM/KA), this never falls back
// to the shared spec.fleet.oauth2.credentialsSecretRef: WE's write-scoped
// client must always be independently configured (least-privilege).
// validateFleetOAuth2 rejects fleet+oauth2 enablement without WE's own
// secretRef before this is ever reached with an empty ref, so the no-mount
// branch below is defense-in-depth, not the primary enforcement.
func appendWorkflowExecutionFleetSecretMount(volumes []corev1.Volume, mounts []corev1.VolumeMount, knV2 *kubernautv1alpha2.Kubernaut, componentEtcDir string) ([]corev1.Volume, []corev1.VolumeMount) {
	fleet := &knV2.Spec.Fleet
	if fleet.Enabled == nil || !*fleet.Enabled || !fleet.OAuth2.Enabled {
		return volumes, mounts
	}
	credentialsSecretRef := knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef
	if credentialsSecretRef == "" {
		return volumes, mounts
	}
	volumes = append(volumes, secretVolume("fleet-oauth2", credentialsSecretRef))
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "fleet-oauth2",
		MountPath: componentEtcDir + "/" + credentialsSecretRef,
		ReadOnly:  true,
	})
	return volumes, mounts
}

func appendFleetSecretMountsVariant(volumes []corev1.Volume, mounts []corev1.VolumeMount, knV2 *kubernautv1alpha2.Kubernaut, componentEtcDir, credentialsSecretRefOverride string, includeBackend bool) ([]corev1.Volume, []corev1.VolumeMount) {
	fleet := &knV2.Spec.Fleet
	if fleet.Enabled == nil || !*fleet.Enabled {
		return volumes, mounts
	}
	if includeBackend {
		if fleet.CASecretName != "" {
			volumes = append(volumes, secretVolume("fleet-ca", fleet.CASecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "fleet-ca", MountPath: "/etc/fleet-tls/ca", ReadOnly: true})
		}
		if fleet.TokenSecretName != "" {
			volumes = append(volumes, secretVolume("fleet-token", fleet.TokenSecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "fleet-token", MountPath: "/etc/fleet-token", ReadOnly: true})
		}
	}
	if credentialsSecretRef := withDefault(credentialsSecretRefOverride, fleet.OAuth2.CredentialsSecretRef); fleet.OAuth2.Enabled && credentialsSecretRef != "" {
		volumes = append(volumes, secretVolume("fleet-oauth2", credentialsSecretRef))
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "fleet-oauth2",
			MountPath: componentEtcDir + "/" + credentialsSecretRef,
			ReadOnly:  true,
		})
	}
	return volumes, mounts
}

// overrideTLSCAFile redirects both TLS_CA_FILE and SSL_CERT_FILE (kept in
// sync, same as appendInterServiceTLSCA) to path -- used when a component
// combines the inter-service CA with an additional CA (e.g. WorkflowExecution's
// AAP CA) into a single file via an init container.
func overrideTLSCAFile(env []corev1.EnvVar, path string) []corev1.EnvVar {
	env = setOrAppendEnv(env, "TLS_CA_FILE", path)
	return setOrAppendEnv(env, "SSL_CERT_FILE", path)
}

func setOrAppendEnv(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			env[i].Value = value
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

// ProbeConfig holds per-component HTTP GET probe paths and timing, mirroring
// the upstream kubernaut service defaults so that cold-start and
// resource-constrained nodes don't see premature restarts.
//
// StartupPath is only set for the fleet-aware components (Gateway,
// APIFrontend, EffectivenessMonitor, FleetMetadataCache,
// SignalProcessing, RemediationOrchestrator, WorkflowExecution) --
// see fleetAwareStartupProbe (#267, DD-PLATFORM-008). When empty, no
// StartupProbe is set: these components build a registry.ClusterRegistry
// against the fleet MCP Gateway synchronously at boot, which can
// legitimately take minutes under node CPU contention, far exceeding the
// existing liveness-probe grace period.
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
	StartupPath               string
	StartupInitialDelay       int32
	StartupPeriod             int32
	StartupTimeout            int32
	StartupFailureThreshold   int32
}

// fleetAwareStartupProbe returns the DD-PLATFORM-008 startupProbe
// thresholds (5s initial delay + 60x5s period = 305s cold-start grace),
// mirroring the chart's kubernaut.startupProbe named template exactly --
// tuned against live evidence of cgroup v2 CPU throttling during
// multi-pod fleet-aware cold starts (#267).
func fleetAwareStartupProbe() (path string, initialDelay, period, timeout, failureThreshold int32) {
	return "/healthz", 5, 5, 5, 60
}

// probeConfigForComponent returns the probe configuration for a given
// component, aligned with upstream kubernaut v1.4.0 defaults.
// Gateway, DataStorage, and KubernautAgent probes use the dedicated health
// port (8081) with /healthz and /readyz paths.
func probeConfigForComponent(component string) ProbeConfig {
	switch component {
	case ComponentGateway:
		pc := ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 10, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 30, ReadinessPeriod: 5, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
		pc.StartupPath, pc.StartupInitialDelay, pc.StartupPeriod, pc.StartupTimeout, pc.StartupFailureThreshold = fleetAwareStartupProbe()
		return pc
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
		pc := ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 10, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 5, ReadinessPeriod: 5, ReadinessTimeout: 5, ReadinessFailureThreshold: 3,
		}
		pc.StartupPath, pc.StartupInitialDelay, pc.StartupPeriod, pc.StartupTimeout, pc.StartupFailureThreshold = fleetAwareStartupProbe()
		return pc
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
	case ComponentAPIFrontend:
		pc := ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 5, ReadinessPeriod: 5, ReadinessTimeout: 3, ReadinessFailureThreshold: 3,
		}
		pc.StartupPath, pc.StartupInitialDelay, pc.StartupPeriod, pc.StartupTimeout, pc.StartupFailureThreshold = fleetAwareStartupProbe()
		return pc
	case ComponentFleetMetadataCache:
		pc := ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 5, LivenessPeriod: 10, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 3, ReadinessPeriod: 5, ReadinessTimeout: 3, ReadinessFailureThreshold: 3,
		}
		pc.StartupPath, pc.StartupInitialDelay, pc.StartupPeriod, pc.StartupTimeout, pc.StartupFailureThreshold = fleetAwareStartupProbe()
		return pc
	default:
		// SignalProcessing, RemediationOrchestrator, WorkflowExecution
		pc := ProbeConfig{
			LivenessPath: "/healthz", LivenessInitialDelay: 15, LivenessPeriod: 20, LivenessTimeout: 5, LivenessFailureThreshold: 3,
			ReadinessPath: "/readyz", ReadinessInitialDelay: 5, ReadinessPeriod: 10, ReadinessTimeout: 5, ReadinessFailureThreshold: 6,
		}
		pc.StartupPath, pc.StartupInitialDelay, pc.StartupPeriod, pc.StartupTimeout, pc.StartupFailureThreshold = fleetAwareStartupProbe()
		return pc
	}
}
