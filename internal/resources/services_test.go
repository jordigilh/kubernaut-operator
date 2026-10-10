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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const testAuthWebhookServiceName = "authwebhook-service"

var _ = Describe("Services", func() {
	It("does not emit OpenShift service-CA annotations for generic administrator-managed TLS", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}

		services := Services(kn, testKnV2(kn))
		for _, service := range services {
			Expect(service.Annotations).NotTo(HaveKey(OCPServingCertAnnotation), service.Name)
		}
	})

	Context("Services()", func() {
		It("returns 6 API services", func() {
			kn := testKubernaut()
			svcs := Services(kn, testKnV2(kn))
			Expect(svcs).To(HaveLen(6))
		})

		It("places all services in the system namespace", func() {
			kn := testKubernaut()
			for _, svc := range Services(kn, testKnV2(kn)) {
				Expect(svc.Namespace).To(Equal(testSystemNamespace), "Service %q namespace = %q, want %q", svc.Name, svc.Namespace, testSystemNamespace)
			}
		})

		It("exposes authwebhook on 443 with serving cert annotation", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == testAuthWebhookServiceName {
					found = true
					Expect(svc.Spec.Ports).NotTo(BeEmpty())
					Expect(svc.Spec.Ports[0].Port).To(Equal(int32(443)))
					Expect(svc.Annotations[OCPServingCertAnnotation]).To(Equal("authwebhook-tls"))
					break
				}
			}
			Expect(found).To(BeTrue(), "Services() should contain authwebhook-service")
		})

		It("gives non-authwebhook API services port 8443", func() {
			kn := testKubernaut()
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == testAuthWebhookServiceName {
					continue
				}
				found := false
				for _, p := range svc.Spec.Ports {
					if p.Port == 8443 {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Service %q should have port 8443", svc.Name)
			}
		})

		It("includes expected service names", func() {
			kn := testKubernaut()
			svcs := Services(kn, testKnV2(kn))
			names := make(map[string]bool, len(svcs))
			for _, svc := range svcs {
				names[svc.Name] = true
			}
			expected := []string{
				"gateway-service",
				"data-storage-service",
				"aianalysis-service",
				"kubernaut-agent",
				testAuthWebhookServiceName,
			}
			for _, name := range expected {
				Expect(names[name]).To(BeTrue(), "Services() missing expected service %q", name)
			}
		})

		It("maps gateway-service to multi-port spec", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == "gateway-service" {
					found = true
					wantPorts := map[string]int32{"https": 8443, "health": 8081, "metrics": 9090}
					gotPorts := make(map[string]int32)
					for _, p := range svc.Spec.Ports {
						gotPorts[p.Name] = p.Port
					}
					for name, port := range wantPorts {
						Expect(gotPorts[name]).To(Equal(port), "gateway-service port %q = %d, want %d", name, gotPorts[name], port)
					}
					break
				}
			}
			Expect(found).To(BeTrue(), "gateway-service not found")
		})

		It("UT-MON-513-003 [CM-8, SI-4]: exposes the DataStorage metrics port used by its ServiceMonitor", func() {
			kn := testKubernaut()
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name != "data-storage-service" {
					continue
				}
				for _, port := range svc.Spec.Ports {
					if port.Name == "metrics" {
						Expect(port.Port).To(Equal(PortMetrics))
						return
					}
				}
				Fail("data-storage-service should expose a named metrics port")
			}
			Fail("data-storage-service not found")
		})

		It("uses configured APIFrontend health and metrics Service ports", func() {
			kn := testKubernautWithAF()
			metricsPort := int32(19090)
			healthPort := int32(19081)
			kn.Spec.APIFrontend.MetricsPort = &metricsPort
			kn.Spec.APIFrontend.HealthPort = &healthPort

			var apifrontend *corev1.Service
			for _, service := range Services(kn, testKnV2(kn)) {
				if service.Name == "apifrontend" {
					apifrontend = service
					break
				}
			}
			Expect(apifrontend).NotTo(BeNil())
			Expect(apifrontend.Spec.Ports).To(ContainElements(
				HaveField("Name", "metrics"),
				HaveField("Name", "health"),
			))
			Expect(apifrontend.Spec.Ports).To(ContainElement(And(
				HaveField("Name", "metrics"),
				HaveField("Port", metricsPort),
			)))
			Expect(apifrontend.Spec.Ports).To(ContainElement(And(
				HaveField("Name", "health"),
				HaveField("Port", healthPort),
			)))
		})

		It("UT-MON-513-004 [CM-8, SI-4]: routes every generated Service port to a declared workload container port", func() {
			kn := testKubernautWithAF()
			services := append(Services(kn, testKnV2(kn)), MetricsServices(kn)...)
			deployments := generatedDeploymentsByComponent(kn)

			for _, service := range services {
				component := service.Spec.Selector["app"]
				deployment, ok := deployments[component]
				Expect(ok).To(BeTrue(), "Service %q selects component %q without a generated Deployment", service.Name, component)

				for _, servicePort := range service.Spec.Ports {
					found := false
					for _, container := range deployment.Spec.Template.Spec.Containers {
						for _, containerPort := range container.Ports {
							if servicePortTargetsContainerPort(servicePort, containerPort) {
								found = true
								break
							}
						}
						if found {
							break
						}
					}
					Expect(found).To(BeTrue(), "Service %q port %q must target a declared port on Deployment %q", service.Name, servicePort.Name, deployment.Name)
				}
			}
		})

		It("annotates kubernaut-agent with serving cert secret name", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == "kubernaut-agent" {
					found = true
					v, ok := svc.Annotations[OCPServingCertAnnotation]
					Expect(ok).To(BeTrue(), "kubernaut-agent missing serving-cert-secret-name annotation")
					Expect(v).To(Equal(KubernautAgentTLSSecretName))
					break
				}
			}
			Expect(found).To(BeTrue(), "kubernaut-agent service not found")
		})

		It("annotates gateway-service with serving cert secret name", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == "gateway-service" {
					found = true
					v, ok := svc.Annotations[OCPServingCertAnnotation]
					Expect(ok).To(BeTrue(), "gateway-service missing serving-cert-secret-name annotation")
					Expect(v).To(Equal(GatewayTLSSecretName))
					break
				}
			}
			Expect(found).To(BeTrue(), "gateway-service not found")
		})

		It("[CM-6] excludes gateway-service when Gateway is disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Gateway.Enabled = &disabled
			for _, svc := range Services(kn, testKnV2(kn)) {
				Expect(svc.Name).NotTo(Equal("gateway-service"),
					"gateway-service should not be present when Gateway is disabled")
			}
		})

		It("annotates data-storage-service with serving cert secret name", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn, testKnV2(kn)) {
				if svc.Name == "data-storage-service" {
					found = true
					v, ok := svc.Annotations[OCPServingCertAnnotation]
					Expect(ok).To(BeTrue(), "data-storage-service missing serving-cert-secret-name annotation")
					Expect(v).To(Equal(DataStorageTLSSecretName))
					break
				}
			}
			Expect(found).To(BeTrue(), "data-storage-service not found")
		})
	})

	Context("MetricsServices()", func() {
		It("returns 5 metrics services", func() {
			kn := testKubernaut()
			svcs := MetricsServices(kn)
			Expect(svcs).To(HaveLen(5))
		})

		It("places all metrics services in the system namespace", func() {
			kn := testKubernaut()
			for _, svc := range MetricsServices(kn) {
				Expect(svc.Namespace).To(Equal(testSystemNamespace), "Service %q namespace = %q, want %q", svc.Name, svc.Namespace, testSystemNamespace)
			}
		})

		It("includes expected metrics service names", func() {
			kn := testKubernaut()
			svcs := MetricsServices(kn)
			names := make(map[string]bool, len(svcs))
			for _, svc := range svcs {
				names[svc.Name] = true
			}
			expected := []string{
				"signalprocessing-controller-metrics",
				"remediationorchestrator-controller",
				"workflowexecution-controller-metrics",
				"effectivenessmonitor-metrics",
				"notification-metrics",
			}
			for _, name := range expected {
				Expect(names[name]).To(BeTrue(), "MetricsServices() missing expected service %q", name)
			}
		})

		It("exposes only port 9090 on each metrics service", func() {
			kn := testKubernaut()
			for _, svc := range MetricsServices(kn) {
				Expect(svc.Spec.Ports).To(HaveLen(1), "metrics service %q should have exactly 1 port, got %d", svc.Name, len(svc.Spec.Ports))
				Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9090)), "metrics service %q port = %d, want 9090", svc.Name, svc.Spec.Ports[0].Port)
			}
		})
	})

	Context("selectors", func() {
		It("use app labels that match known components", func() {
			kn := testKubernaut()
			all := append(Services(kn, testKnV2(kn)), MetricsServices(kn)...)

			knownComponents := make(map[string]bool)
			for _, c := range AllComponents() {
				knownComponents[c] = true
			}

			for _, svc := range all {
				app, ok := svc.Spec.Selector["app"]
				Expect(ok).To(BeTrue(), "Service %q missing 'app' selector", svc.Name)
				Expect(knownComponents[app]).To(BeTrue(), "Service %q selector app=%q is not a known component", svc.Name, app)
			}
		})
	})
})

func generatedDeploymentsByComponent(kn *kubernautv1alpha2.Kubernaut) map[string]*appsv1.Deployment {
	deployments := getAllDeployments(kn)
	apiFrontend, err := APIFrontendDeployment(kn, testKnV2(kn))
	Expect(err).NotTo(HaveOccurred())
	deployments = append(deployments, apiFrontend)

	byComponent := make(map[string]*appsv1.Deployment, len(deployments))
	for _, deployment := range deployments {
		byComponent[deployment.Spec.Template.Labels["app"]] = deployment
	}
	return byComponent
}

func servicePortTargetsContainerPort(servicePort corev1.ServicePort, containerPort corev1.ContainerPort) bool {
	switch servicePort.TargetPort.Type {
	case intstr.Int:
		return servicePort.TargetPort.IntVal == containerPort.ContainerPort
	case intstr.String:
		return servicePort.TargetPort.StrVal == containerPort.Name
	default:
		return false
	}
}
