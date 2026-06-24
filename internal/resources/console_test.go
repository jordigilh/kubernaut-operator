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
	"os"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

func testKubernautWithConsole() *kubernautv1alpha1.Kubernaut {
	kn := testKubernaut()
	kn.Spec.Console = kubernautv1alpha1.ConsoleSpec{
		Enabled: ptr.To(true),
		Auth:    kubernautv1alpha1.ConsoleAuthSpec{SecretName: "console-oidc-creds"},
		Route:   kubernautv1alpha1.ConsoleRouteSpec{Enabled: ptr.To(true)},
	}
	return kn
}

var _ = Describe("Console Resources", func() {

	Context("ConsoleDeployment", func() {

		It("UT-CD-01 [CM-6, CC8.1]: rejects deployment when container image is unresolvable", func() {
			kn := testKubernautWithConsole()
			saved := os.Getenv("RELATED_IMAGE_CONSOLE")
			Expect(os.Unsetenv("RELATED_IMAGE_CONSOLE")).To(Succeed())
			defer func() { Expect(os.Setenv("RELATED_IMAGE_CONSOLE", saved)).To(Succeed()) }()

			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).To(HaveOccurred())
			Expect(dep).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("console"))
		})

		It("UT-CD-02 [IA-5, CC6.1]: rejects deployment when OIDC issuer is empty", func() {
			kn := testKubernautWithConsole()
			kn.Spec.APIFrontend.Auth.IssuerURL = ""
			kn.Spec.APIFrontend.Auth.JWTProviders = nil

			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).To(HaveOccurred())
			Expect(dep).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("issuerURL"))
		})

		It("UT-CD-03 [IA-5, CC6.1]: rejects deployment when auth secret name is missing", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Auth.SecretName = ""

			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).To(HaveOccurred())
			Expect(dep).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("secretName"))
		})

		It("UT-CD-04 [SC-7, CC6.6]: produces hardened pod spec with oauth2-proxy sidecar", func() {
			kn := testKubernautWithConsole()

			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).NotTo(HaveOccurred())
			Expect(dep).NotTo(BeNil())

			Expect(dep.Name).To(Equal(ComponentConsole))
			Expect(dep.Namespace).To(Equal(testSystemNamespace))
			Expect(dep.Spec.Replicas).To(Equal(ptr.To(int32(1))))

			spec := dep.Spec.Template.Spec

			Expect(spec.SecurityContext).NotTo(BeNil())
			Expect(*spec.SecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))

			Expect(spec.Containers).To(HaveLen(2))

			oauth2 := spec.Containers[0]
			Expect(oauth2.Name).To(Equal("oauth2-proxy"))
			Expect(oauth2.Image).To(Equal("quay.io/oauth2-proxy/oauth2-proxy:v7.9.0"))
			Expect(*oauth2.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*oauth2.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(oauth2.SecurityContext.RunAsUser).To(BeNil(), "hardcoded RunAsUser breaks OCP restricted SCC")
			Expect(oauth2.Ports).To(HaveLen(1))
			Expect(oauth2.Ports[0].ContainerPort).To(Equal(consoleProxyPort))
			Expect(oauth2.Env).To(HaveLen(3))
			for _, env := range oauth2.Env {
				Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal("console-oidc-creds"))
			}
			Expect(oauth2.ReadinessProbe).NotTo(BeNil())
			Expect(oauth2.LivenessProbe).NotTo(BeNil())

			console := spec.Containers[1]
			Expect(console.Name).To(Equal("console"))
			Expect(console.Image).To(Equal("quay.io/kubernaut-ai/console:v1.3.0"))
			Expect(console.ImagePullPolicy).To(Equal(corev1.PullIfNotPresent))
			Expect(*console.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(console.SecurityContext.RunAsUser).To(BeNil(), "hardcoded RunAsUser breaks OCP restricted SCC")
			Expect(console.VolumeMounts).To(HaveLen(4))

			Expect(spec.Volumes).To(HaveLen(3))
			Expect(spec.Volumes[0].Name).To(Equal("nginx-tmp"))
			Expect(spec.Volumes[1].Name).To(Equal("nginx-config"))
			Expect(spec.Volumes[1].ConfigMap.Name).To(Equal(ComponentConsole + "-nginx"))
			Expect(spec.Volumes[2].Name).To(Equal("tls-ca"))
		})
	})

	Context("ConsoleService", func() {
		It("UT-CS-01 [SC-7, CC6.6]: exposes only the oauth2-proxy port", func() {
			kn := testKubernautWithConsole()
			svc := ConsoleService(kn)

			Expect(svc.Name).To(Equal(ComponentConsole))
			Expect(svc.Namespace).To(Equal(testSystemNamespace))
			Expect(svc.Spec.Selector).To(Equal(SelectorLabels(ComponentConsole)))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Name).To(Equal("http"))
			Expect(svc.Spec.Ports[0].Port).To(Equal(consoleProxyPort))
			Expect(svc.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		})
	})

	Context("ConsoleNginxConfigMap", func() {
		It("UT-CN-01 [SC-8, CC6.7]: enforces transport security headers", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)

			serverConf := cm.Data["server.conf"]
			Expect(serverConf).To(ContainSubstring("Strict-Transport-Security"))
			Expect(serverConf).To(ContainSubstring("Content-Security-Policy"))
			Expect(serverConf).To(ContainSubstring("X-Frame-Options"))
			Expect(serverConf).To(ContainSubstring("X-Content-Type-Options"))
			Expect(serverConf).To(ContainSubstring("Referrer-Policy"))
			Expect(serverConf).To(ContainSubstring("Permissions-Policy"))
		})

		It("UT-CN-02 [AC-4, CC6.6]: upstream targets namespace-scoped API Frontend", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)

			Expect(cm.Name).To(Equal(ComponentConsole + "-nginx"))
			Expect(cm.Namespace).To(Equal(testSystemNamespace))

			serverConf := cm.Data["server.conf"]
			Expect(serverConf).To(ContainSubstring(ComponentAPIFrontend + "." + testSystemNamespace + ".svc"))
			Expect(serverConf).To(ContainSubstring("/a2a/"))
			Expect(serverConf).To(ContainSubstring("/mcp"))
			Expect(serverConf).To(ContainSubstring("/.well-known/"))
			Expect(serverConf).To(ContainSubstring("try_files $uri $uri/ /index.html"))

			httpConf := cm.Data["http.conf"]
			Expect(httpConf).To(ContainSubstring("limit_req_zone"))
			Expect(httpConf).To(ContainSubstring("gzip on"))
		})
	})

	Context("ConsoleNginxConfigMap TLS (#198)", func() {
		It("UT-CN-198-001 [SC-8]: proxy_pass uses https:// scheme for AF upstream", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)
			serverConf := cm.Data["server.conf"]
			Expect(serverConf).To(ContainSubstring("proxy_pass https://"),
				"proxy_pass must use https:// to connect to AF over TLS")
		})

		It("UT-CN-198-002 [SC-8]: includes proxy_ssl_trusted_certificate directive", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)
			serverConf := cm.Data["server.conf"]
			Expect(serverConf).To(ContainSubstring("proxy_ssl_trusted_certificate"),
				"nginx must trust the service-ca bundle for upstream TLS verification")
		})

		It("UT-CN-198-003 [SC-8]: includes proxy_ssl_verify on", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)
			serverConf := cm.Data["server.conf"]
			Expect(serverConf).To(ContainSubstring("proxy_ssl_verify on"),
				"nginx must verify the AF upstream certificate")
		})

		It("UT-CN-198-004 [SC-8]: proxy_pass does NOT use http:// (negative)", func() {
			kn := testKubernautWithConsole()
			cm := ConsoleNginxConfigMap(kn)
			serverConf := cm.Data["server.conf"]
			Expect(serverConf).NotTo(ContainSubstring("proxy_pass http://"),
				"plaintext HTTP proxy_pass to AF must not appear")
		})
	})

	Context("ConsoleDeployment TLS (#198)", func() {
		const tlsCAMountPath = "/etc/tls-ca"

		It("UT-CD-198-001 [SC-8]: console container mounts tls-ca volume at /etc/tls-ca", func() {
			kn := testKubernautWithConsole()
			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).NotTo(HaveOccurred())

			console := dep.Spec.Template.Spec.Containers[1]
			Expect(console.Name).To(Equal("console"))

			found := false
			for _, m := range console.VolumeMounts {
				if m.Name == testVolumeTLSCA && m.MountPath == tlsCAMountPath && m.ReadOnly {
					found = true
				}
			}
			Expect(found).To(BeTrue(),
				"console container must mount tls-ca at %s for nginx proxy_ssl_trusted_certificate", tlsCAMountPath)
		})

		It("UT-CD-198-002 [CM-6]: tls-ca volume sources from inter-service-ca ConfigMap", func() {
			kn := testKubernautWithConsole()
			dep, err := ConsoleDeployment(kn, testIngressDomain)
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeTLSCA && v.ConfigMap != nil && v.ConfigMap.Name == InterServiceCAConfigMapName {
					found = true
				}
			}
			Expect(found).To(BeTrue(),
				"tls-ca volume must source from the inter-service-ca ConfigMap")
		})
	})

	Context("ConsoleRoute", func() {
		It("UT-CR-01 [SC-7, CC6.6]: creates route by default with edge TLS", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Enabled = nil

			route := ConsoleRoute(kn)
			Expect(route).NotTo(BeNil())
			Expect(route.Name).To(Equal(ComponentConsole))
			Expect(route.Spec.TLS).NotTo(BeNil())
			Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationEdge))
			Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect))
			Expect(route.Spec.To.Name).To(Equal(ComponentConsole))
			Expect(*route.Spec.To.Weight).To(Equal(int32(100)))
			Expect(route.Spec.Host).To(BeEmpty())
		})

		It("UT-CR-02 [SC-7, CC6.6]: suppressed when explicitly disabled", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Enabled = ptr.To(false)

			route := ConsoleRoute(kn)
			Expect(route).To(BeNil())
		})

		It("UT-CR-03 [AC-4, CC6.1]: custom hostname propagates to route spec", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Host = "console.example.com"

			route := ConsoleRoute(kn)
			Expect(route).NotTo(BeNil())
			Expect(route.Spec.Host).To(Equal("console.example.com"))
		})
	})

	Context("ConsoleRouteStub", func() {
		It("UT-CRS-01 [CM-6]: contains only the lookup key for cleanup", func() {
			kn := testKubernautWithConsole()
			stub := ConsoleRouteStub(kn)

			Expect(stub.Name).To(Equal(ComponentConsole))
			Expect(stub.Namespace).To(Equal(testSystemNamespace))
			Expect(stub.Spec).To(Equal(routev1.RouteSpec{}))
		})
	})

	Context("consoleRedirectURL", func() {
		It("UT-CRU-01 [IA-5, CC6.1]: uses custom route host when configured", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Host = "console.example.com"

			url := consoleRedirectURL(kn, testIngressDomain)
			Expect(url).To(Equal("https://console.example.com/oauth2/callback"))
		})

		It("UT-CRU-02 [IA-5, CC6.1]: uses cluster ingress domain when route host is empty", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Host = ""

			url := consoleRedirectURL(kn, testIngressDomain)
			Expect(url).To(Equal("https://kubernaut-console-kubernaut-system.apps.test.example.com/oauth2/callback"))
		})

		It("UT-CRU-03 [IA-5, CC6.1]: falls back to apps.cluster.local when ingress domain is empty", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Route.Host = ""

			url := consoleRedirectURL(kn, "")
			Expect(url).To(Equal("https://kubernaut-console-kubernaut-system.apps.cluster.local/oauth2/callback"))
		})
	})

	Context("consoleContainerResources", func() {
		It("UT-CCR-01 [SC-5, CC6.6]: applies default resource limits when unset", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Resources = corev1.ResourceRequirements{}

			res := consoleContainerResources(kn)
			Expect(res.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("10m")))
			Expect(res.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("16Mi")))
			Expect(res.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("50m")))
			Expect(res.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("64Mi")))
		})

		It("UT-CCR-02 [CM-6, CC8.1]: operator overrides take precedence", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			}

			res := consoleContainerResources(kn)
			Expect(res.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
			Expect(res.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("128Mi")))
			Expect(res.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
			Expect(res.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
		})
	})
})
