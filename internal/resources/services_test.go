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
)

const testAuthWebhookServiceName = "authwebhook-service"

var _ = Describe("Services", func() {
	Context("Services()", func() {
		It("returns 6 API services", func() {
			kn := testKubernaut()
			svcs := Services(kn)
			Expect(svcs).To(HaveLen(6))
		})

		It("places all services in the system namespace", func() {
			kn := testKubernaut()
			for _, svc := range Services(kn) {
				Expect(svc.Namespace).To(Equal(testSystemNamespace), "Service %q namespace = %q, want %q", svc.Name, svc.Namespace, testSystemNamespace)
			}
		})

		It("exposes authwebhook on 443 with serving cert annotation", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn) {
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
			for _, svc := range Services(kn) {
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
			svcs := Services(kn)
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
			for _, svc := range Services(kn) {
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

		It("annotates kubernaut-agent with serving cert secret name", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn) {
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
			for _, svc := range Services(kn) {
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

		It("exposes agent-tls port on apifrontend service", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn) {
				if svc.Name == "apifrontend" {
					found = true
					var agentTLS *int32
					for _, p := range svc.Spec.Ports {
						if p.Name == AgentTLSPortName {
							agentTLS = &p.Port
							Expect(p.Port).To(Equal(int32(443)))
							Expect(p.TargetPort.IntValue()).To(Equal(int(PortHTTPS)))
						}
					}
					Expect(agentTLS).NotTo(BeNil(), "apifrontend service should have an %s port", AgentTLSPortName)
					break
				}
			}
			Expect(found).To(BeTrue(), "apifrontend service not found")
		})

		It("SC-8: agent-tls targets SPIRE sidecar port when SPIRE is enabled", func() {
			kn := testKubernautWithAF()
			kn.Spec.APIFrontend.SPIRE.Enabled = true
			found := false
			for _, svc := range Services(kn) {
				if svc.Name == "apifrontend" {
					found = true
					for _, p := range svc.Spec.Ports {
						if p.Name == AgentTLSPortName {
							Expect(p.Port).To(Equal(int32(443)))
							Expect(p.TargetPort.IntValue()).To(Equal(int(SPIRESidecarPort)),
								"agent-tls must target SPIRE sidecar port %d when SPIRE is enabled", SPIRESidecarPort)
							return
						}
					}
					Fail("apifrontend service should have an agent-tls port")
				}
			}
			Expect(found).To(BeTrue(), "apifrontend service not found")
		})

		It("annotates data-storage-service with serving cert secret name", func() {
			kn := testKubernaut()
			found := false
			for _, svc := range Services(kn) {
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
			all := append(Services(kn), MetricsServices(kn)...)

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
