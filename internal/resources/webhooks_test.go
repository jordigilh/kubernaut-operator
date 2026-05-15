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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

var _ = Describe("MutatingWebhookConfiguration", func() {
	It("has expected name, annotation, and webhook count", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)

		Expect(mwc.Name).To(Equal("kubernaut-system-authwebhook-mutating"), "name = %q, want %q", mwc.Name, "kubernaut-system-authwebhook-mutating")
		Expect(mwc.Annotations[OCPServiceCAInjectAnnotation]).To(Equal("true"), "MWC should have OCP service-CA inject annotation")
		Expect(mwc.Webhooks).To(HaveLen(3), "should have 3 webhooks, got %d", len(mwc.Webhooks))
	})

	It("uses the expected webhook names", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)

		wantNames := []string{
			"workflowexecution.mutate.kubernaut.ai",
			"remediationapprovalrequest.mutate.kubernaut.ai",
			"remediationrequest.mutate.kubernaut.ai",
		}

		for i, wh := range mwc.Webhooks {
			Expect(wh.Name).To(Equal(wantNames[i]), "webhook[%d] name = %q, want %q", i, wh.Name, wantNames[i])
		}
	})

	It("sets client paths", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)

		wantPaths := []string{
			"/mutate-workflowexecution",
			"/mutate-remediationapprovalrequest",
			"/mutate-remediationrequest",
		}

		for i, wh := range mwc.Webhooks {
			Expect(wh.ClientConfig.Service).NotTo(BeNil(), "webhook[%d] should use service reference", i)
			Expect(wh.ClientConfig.Service.Path).NotTo(BeNil())
			Expect(*wh.ClientConfig.Service.Path).To(Equal(wantPaths[i]), "webhook[%d] path = %v, want %q", i, wh.ClientConfig.Service.Path, wantPaths[i])
		}
	})

	It("defines admission rules", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)

		wantResources := []string{
			"workflowexecutions/status",
			"remediationapprovalrequests/status",
			"remediationrequests/status",
		}

		for i, wh := range mwc.Webhooks {
			Expect(wh.Rules).To(HaveLen(1), "webhook[%d] should have 1 rule, got %d", i, len(wh.Rules))
			rule := wh.Rules[0]

			Expect(rule.Operations).To(HaveLen(1))
			Expect(rule.Operations[0]).To(Equal(admissionregistrationv1.Update), "webhook[%d] should only have UPDATE operation, got %v", i, rule.Operations)

			Expect(rule.APIGroups).NotTo(BeEmpty())
			Expect(rule.APIGroups[0]).To(Equal("kubernaut.ai"), "webhook[%d] apiGroups = %v, want [kubernaut.ai]", i, rule.APIGroups)
			Expect(rule.Resources).NotTo(BeEmpty())
			Expect(rule.Resources[0]).To(Equal(wantResources[i]), "webhook[%d] resources = %v, want [%s]", i, rule.Resources, wantResources[i])
		}
	})

	It("references the authwebhook service", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)

		for i, wh := range mwc.Webhooks {
			svc := wh.ClientConfig.Service
			Expect(svc).NotTo(BeNil(), "webhook[%d] should use service reference", i)
			Expect(svc.Namespace).To(Equal("kubernaut-system"), "webhook[%d] service namespace = %q, want %q", i, svc.Namespace, "kubernaut-system")
			Expect(svc.Name).To(Equal("authwebhook-service"), "webhook[%d] service name = %q, want %q", i, svc.Name, "authwebhook-service")
			Expect(*svc.Port).To(Equal(PortAuthWebhookService), "webhook[%d] service port = %d, want %d", i, *svc.Port, PortAuthWebhookService)
		}
	})
})

