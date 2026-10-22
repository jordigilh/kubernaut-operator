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
	"encoding/pem"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	corev1 "k8s.io/api/core/v1"

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
			{input: "", expected: "certmanager"},
			{input: "certmanager", expected: "certmanager"},
			{input: "cert-manager", expected: "certmanager"},
			{input: "manual-admin", expected: "manual-admin"},
			{input: "manual", expected: "manual-admin"},
			{input: "admin", expected: "manual-admin"},
		} {
			actual, err := kind.TLSSourceForValue(test.input)
			Expect(err).NotTo(HaveOccurred(), test.input)
			Expect(actual).To(Equal(test.expected), test.input)
		}
	})

	It("UT-TLS-498-002 rejects unsupported selectors instead of falling back to cert-manager", func() {
		_, err := kind.TLSSourceForValue("plaintext")
		Expect(err).To(MatchError(ContainSubstring("expected certmanager or manual-admin")))
	})

	It("UT-TLS-498-003 maps every lane to the exact v1alpha2 CR mode", func() {
		for _, test := range []struct {
			selector  string
			selection string
			expected  string
		}{
			{selector: "certmanager", expected: "cert-manager"},
			{selector: "manual-admin", selection: "manual", expected: "manual"},
			{selector: "manual-admin", selection: "admin", expected: "manual"},
		} {
			mode, err := kind.TLSModeForSource(test.selector, test.selection)
			Expect(err).NotTo(HaveOccurred(), test)
			Expect(string(mode)).To(Equal(test.expected), test)
		}
	})

	It("UT-TLS-498-004 rejects an unknown manual/admin scenario instead of using manual mode", func() {
		_, err := kind.TLSModeForSource("manual-admin", "unexpected")
		Expect(err).To(MatchError(ContainSubstring("expected manual or admin")))
	})

	It("UT-TLS-498-005 maps every generated workload image to the local contract image", func() {
		const image = "kind.local/kubernaut-contract:test"

		overrides := kind.ContractImageOverrides(image)
		Expect(overrides).To(HaveLen(16))
		for _, name := range []string{
			"gateway", "datastorage", "aianalysis", "signalprocessing", "remediationorchestrator",
			"workflowexecution", "effectivenessmonitor", "notification", "kubernautagent", "authwebhook",
			"apifrontend", "db-migrate", "console", "fleetmetadatacache", "init-ubi-minimal", "oauth2-proxy",
		} {
			Expect(overrides).To(HaveKeyWithValue(name, image), name)
		}
	})

	It("UT-TLS-498-006 labels the TLS trust probe as an allowed managed policy peer", func() {
		labels := kind.TLSProbeLabels()

		Expect(labels).To(HaveKeyWithValue("kubernaut.ai/tls-probe", "true"))
		Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
		Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/instance", "kubernaut"))
	})

	It("UT-TLS-498-007 requires the current peer certificate rather than a stale leaf under the same CA", func() {
		expected := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("current-public-certificate")})
		stale := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("stale-public-certificate")})
		Expect(kind.TLSProbeCertificateMatches(expected, "Subject:current\nCert:"+string(expected))).To(Succeed())
		for _, output := range []string{
			"", "curl verification failed", "Cert:-----BEGIN CERTIFICATE-----\ninvalid PEM",
			"Cert:" + string(stale) + "Cert:" + string(expected),
		} {
			Expect(kind.TLSProbeCertificateMatches(expected, output)).To(HaveOccurred())
		}
		Expect(kind.TLSProbeCertificateMatches([]byte("invalid"), "Cert:"+string(expected))).To(HaveOccurred())
		wrongType := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("public-key")})
		Expect(kind.TLSProbeCertificateMatches(wrongType, "Cert:"+string(expected))).To(HaveOccurred())
	})

	It("UT-TLS-498-008 requests a fresh server-assigned probe identity without changing its trust or policy", func() {
		volume := corev1.Volume{
			Name: "tls-ca",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "selected-public-trust"},
				Items:                []corev1.KeyToPath{{Key: "selected-ca.pem", Path: "ca.crt"}},
			}},
		}
		pod := kind.TLSProbePod(volume)
		Expect(pod.Name).To(BeEmpty(), "never apply a previous terminating Pod by a reusable fixed name")
		Expect(pod.GenerateName).To(Equal("tls-trust-probe-"))
		Expect(pod.Namespace).To(Equal("kubernaut-system"))
		Expect(pod.Labels).To(Equal(kind.TLSProbeLabels()))
		Expect(pod.OwnerReferences).To(BeEmpty())
		Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
		Expect(pod.Spec.Volumes).To(Equal([]corev1.Volume{volume}))
		Expect(pod.Spec.Containers).To(HaveLen(1))
		Expect(pod.Spec.Containers[0].VolumeMounts).To(Equal([]corev1.VolumeMount{{
			Name: "tls-ca", MountPath: "/etc/tls", ReadOnly: true,
		}}))
	})
})
