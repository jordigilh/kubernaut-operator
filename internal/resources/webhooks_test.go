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
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func TestMutatingWebhookConfiguration_Basic(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)

	if mwc.Name != "kubernaut-system-authwebhook-mutating" {
		t.Errorf("name = %q, want %q", mwc.Name, "kubernaut-system-authwebhook-mutating")
	}
	if mwc.Annotations[OCPServiceCAInjectAnnotation] != "true" {
		t.Error("MWC should have OCP service-CA inject annotation")
	}
	if len(mwc.Webhooks) != 3 {
		t.Fatalf("should have 3 webhooks, got %d", len(mwc.Webhooks))
	}
}

func TestMutatingWebhookConfiguration_WebhookNames(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)

	wantNames := []string{
		"workflowexecution.mutate.kubernaut.ai",
		"remediationapprovalrequest.mutate.kubernaut.ai",
		"remediationrequest.mutate.kubernaut.ai",
	}

	for i, wh := range mwc.Webhooks {
		if wh.Name != wantNames[i] {
			t.Errorf("webhook[%d] name = %q, want %q", i, wh.Name, wantNames[i])
		}
	}
}

func TestMutatingWebhookConfiguration_Paths(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)

	wantPaths := []string{
		"/mutate-workflowexecution",
		"/mutate-remediationapprovalrequest",
		"/mutate-remediationrequest",
	}

	for i, wh := range mwc.Webhooks {
		if wh.ClientConfig.Service == nil {
			t.Fatalf("webhook[%d] should use service reference", i)
		}
		if wh.ClientConfig.Service.Path == nil || *wh.ClientConfig.Service.Path != wantPaths[i] {
			t.Errorf("webhook[%d] path = %v, want %q", i, wh.ClientConfig.Service.Path, wantPaths[i])
		}
	}
}

func TestMutatingWebhookConfiguration_Rules(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)

	wantResources := []string{
		"workflowexecutions/status",
		"remediationapprovalrequests/status",
		"remediationrequests/status",
	}

	for i, wh := range mwc.Webhooks {
		if len(wh.Rules) != 1 {
			t.Fatalf("webhook[%d] should have 1 rule, got %d", i, len(wh.Rules))
		}
		rule := wh.Rules[0]

		if len(rule.Operations) != 1 || rule.Operations[0] != admissionregistrationv1.Update {
			t.Errorf("webhook[%d] should only have UPDATE operation, got %v", i, rule.Operations)
		}

		if len(rule.APIGroups) == 0 || rule.APIGroups[0] != "kubernaut.ai" {
			t.Errorf("webhook[%d] apiGroups = %v, want [kubernaut.ai]", i, rule.APIGroups)
		}
		if len(rule.Resources) == 0 || rule.Resources[0] != wantResources[i] {
			t.Errorf("webhook[%d] resources = %v, want [%s]", i, rule.Resources, wantResources[i])
		}
	}
}

func TestMutatingWebhookConfiguration_ServiceReference(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)

	for i, wh := range mwc.Webhooks {
		svc := wh.ClientConfig.Service
		if svc == nil {
			t.Fatalf("webhook[%d] should use service reference", i)
		}
		if svc.Namespace != "kubernaut-system" {
			t.Errorf("webhook[%d] service namespace = %q, want %q", i, svc.Namespace, "kubernaut-system")
		}
		if svc.Name != "authwebhook-service" {
			t.Errorf("webhook[%d] service name = %q, want %q", i, svc.Name, "authwebhook-service")
		}
		if *svc.Port != PortAuthWebhookService {
			t.Errorf("webhook[%d] service port = %d, want %d", i, *svc.Port, PortAuthWebhookService)
		}
	}
}

func TestValidatingWebhookConfiguration_Basic(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	if vwc.Name != "kubernaut-system-authwebhook-validating" {
		t.Errorf("name = %q, want %q", vwc.Name, "kubernaut-system-authwebhook-validating")
	}
	if vwc.Annotations[OCPServiceCAInjectAnnotation] != "true" {
		t.Error("VWC should have OCP service-CA inject annotation")
	}
	if len(vwc.Webhooks) != 3 {
		t.Fatalf("should have 3 webhooks, got %d", len(vwc.Webhooks))
	}
}

func TestValidatingWebhookConfiguration_WebhookNames(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	wantNames := []string{
		"notificationrequest.validate.kubernaut.ai",
		"remediationworkflow.validate.kubernaut.ai",
		"actiontype.validate.kubernaut.ai",
	}

	for i, wh := range vwc.Webhooks {
		if wh.Name != wantNames[i] {
			t.Errorf("webhook[%d] name = %q, want %q", i, wh.Name, wantNames[i])
		}
	}
}

