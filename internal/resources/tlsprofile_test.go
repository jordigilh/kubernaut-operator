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

	configv1 "github.com/openshift/api/config/v1"
)

var _ = Describe("MapTLSProfile", func() {
	It("returns empty when profile is nil", func() {
		Expect(MapTLSProfile(nil)).To(BeEmpty())
	})

	It("maps Old type to TLSProfileNameOld", func() {
		p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameOld))
	})

	It("maps Intermediate type to TLSProfileNameIntermediate", func() {
		p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameIntermediate))
	})

	It("maps Modern type to TLSProfileNameModern", func() {
		p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameModern))
	})

	It("maps Custom with TLS 1.3 minimum to Modern", func() {
		p := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS13,
					Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
				},
			},
		}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameModern))
	})

	It("maps Custom with TLS 1.2 minimum to Intermediate", func() {
		p := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				},
			},
		}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameIntermediate))
	})

	It("maps Custom with TLS 1.0 minimum to Old", func() {
		p := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS10,
					Ciphers:       []string{"AES128-SHA"},
				},
			},
		}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameOld))
	})

	It("maps Custom with TLS 1.1 minimum to Old", func() {
		p := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS11,
				},
			},
		}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameOld))
	})

	It("defaults Custom with nil custom field to Intermediate", func() {
		p := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType}
		Expect(MapTLSProfile(p)).To(Equal(TLSProfileNameIntermediate))
	})

	It("returns empty for an unknown type", func() {
		p := &configv1.TLSSecurityProfile{Type: "FutureProfile"}
		Expect(MapTLSProfile(p)).To(BeEmpty())
	})
})