var _ = Describe("ValidatingWebhookConfiguration", func() {
	It("has expected name, annotation, and webhook count", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		Expect(vwc.Name).To(Equal("kubernaut-system-authwebhook-validating"), "name = %q, want %q", vwc.Name, "kubernaut-system-authwebhook-validating")
		Expect(vwc.Annotations[OCPServiceCAInjectAnnotation]).To(Equal("true"), "VWC should have OCP service-CA inject annotation")
		Expect(vwc.Webhooks).To(HaveLen(3), "should have 3 webhooks, got %d", len(vwc.Webhooks))
	})

	It("uses the expected webhook names", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		wantNames := []string{
			"notificationrequest.validate.kubernaut.ai",
			"remediationworkflow.validate.kubernaut.ai",
			"actiontype.validate.kubernaut.ai",
		}

		for i, wh := range vwc.Webhooks {
			Expect(wh.Name).To(Equal(wantNames[i]), "webhook[%d] name = %q, want %q", i, wh.Name, wantNames[i])
		}
	})

	It("sets client paths", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		wantPaths := []string{
			"/validate-notificationrequest-delete",
			"/validate-remediationworkflow",
			"/validate-actiontype",
		}

		for i, wh := range vwc.Webhooks {
			Expect(wh.ClientConfig.Service).NotTo(BeNil(), "webhook[%d] should use service reference", i)
			Expect(wh.ClientConfig.Service.Path).NotTo(BeNil())
			Expect(*wh.ClientConfig.Service.Path).To(Equal(wantPaths[i]), "webhook[%d] path = %v, want %q", i, wh.ClientConfig.Service.Path, wantPaths[i])
		}
	})

	It("defines admission rules", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		wantResources := []string{
			"notificationrequests",
			"remediationworkflows",
			"actiontypes",
		}

		for i, wh := range vwc.Webhooks {
			Expect(wh.Rules).To(HaveLen(1), "webhook[%d] should have 1 rule, got %d", i, len(wh.Rules))
			rule := wh.Rules[0]

			if i == 0 {
				Expect(rule.Operations).To(HaveLen(1))
				Expect(rule.Operations[0]).To(Equal(admissionregistrationv1.Delete), "webhook[%d] should only have DELETE, got %v", i, rule.Operations)
			} else {
				Expect(len(rule.Operations)).To(Equal(3), "webhook[%d] should have CREATE, UPDATE, DELETE, got %v", i, rule.Operations)
			}

			Expect(rule.Resources).NotTo(BeEmpty())
			Expect(rule.Resources[0]).To(Equal(wantResources[i]), "webhook[%d] resources = %v, want [%s]", i, rule.Resources, wantResources[i])
		}
	})

	It("applies namespace selectors as expected", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		Expect(vwc.Webhooks[0].NamespaceSelector).To(BeNil(), "notificationrequest validating webhook should not have namespaceSelector")

		for _, i := range []int{1, 2} {
			wh := vwc.Webhooks[i]
			Expect(wh.NamespaceSelector).NotTo(BeNil(), "webhook[%d] should have namespaceSelector", i)
			ns, ok := wh.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			Expect(ok && ns == "kubernaut-system").To(BeTrue(), "webhook[%d] namespaceSelector should match %q, got %v", i, "kubernaut-system", wh.NamespaceSelector.MatchLabels)
		}
	})

	It("sets side effects", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		Expect(*vwc.Webhooks[0].SideEffects).To(Equal(admissionregistrationv1.SideEffectClassNone), "notificationrequest webhook should have SideEffects=None")
		for _, i := range []int{1, 2} {
			Expect(*vwc.Webhooks[i].SideEffects).To(Equal(admissionregistrationv1.SideEffectClassNoneOnDryRun), "webhook[%d] should have SideEffects=NoneOnDryRun", i)
		}
	})

	It("sets timeouts", func() {
		kn := testKubernaut()
		vwc := ValidatingWebhookConfiguration(kn)

		Expect(*vwc.Webhooks[0].TimeoutSeconds).To(Equal(int32(10)), "notificationrequest timeout = %d, want 10", *vwc.Webhooks[0].TimeoutSeconds)
		for _, i := range []int{1, 2} {
			Expect(*vwc.Webhooks[i].TimeoutSeconds).To(Equal(int32(15)), "webhook[%d] timeout = %d, want 15", i, *vwc.Webhooks[i].TimeoutSeconds)
		}
	})
})

var _ = Describe("WebhookConfigurations", func() {
	It("use FailurePolicy=Fail", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)
		vwc := ValidatingWebhookConfiguration(kn)

		for i, wh := range mwc.Webhooks {
			Expect(*wh.FailurePolicy).To(Equal(admissionregistrationv1.Fail), "mutating webhook[%d] should have FailurePolicy=Fail", i)
		}
		for i, wh := range vwc.Webhooks {
			Expect(*wh.FailurePolicy).To(Equal(admissionregistrationv1.Fail), "validating webhook[%d] should have FailurePolicy=Fail", i)
		}
	})

	It("have managed-by labels", func() {
		kn := testKubernaut()
		mwc := MutatingWebhookConfiguration(kn)
		vwc := ValidatingWebhookConfiguration(kn)

		for _, obj := range []struct {
			name   string
			labels map[string]string
		}{
			{"mutating", mwc.Labels},
			{"validating", vwc.Labels},
		} {
			Expect(obj.labels["app.kubernetes.io/managed-by"]).To(Equal("kubernaut-operator"), "%s webhook missing managed-by label", obj.name)
		}
	})
})