func TestValidatingWebhookConfiguration_Paths(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	wantPaths := []string{
		"/validate-notificationrequest-delete",
		"/validate-remediationworkflow",
		"/validate-actiontype",
	}

	for i, wh := range vwc.Webhooks {
		if wh.ClientConfig.Service == nil {
			t.Fatalf("webhook[%d] should use service reference", i)
		}
		if wh.ClientConfig.Service.Path == nil || *wh.ClientConfig.Service.Path != wantPaths[i] {
			t.Errorf("webhook[%d] path = %v, want %q", i, wh.ClientConfig.Service.Path, wantPaths[i])
		}
	}
}

func TestValidatingWebhookConfiguration_Rules(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	wantResources := []string{
		"notificationrequests",
		"remediationworkflows",
		"actiontypes",
	}

	// notificationrequest: DELETE only; others: CREATE, UPDATE, DELETE
	for i, wh := range vwc.Webhooks {
		if len(wh.Rules) != 1 {
			t.Fatalf("webhook[%d] should have 1 rule, got %d", i, len(wh.Rules))
		}
		rule := wh.Rules[0]

		if i == 0 {
			if len(rule.Operations) != 1 || rule.Operations[0] != admissionregistrationv1.Delete {
				t.Errorf("webhook[%d] should only have DELETE, got %v", i, rule.Operations)
			}
		} else {
			if len(rule.Operations) != 3 {
				t.Errorf("webhook[%d] should have CREATE, UPDATE, DELETE, got %v", i, rule.Operations)
			}
		}

		if len(rule.Resources) == 0 || rule.Resources[0] != wantResources[i] {
			t.Errorf("webhook[%d] resources = %v, want [%s]", i, rule.Resources, wantResources[i])
		}
	}
}

func TestValidatingWebhookConfiguration_NamespaceSelector(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	// notificationrequest (index 0) has no namespaceSelector
	if vwc.Webhooks[0].NamespaceSelector != nil {
		t.Error("notificationrequest validating webhook should not have namespaceSelector")
	}

	// remediationworkflow and actiontype should have namespaceSelector
	for _, i := range []int{1, 2} {
		wh := vwc.Webhooks[i]
		if wh.NamespaceSelector == nil {
			t.Errorf("webhook[%d] should have namespaceSelector", i)
			continue
		}
		ns, ok := wh.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
		if !ok || ns != "kubernaut-system" {
			t.Errorf("webhook[%d] namespaceSelector should match %q, got %v", i, "kubernaut-system", wh.NamespaceSelector.MatchLabels)
		}
	}
}

func TestValidatingWebhookConfiguration_SideEffects(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	// notificationrequest: SideEffects=None
	if *vwc.Webhooks[0].SideEffects != admissionregistrationv1.SideEffectClassNone {
		t.Error("notificationrequest webhook should have SideEffects=None")
	}
	// remediationworkflow and actiontype: SideEffects=NoneOnDryRun
	for _, i := range []int{1, 2} {
		if *vwc.Webhooks[i].SideEffects != admissionregistrationv1.SideEffectClassNoneOnDryRun {
			t.Errorf("webhook[%d] should have SideEffects=NoneOnDryRun", i)
		}
	}
}

func TestValidatingWebhookConfiguration_Timeouts(t *testing.T) {
	kn := testKubernaut()
	vwc := ValidatingWebhookConfiguration(kn)

	// notificationrequest: 10s; others: 15s
	if *vwc.Webhooks[0].TimeoutSeconds != 10 {
		t.Errorf("notificationrequest timeout = %d, want 10", *vwc.Webhooks[0].TimeoutSeconds)
	}
	for _, i := range []int{1, 2} {
		if *vwc.Webhooks[i].TimeoutSeconds != 15 {
			t.Errorf("webhook[%d] timeout = %d, want 15", i, *vwc.Webhooks[i].TimeoutSeconds)
		}
	}
}

func TestWebhookConfigurations_FailurePolicy(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn)
	vwc := ValidatingWebhookConfiguration(kn)

	for i, wh := range mwc.Webhooks {
		if *wh.FailurePolicy != admissionregistrationv1.Fail {
			t.Errorf("mutating webhook[%d] should have FailurePolicy=Fail", i)
		}
	}
	for i, wh := range vwc.Webhooks {
		if *wh.FailurePolicy != admissionregistrationv1.Fail {
			t.Errorf("validating webhook[%d] should have FailurePolicy=Fail", i)
		}
	}
}

func TestWebhookConfigurations_HaveCommonLabels(t *testing.T) {
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
		if obj.labels["app.kubernetes.io/managed-by"] != "kubernaut-operator" {
			t.Errorf("%s webhook missing managed-by label", obj.name)
		}
	}
}
