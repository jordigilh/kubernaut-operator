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
	caBundle := []byte("fake-ca")
	mwc := MutatingWebhookConfiguration(kn, caBundle)

	if mwc.Name != "authwebhook-mutating" {
		t.Errorf("name = %q, want %q", mwc.Name, "authwebhook-mutating")
	}
	if len(mwc.Webhooks) != 1 {
		t.Fatalf("should have 1 webhook, got %d", len(mwc.Webhooks))
	}

	wh := mwc.Webhooks[0]
	if wh.Name != "mutating.authwebhook.kubernaut.ai" {
		t.Errorf("webhook name = %q", wh.Name)
	}
	if wh.ClientConfig.Service == nil {
		t.Fatal("webhook should use service reference")
	}
	if wh.ClientConfig.Service.Namespace != "kubernaut-system" {
		t.Errorf("service namespace = %q, want %q", wh.ClientConfig.Service.Namespace, "kubernaut-system")
	}
	if wh.ClientConfig.Service.Name != "authwebhook-service" {
		t.Errorf("service name = %q, want %q", wh.ClientConfig.Service.Name, "authwebhook-service")
	}
	if *wh.ClientConfig.Service.Port != 8443 {
		t.Errorf("service port = %d, want 8443", *wh.ClientConfig.Service.Port)
	}
	if string(wh.ClientConfig.CABundle) != "fake-ca" {
		t.Error("caBundle should be set")
	}
}

func TestMutatingWebhookConfiguration_Rules(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn, nil)

	wh := mwc.Webhooks[0]
	if len(wh.Rules) != 1 {
		t.Fatalf("should have 1 rule, got %d", len(wh.Rules))
	}
	rule := wh.Rules[0]

	hasCreate := false
	hasUpdate := false
	for _, op := range rule.Operations {
		if op == admissionregistrationv1.Create {
			hasCreate = true
		}
		if op == admissionregistrationv1.Update {
			hasUpdate = true
		}
	}
	if !hasCreate || !hasUpdate {
		t.Errorf("rule should include Create and Update operations, got %v", rule.Operations)
	}

	if len(rule.APIGroups) == 0 || rule.APIGroups[0] != "kubernaut.ai" {
		t.Errorf("rule apiGroups = %v, want [kubernaut.ai]", rule.APIGroups)
	}
	if len(rule.Resources) == 0 || rule.Resources[0] != "actiontypes" {
		t.Errorf("rule resources = %v, want [actiontypes]", rule.Resources)
	}
}

func TestMutatingWebhookConfiguration_Path(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn, nil)

	wh := mwc.Webhooks[0]
	if wh.ClientConfig.Service.Path == nil || *wh.ClientConfig.Service.Path != "/mutate" {
		t.Errorf("mutating webhook path = %v, want /mutate", wh.ClientConfig.Service.Path)
	}
}

func TestValidatingWebhookConfiguration_Basic(t *testing.T) {
	kn := testKubernaut()
	caBundle := []byte("fake-ca")
	vwc := ValidatingWebhookConfiguration(kn, caBundle)

	if vwc.Name != "authwebhook-validating" {
		t.Errorf("name = %q, want %q", vwc.Name, "authwebhook-validating")
	}
	if len(vwc.Webhooks) != 1 {
		t.Fatalf("should have 1 webhook, got %d", len(vwc.Webhooks))
	}

	wh := vwc.Webhooks[0]
	if wh.Name != "validating.authwebhook.kubernaut.ai" {
		t.Errorf("webhook name = %q", wh.Name)
	}
	if *wh.ClientConfig.Service.Path != "/validate" {
		t.Errorf("service path = %q, want %q", *wh.ClientConfig.Service.Path, "/validate")
	}
}

func TestWebhookConfigurations_SideEffectsNone(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn, nil)
	vwc := ValidatingWebhookConfiguration(kn, nil)

	if *mwc.Webhooks[0].SideEffects != admissionregistrationv1.SideEffectClassNone {
		t.Error("mutating webhook should have SideEffects=None")
	}
	if *vwc.Webhooks[0].SideEffects != admissionregistrationv1.SideEffectClassNone {
		t.Error("validating webhook should have SideEffects=None")
	}
}

func TestWebhookConfigurations_FailurePolicy(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn, nil)
	vwc := ValidatingWebhookConfiguration(kn, nil)

	if *mwc.Webhooks[0].FailurePolicy != admissionregistrationv1.Fail {
		t.Error("mutating webhook should have FailurePolicy=Fail")
	}
	if *vwc.Webhooks[0].FailurePolicy != admissionregistrationv1.Fail {
		t.Error("validating webhook should have FailurePolicy=Fail")
	}
}

func TestWebhookConfigurations_HaveCommonLabels(t *testing.T) {
	kn := testKubernaut()
	mwc := MutatingWebhookConfiguration(kn, nil)
	vwc := ValidatingWebhookConfiguration(kn, nil)

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
