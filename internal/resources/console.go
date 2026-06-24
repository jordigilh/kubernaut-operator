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

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const (
	consoleProxyPort  = int32(4180)
	consoleStaticPort = int32(8080)
)

// ConsoleDeployment builds the Deployment for the standalone web console.
// ingressDomain is the cluster's ingress domain (e.g. "apps.dev.example.com")
// used to derive the oauth2-proxy redirect URL when console.route.host is empty.
func ConsoleDeployment(kn *kubernautv1alpha1.Kubernaut, ingressDomain string) (*appsv1.Deployment, error) {
	consoleImage, err := ResolveImage(kn, "console")
	if err != nil {
		return nil, err
	}
	oauth2ProxyImage, err := ResolveImage(kn, "oauth2-proxy")
	if err != nil {
		return nil, err
	}

	issuerURL := kn.Spec.ConsoleIssuerURL()
	if issuerURL == "" {
		return nil, fmt.Errorf("console requires apiFrontend.auth.issuerURL or apiFrontend.auth.jwtProviders to be configured")
	}

	secretName := kn.Spec.Console.Auth.SecretName
	if secretName == "" {
		return nil, fmt.Errorf("console.auth.secretName is required")
	}

	redirectURL := consoleRedirectURL(kn, ingressDomain)

	oauthArgs := []string{
		"--provider=oidc",
		fmt.Sprintf("--oidc-issuer-url=%s", issuerURL),
		fmt.Sprintf("--redirect-url=%s", redirectURL),
		"--http-address=0.0.0.0:4180",
		"--upstream=http://localhost:8080",
		"--pass-access-token=true",
		"--skip-provider-button=true",
		"--cookie-name=_kubernaut_console",
		"--cookie-secure=true",
		"--cookie-samesite=lax",
		"--email-domain=*",
		"--scope=openid email profile",
		"--skip-jwt-bearer-tokens=true",
		"--cookie-refresh=25m",
		"--ssl-insecure-skip-verify=true",
	}

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: ObjectMeta(kn, ComponentConsole, ComponentConsole),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(ComponentConsole),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ComponentLabels(kn, ComponentConsole),
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "oauth2-proxy",
							Image: oauth2ProxyImage,
							Args:  oauthArgs,
							Env: []corev1.EnvVar{
								{Name: "OAUTH2_PROXY_CLIENT_ID", ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
										Key:                  "client-id",
									},
								}},
								{Name: "OAUTH2_PROXY_CLIENT_SECRET", ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
										Key:                  "client-secret",
									},
								}},
								{Name: "OAUTH2_PROXY_COOKIE_SECRET", ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
										Key:                  "cookie-secret",
									},
								}},
							},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: consoleProxyPort, Protocol: corev1.ProtocolTCP},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
								ReadOnlyRootFilesystem:   ptr.To(true),
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
									Path: "/oauth2/ping", Port: intstr.FromInt32(consoleProxyPort),
								}},
								InitialDelaySeconds: 3, PeriodSeconds: 5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
									Path: "/oauth2/ping", Port: intstr.FromInt32(consoleProxyPort),
								}},
								InitialDelaySeconds: 10, PeriodSeconds: 15,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("25m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Name:            "console",
							Image:           consoleImage,
							ImagePullPolicy: kn.Spec.Image.PullPolicy,
							Ports: []corev1.ContainerPort{
								{Name: "static", ContainerPort: consoleStaticPort, Protocol: corev1.ProtocolTCP},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
									Path: "/healthz", Port: intstr.FromInt32(consoleStaticPort),
								}},
								InitialDelaySeconds: 2, PeriodSeconds: 5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
									Path: "/healthz", Port: intstr.FromInt32(consoleStaticPort),
								}},
								InitialDelaySeconds: 5, PeriodSeconds: 10,
							},
							Resources: consoleContainerResources(kn),
							VolumeMounts: []corev1.VolumeMount{
								{Name: "nginx-tmp", MountPath: "/tmp"},
								{Name: "nginx-config", MountPath: "/opt/app-root/etc/nginx.d/kubernaut-http.conf", SubPath: "http.conf", ReadOnly: true},
								{Name: "nginx-config", MountPath: "/opt/app-root/etc/nginx.default.d/kubernaut-server.conf", SubPath: "server.conf", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "nginx-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "nginx-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ComponentConsole + "-nginx"},
						}}},
					},
				},
			},
		},
	}

	return dep, nil
}

