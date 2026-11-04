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

package kind

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// The realm is intentionally based on the upstream Kubernaut Keycloak E2E
// fixture: the Console and API Frontend clients, sre-user/password, /sre
// membership, groups mapper, and API/Console audience mapper are retained.
// This lane uses upstream Keycloak itself, not Dex or a test OIDC stub.
//
//go:embed keycloak-realm-demo.json
var qualificationKeycloakRealm string

const (
	qualificationKeycloakRealmName = "kubernaut-demo"
	qualificationKeycloakTLSSecret = "keycloak-tls" //nolint:gosec // Kubernetes object name, not secret material
	qualificationKeycloakRealmMap  = "keycloak-realm-config"
)

func ensureQualificationKeycloak(ctx context.Context) error {
	if !qualificationLane {
		return nil
	}
	fixture, err := activeManualTLSFixture()
	if err != nil {
		return fmt.Errorf("loading the active TLS fixture for Keycloak: %w", err)
	}
	if fixture.caCertificate == nil || fixture.caKey == nil || len(fixture.caPEM) == 0 {
		return fmt.Errorf("active TLS fixture does not contain a reusable CA")
	}
	if err := ensureNamespace(ctx, qualificationKeycloakNamespace); err != nil {
		return fmt.Errorf("creating Keycloak namespace: %w", err)
	}
	keycloakTLS, err := newQualificationKeycloakTLSSecret(fixture)
	if err != nil {
		return fmt.Errorf("creating Keycloak TLS fixture: %w", err)
	}
	if err := applyYAML(ctx,
		keycloakTLS,
		qualificationKeycloakRealmConfigMap(),
		qualificationKeycloakDeployment(),
		qualificationKeycloakServiceObject(),
		qualificationKeycloakAliasService(),
		qualificationConsoleOIDCSecret(),
	); err != nil {
		return fmt.Errorf("applying Keycloak qualification fixtures: %w", err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/keycloak", "-n", qualificationKeycloakNamespace, "--timeout=5m",
	); err != nil {
		return fmt.Errorf("waiting for Keycloak rollout: %w", err)
	}
	if err := pollUntilSuccess(ctx, 3*time.Minute, 2*time.Second, func() error {
		client := qualificationHTTPClient(fixture.caPEM, nil)
		request, requestErr := http.NewRequestWithContext(
			ctx, http.MethodGet, "https://keycloak:8443/realms/"+qualificationKeycloakRealmName, nil,
		)
		if requestErr != nil {
			return requestErr
		}
		resp, getErr := client.Do(request)
		if getErr != nil {
			return getErr
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("keycloak realm endpoint returned HTTP %d", resp.StatusCode)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("waiting for the imported Keycloak realm: %w", err)
	}
	return nil
}

func newQualificationKeycloakTLSSecret(fixture manualTLSFixture) (*corev1.Secret, error) {
	certificate, key, err := generateManualLeaf(
		time.Now().UTC(), fixture.caCertificate, fixture.caKey,
		qualificationKeycloakService, qualificationKeycloakNamespace, "keycloak",
	)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      qualificationKeycloakTLSSecret,
			Namespace: qualificationKeycloakNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certificate,
			corev1.TLSPrivateKeyKey: key,
			"ca.crt":                append([]byte(nil), fixture.caPEM...),
		},
	}, nil
}

func qualificationKeycloakRealmConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      qualificationKeycloakRealmMap,
			Namespace: qualificationKeycloakNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Data: map[string]string{
			"kubernaut-demo-realm.json": qualificationKeycloakRealm,
		},
	}
}

func qualificationKeycloakDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      qualificationKeycloakService,
			Namespace: qualificationKeycloakNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": qualificationKeycloakService}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"app":                        qualificationKeycloakService,
					"kubernaut.ai/qualification": "issue-609",
				}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{qualificationKeycloakContainer()},
					Volumes: []corev1.Volume{
						{
							Name: "realm-config",
							VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: qualificationKeycloakRealmMap},
							}},
						},
						{
							Name: "tls-certs",
							VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
								SecretName: qualificationKeycloakTLSSecret,
							}},
						},
					},
				},
			},
		},
	}
}

func qualificationKeycloakContainer() corev1.Container {
	return corev1.Container{
		Name:  qualificationKeycloakService,
		Image: qualificationKeycloakImage(),
		Args:  []string{"start-dev", "--import-realm"},
		Env: []corev1.EnvVar{
			{Name: "KC_BOOTSTRAP_ADMIN_USERNAME", Value: "admin"},
			{Name: "KC_BOOTSTRAP_ADMIN_PASSWORD", Value: "admin"}, //nolint:gosec // disposable qualification fixture
			{Name: "KC_HTTPS_CERTIFICATE_FILE", Value: "/etc/keycloak-tls/tls.crt"},
			{Name: "KC_HTTPS_CERTIFICATE_KEY_FILE", Value: "/etc/keycloak-tls/tls.key"},
			{Name: "KC_HOSTNAME", Value: "https://keycloak:8443"},
			{Name: "KC_HOSTNAME_STRICT_HTTPS", Value: "false"},
		},
		Ports: []corev1.ContainerPort{{Name: "https", ContainerPort: 8443, Protocol: corev1.ProtocolTCP}},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "realm-config", MountPath: "/opt/keycloak/data/import", ReadOnly: true},
			{Name: "tls-certs", MountPath: "/etc/keycloak-tls", ReadOnly: true},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: "/realms/master", Port: intstr.FromInt32(8443), Scheme: corev1.URISchemeHTTPS,
			}},
			InitialDelaySeconds: 15,
			PeriodSeconds:       5,
			FailureThreshold:    24,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: "/realms/master", Port: intstr.FromInt32(8443), Scheme: corev1.URISchemeHTTPS,
			}},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    12,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resourceMustParse("250m"),
				corev1.ResourceMemory: resourceMustParse("768Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resourceMustParse("1000m"),
				corev1.ResourceMemory: resourceMustParse("1536Mi"),
			},
		},
	}
}

func qualificationKeycloakServiceObject() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      qualificationKeycloakService,
			Namespace: qualificationKeycloakNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: map[string]string{"app": qualificationKeycloakService},
			Ports: []corev1.ServicePort{{
				Name: "https", Port: 8443, TargetPort: intstr.FromInt32(8443), NodePort: qualificationKeycloakNodePort,
			}},
		},
	}
}

func qualificationKeycloakAliasService() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      qualificationKeycloakService,
			Namespace: kubernautNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: qualificationKeycloakService + "." + qualificationKeycloakNamespace + ".svc.cluster.local",
		},
	}
}

func qualificationConsoleOIDCSecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "console-oidc-creds",
			Namespace: kubernautNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "issue-609-qualification",
				"kubernaut.ai/qualification":   "issue-609",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"client-id":     qualificationConsoleClient,
			"client-secret": qualificationConsoleSecret,
			"cookie-secret": qualificationConsoleCookie,
		},
	}
}

// resourceMustParse keeps this fixture's resource literals local and avoids
// silently accepting invalid values in a manifest builder.
func resourceMustParse(value string) resource.Quantity {
	return resource.MustParse(value)
}
