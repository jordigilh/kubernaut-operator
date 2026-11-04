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

package contract

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck

	kind "github.com/jordigilh/kubernaut-operator/test/e2e/kind"
)

var _ = Describe("Issue 609 qualification configuration", func() {
	It("selects only explicit opt-in values", func() {
		for _, value := range []string{"", "0", "false", "off", "no"} {
			Expect(kind.QualificationEnabledForValue(value)).To(BeFalse(), value)
		}
		for _, value := range []string{"1", "true", "on", "yes", " TRUE "} {
			Expect(kind.QualificationEnabledForValue(value)).To(BeTrue(), value)
		}
	})

	It("requires complete immutable sha256 image references", func() {
		valid := "quay.io/example/component:v1.2.3@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		Expect(kind.ValidateImmutableImageReference(valid)).To(Succeed())
		for _, image := range []string{
			"quay.io/example/component:v1.2.3",
			"quay.io/example/component@sha256:0123",
			"quay.io/example/component@sha256:GGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG",
			"",
		} {
			Expect(kind.ValidateImmutableImageReference(image)).NotTo(Succeed(), image)
		}
	})

	It("keeps the qualification image set immutable, including OAuth2 Proxy and Keycloak", func() {
		images := kind.QualificationImageReferences(
			"quay.io/keycloak/keycloak@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"localhost/kubernaut-operator@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		)
		Expect(images).To(HaveKey("oauth2-proxy"))
		Expect(images).To(HaveKey("keycloak"))
		Expect(kind.ValidateQualificationImageReferences(images)).To(Succeed())
	})

	It("uses archive transfer only for Podman qualification image loading", func() {
		Expect(kind.QualificationImageLoadUsesArchive("podman")).To(BeTrue())
		Expect(kind.QualificationImageLoadUsesArchive(" PODMAN ")).To(BeTrue())
		Expect(kind.QualificationImageLoadUsesArchive("docker")).To(BeFalse())
	})
})