// ConsoleService builds the Service for the console.
func ConsoleService(kn *kubernautv1alpha1.Kubernaut) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: ObjectMeta(kn, ComponentConsole, ComponentConsole),
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(ComponentConsole),
			Ports: []corev1.ServicePort{
				{Name: "http", Port: consoleProxyPort, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// ConsoleNginxConfigMap builds the nginx configuration ConfigMap for the console.
func ConsoleNginxConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	afURL := fmt.Sprintf("http://%s.%s.svc:%d", ComponentAPIFrontend, kn.Namespace, PortHTTPS)

	httpConf := `limit_req_zone $binary_remote_addr zone=api:10m rate=30r/s;
limit_req_zone $binary_remote_addr zone=mcp:10m rate=10r/s;

gzip on;
gzip_types text/plain text/css application/json application/javascript text/xml application/xml text/javascript image/svg+xml;
gzip_min_length 256;
gzip_vary on;
`

	serverConf := fmt.Sprintf(`add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; img-src 'self' data:; connect-src 'self'; font-src 'self' https://fonts.gstatic.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-Frame-Options "DENY" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

location /a2a/ {
  limit_req zone=api burst=50 nodelay;
  proxy_pass %s;
  proxy_http_version 1.1;
  proxy_set_header Connection "";
  proxy_set_header Host $host;
  proxy_set_header X-Real-IP $remote_addr;
  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
  proxy_set_header Authorization "Bearer $http_x_forwarded_access_token";
  proxy_buffering off;
  proxy_cache off;
  proxy_read_timeout 3600s;
  proxy_send_timeout 3600s;
  send_timeout 3600s;
  keepalive_timeout 3600s;
}

location = /mcp {
  limit_req zone=mcp burst=20 nodelay;
  proxy_pass %s;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Real-IP $remote_addr;
  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
  proxy_set_header Authorization "Bearer $http_x_forwarded_access_token";
  proxy_set_header Mcp-Session-Id $http_mcp_session_id;
  proxy_read_timeout 30s;
  proxy_hide_header Access-Control-Expose-Headers;
  add_header Access-Control-Expose-Headers "Mcp-Session-Id" always;
}

location /.well-known/ {
  proxy_pass %s;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
}

location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff2?|ttf|eot)$ {
  expires 1y;
  add_header Cache-Control "public, immutable";
  try_files $uri =404;
}

location / {
  add_header Cache-Control "no-cache, must-revalidate";
  try_files $uri $uri/ /index.html;
}
`, afURL, afURL, afURL)

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, ComponentConsole+"-nginx", ComponentConsole),
		Data: map[string]string{
			"http.conf":   httpConf,
			"server.conf": serverConf,
		},
	}
}

// ConsoleRoute builds the OCP Route for external access to the console.
func ConsoleRoute(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	if kn.Spec.Console.Route.Enabled != nil && !*kn.Spec.Console.Route.Enabled {
		return nil
	}

	route := &routev1.Route{
		ObjectMeta: ObjectMeta(kn, ComponentConsole, ComponentConsole),
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   ComponentConsole,
				Weight: ptr.To(int32(100)),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}

	if kn.Spec.Console.Route.Host != "" {
		route.Spec.Host = kn.Spec.Console.Route.Host
	}

	return route
}

// ConsoleRouteStub returns a minimal Route for deletion lookups.
func ConsoleRouteStub(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ComponentConsole,
			Namespace: kn.Namespace,
		},
	}
}

func consoleRedirectURL(kn *kubernautv1alpha1.Kubernaut, ingressDomain string) string {
	if kn.Spec.Console.Route.Host != "" {
		return fmt.Sprintf("https://%s/oauth2/callback", kn.Spec.Console.Route.Host)
	}
	if ingressDomain == "" {
		ingressDomain = "apps.cluster.local"
	}
	return fmt.Sprintf("https://%s-%s.%s/oauth2/callback",
		ComponentConsole, kn.Namespace, ingressDomain)
}

func consoleContainerResources(kn *kubernautv1alpha1.Kubernaut) corev1.ResourceRequirements {
	if len(kn.Spec.Console.Resources.Requests) > 0 || len(kn.Spec.Console.Resources.Limits) > 0 {
		return kn.Spec.Console.Resources
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("16Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
}
