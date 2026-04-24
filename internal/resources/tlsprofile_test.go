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

	configv1 "github.com/openshift/api/config/v1"
)

func TestMapTLSProfile_Nil(t *testing.T) {
	if got := MapTLSProfile(nil); got != "" {
		t.Fatalf("nil profile should return empty string, got %q", got)
	}
}

func TestMapTLSProfile_Old(t *testing.T) {
	p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}
	if got := MapTLSProfile(p); got != "Old" {
		t.Fatalf("expected Old, got %q", got)
	}
}

func TestMapTLSProfile_Intermediate(t *testing.T) {
	p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	if got := MapTLSProfile(p); got != "Intermediate" {
		t.Fatalf("expected Intermediate, got %q", got)
	}
}

func TestMapTLSProfile_Modern(t *testing.T) {
	p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	if got := MapTLSProfile(p); got != "Modern" {
		t.Fatalf("expected Modern, got %q", got)
	}
}

func TestMapTLSProfile_CustomTLS13(t *testing.T) {
	p := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS13,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			},
		},
	}
	if got := MapTLSProfile(p); got != "Modern" {
		t.Fatalf("Custom with TLS 1.3 should map to Modern, got %q", got)
	}
}

func TestMapTLSProfile_CustomTLS12(t *testing.T) {
	p := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			},
		},
	}
	if got := MapTLSProfile(p); got != "Intermediate" {
		t.Fatalf("Custom with TLS 1.2 should map to Intermediate, got %q", got)
	}
}

func TestMapTLSProfile_CustomTLS10(t *testing.T) {
	p := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS10,
				Ciphers:       []string{"AES128-SHA"},
			},
		},
	}
	if got := MapTLSProfile(p); got != "Old" {
		t.Fatalf("Custom with TLS 1.0 should map to Old, got %q", got)
	}
}

func TestMapTLSProfile_CustomTLS11(t *testing.T) {
	p := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS11,
			},
		},
	}
	if got := MapTLSProfile(p); got != "Old" {
		t.Fatalf("Custom with TLS 1.1 should map to Old, got %q", got)
	}
}

func TestMapTLSProfile_CustomNilCustomField(t *testing.T) {
	p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType}
	if got := MapTLSProfile(p); got != "Intermediate" {
		t.Fatalf("Custom with nil custom field should default to Intermediate, got %q", got)
	}
}

func TestMapTLSProfile_UnknownType(t *testing.T) {
	p := &configv1.TLSSecurityProfile{Type: "FutureProfile"}
	if got := MapTLSProfile(p); got != "" {
		t.Fatalf("unknown type should return empty string, got %q", got)
	}
}
