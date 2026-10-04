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
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck

	kind "github.com/jordigilh/kubernaut-operator/test/e2e/kind"
)

func TestTLSSourceContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kind TLS source contract suite")
}

var _ = Describe("Kind TLS source selector", func() {
	It("UT-TLS-498-001 maps every supported lane selector deterministically", func() {
		for _, test := range []struct {
			input    string
			expected string
		}{
			{input: "", expected: "development"},
			{input: "development", expected: "development"},
			{input: "development-self-signed", expected: "development"},
			{input: "hook", expected: "hook"},
			{input: "manual-admin", expected: "manual-admin"},
			{input: "manual", expected: "manual-admin"},
			{input: "administrator-managed", expected: "manual-admin"},
			{input: "AdministratorManaged", expected: "manual-admin"},
			{input: "certmanager", expected: "certmanager"},
			{input: "cert-manager", expected: "certmanager"},
		} {
			actual, err := kind.TLSSourceForValue(test.input)
			Expect(err).NotTo(HaveOccurred(), test.input)
			Expect(actual).To(Equal(test.expected), test.input)
		}
	})

	It("UT-TLS-498-002 rejects unsupported selectors instead of falling back to development TLS", func() {
		_, err := kind.TLSSourceForValue("plaintext")
		Expect(err).To(MatchError(ContainSubstring("expected development, hook, manual-admin, or certmanager")))
	})

	It("UT-TLS-498-003 maps every lane to the exact v1alpha2 CR mode", func() {
		for _, test := range []struct {
			selector  string
			selection string
			expected  string
		}{
			{selector: "development", expected: "DevelopmentSelfSigned"},
			{selector: "hook", expected: "hook"},
			{selector: "certmanager", expected: "CertManager"},
			{selector: "manual-admin", selection: "manual", expected: "manual"},
			{selector: "manual-admin", selection: "administrator-managed", expected: "AdministratorManaged"},
		} {
			mode, err := kind.TLSModeForSource(test.selector, test.selection)
			Expect(err).NotTo(HaveOccurred(), test)
			Expect(string(mode)).To(Equal(test.expected), test)
		}
	})

	It("UT-TLS-498-004 rejects an unknown manual/admin scenario instead of using manual mode", func() {
		_, err := kind.TLSModeForSource("manual-admin", "unexpected")
		Expect(err).To(MatchError(ContainSubstring("expected manual or administrator-managed")))
	})
})
