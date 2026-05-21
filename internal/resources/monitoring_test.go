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

var _ = Describe("Monitoring Builders", func() {
	var kn = testKubernaut

	Describe("APIFrontendServiceMonitor", func() {
		It("scrapes the metrics port at /metrics with 15s interval", func() {
			sm := APIFrontendServiceMonitor(kn())
			Expect(sm.Spec.Endpoints).To(HaveLen(1))
			ep := sm.Spec.Endpoints[0]
			Expect(ep.Port).To(Equal("metrics"))
			Expect(ep.Path).To(Equal("/metrics"))
			Expect(string(ep.Interval)).To(Equal("15s"))
		})

		It("relabels job to apifrontend", func() {
			sm := APIFrontendServiceMonitor(kn())
			ep := sm.Spec.Endpoints[0]
			Expect(ep.RelabelConfigs).To(HaveLen(1))
			Expect(ep.RelabelConfigs[0].TargetLabel).To(Equal("job"))
			Expect(*ep.RelabelConfigs[0].Replacement).To(Equal("apifrontend"))
		})

		It("selects apifrontend pods via component labels", func() {
			sm := APIFrontendServiceMonitor(kn())
			Expect(sm.Spec.Selector.MatchLabels).To(Equal(SelectorLabels(ComponentAPIFrontend)))
		})

		It("restricts to the kubernaut namespace", func() {
			sm := APIFrontendServiceMonitor(kn())
			Expect(sm.Spec.NamespaceSelector.MatchNames).To(ConsistOf(testSystemNamespace))
		})

		It("sets standard labels and namespace", func() {
			sm := APIFrontendServiceMonitor(kn())
			Expect(sm.Namespace).To(Equal(testSystemNamespace))
			Expect(sm.Labels["app.kubernetes.io/managed-by"]).To(Equal("kubernaut-operator"))
		})
	})

	Describe("APIFrontendPrometheusRule", func() {
		It("defines 6 rule groups", func() {
			pr := APIFrontendPrometheusRule(kn())
			Expect(pr.Spec.Groups).To(HaveLen(6))
		})

		It("includes the ApifrontendDown critical alert", func() {
			pr := APIFrontendPrometheusRule(kn())
			found := false
			for _, g := range pr.Spec.Groups {
				for _, r := range g.Rules {
					if r.Alert == "ApifrontendDown" {
						Expect(r.Labels["severity"]).To(Equal("critical"))
						Expect(r.Expr.String()).To(ContainSubstring(`up{job="apifrontend"}`))
						found = true
					}
				}
			}
			Expect(found).To(BeTrue(), "ApifrontendDown alert not found")
		})

		It("all alert expressions reference job=apifrontend", func() {
			pr := APIFrontendPrometheusRule(kn())
			for _, g := range pr.Spec.Groups {
				for _, r := range g.Rules {
					if r.Alert != "" {
						Expect(r.Expr.String()).To(ContainSubstring(`job="apifrontend"`),
							"Alert %q expression should filter on job=apifrontend", r.Alert)
					}
				}
			}
		})

		It("group names are prefixed with apifrontend.", func() {
			pr := APIFrontendPrometheusRule(kn())
			for _, g := range pr.Spec.Groups {
				Expect(g.Name).To(HavePrefix("apifrontend."),
					"Rule group %q should be prefixed with apifrontend.", g.Name)
			}
		})

		It("all alerts include runbook_url annotation", func() {
			pr := APIFrontendPrometheusRule(kn())
			for _, g := range pr.Spec.Groups {
				for _, r := range g.Rules {
					if r.Alert != "" {
						Expect(r.Annotations).To(HaveKey("runbook_url"),
							"Alert %q should have a runbook_url annotation", r.Alert)
					}
				}
			}
		})
	})

	Describe("DataStorageServiceMonitor", func() {
		It("scrapes the metrics port at /metrics with 15s interval", func() {
			sm := DataStorageServiceMonitor(kn())
			Expect(sm.Spec.Endpoints).To(HaveLen(1))
			ep := sm.Spec.Endpoints[0]
			Expect(ep.Port).To(Equal("metrics"))
			Expect(ep.Path).To(Equal("/metrics"))
			Expect(string(ep.Interval)).To(Equal("15s"))
		})

		It("relabels job to datastorage", func() {
			sm := DataStorageServiceMonitor(kn())
			ep := sm.Spec.Endpoints[0]
			Expect(ep.RelabelConfigs).To(HaveLen(1))
			Expect(*ep.RelabelConfigs[0].Replacement).To(Equal("datastorage"))
		})

		It("selects datastorage pods via component labels", func() {
			sm := DataStorageServiceMonitor(kn())
			Expect(sm.Spec.Selector.MatchLabels).To(Equal(SelectorLabels(ComponentDataStorage)))
		})
	})

	Describe("DataStoragePrometheusRule", func() {
		It("defines 4 rule groups", func() {
			pr := DataStoragePrometheusRule(kn())
			Expect(pr.Spec.Groups).To(HaveLen(4))
		})

		It("includes DLQ depth and processing error alerts", func() {
			pr := DataStoragePrometheusRule(kn())
			alertNames := map[string]bool{}
			for _, g := range pr.Spec.Groups {
				for _, r := range g.Rules {
					alertNames[r.Alert] = true
				}
			}
			Expect(alertNames).To(HaveKey("DataStorageDLQDepthHigh"))
			Expect(alertNames).To(HaveKey("DataStorageDLQProcessingErrors"))
			Expect(alertNames).To(HaveKey("DataStorageDown"))
			Expect(alertNames).To(HaveKey("DataStorageHighLatencyP95"))
			Expect(alertNames).To(HaveKey("DataStorageDBConnectionPoolExhausted"))
		})

		It("group names are prefixed with datastorage.", func() {
			pr := DataStoragePrometheusRule(kn())
			for _, g := range pr.Spec.Groups {
				Expect(g.Name).To(HavePrefix("datastorage."),
					"Rule group %q should be prefixed with datastorage.", g.Name)
			}
		})
	})
})
