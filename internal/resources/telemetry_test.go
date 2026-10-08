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
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("OTLP telemetry Secret material", func() {
	It("does not return Secret references for local-only telemetry", func() {
		Expect(TelemetrySecretReferences(kubernautv1alpha2.TelemetrySpec{Endpoint: "stdout"})).To(BeNil())
		Expect(TelemetrySecretReferences(kubernautv1alpha2.TelemetrySpec{LogSink: boolPtr(true)})).To(BeNil())
	})

	It("returns the selected CA identity and defaults its key", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "collector-ca"},
			},
		}
		Expect(TelemetrySecretReferences(telemetry)).To(Equal([]TelemetrySecretReference{{
			Name: "collector-ca", Keys: []string{"ca.crt"},
		}}))
	})

	It("returns client Secret identity with the fixed TLS keys", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CertFile:           TelemetryMountDir + "/tls.crt",
				KeyFile:            TelemetryMountDir + "/tls.key",
				TLSClientSecretRef: "collector-client",
			},
		}
		Expect(TelemetrySecretReferences(telemetry)).To(Equal([]TelemetrySecretReference{{
			Name: "collector-client", Keys: []string{"tls.crt", "tls.key"},
		}}))
	})

	It("returns CA before client identity when both are configured", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef:    &kubernautv1alpha2.CACertSecretRef{Name: "collector-ca", Key: "collector.pem"},
				CertFile:           TelemetryMountDir + "/tls.crt",
				KeyFile:            TelemetryMountDir + "/tls.key",
				TLSClientSecretRef: "collector-client",
			},
		}
		Expect(TelemetrySecretReferences(telemetry)).To(Equal([]TelemetrySecretReference{
			{Name: "collector-ca", Keys: []string{"collector.pem"}},
			{Name: "collector-client", Keys: []string{"tls.crt", "tls.key"}},
		}))
	})

	It("does not add mounts for a disabled network lane", func() {
		volumes := []corev1.Volume{{Name: "existing"}}
		mounts := []corev1.VolumeMount{{Name: "existing", MountPath: "/existing"}}
		gotVolumes, gotMounts := AppendTelemetrySecretMounts(kubernautv1alpha2.TelemetrySpec{}, volumes, mounts)
		Expect(gotVolumes).To(Equal(volumes))
		Expect(gotMounts).To(Equal(mounts))
	})

	It("mounts CA material as a read-only Secret volume", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "collector-ca", Key: "collector.pem"},
			},
		}
		volumes, mounts := AppendTelemetrySecretMounts(telemetry, nil, nil)
		Expect(volumes).To(HaveLen(1))
		Expect(volumes[0].Name).To(Equal("telemetry-ca"))
		Expect(volumes[0].Secret.SecretName).To(Equal("collector-ca"))
		Expect(volumes[0].Secret.Items).To(Equal([]corev1.KeyToPath{{Key: "collector.pem", Path: "ca.crt"}}))
		Expect(mounts).To(Equal([]corev1.VolumeMount{{Name: "telemetry-ca", MountPath: TelemetryMountDir, ReadOnly: true}}))
	})

	It("mounts client material as a read-only Secret volume", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CertFile:           TelemetryMountDir + "/tls.crt",
				KeyFile:            TelemetryMountDir + "/tls.key",
				TLSClientSecretRef: "collector-client",
			},
		}
		volumes, mounts := AppendTelemetrySecretMounts(telemetry, nil, nil)
		Expect(volumes).To(HaveLen(1))
		Expect(volumes[0].Name).To(Equal("telemetry-client"))
		Expect(volumes[0].Secret.SecretName).To(Equal("collector-client"))
		Expect(volumes[0].Secret.Items).To(Equal([]corev1.KeyToPath{
			{Key: "tls.crt", Path: "tls.crt"},
			{Key: "tls.key", Path: "tls.key"},
		}))
		Expect(mounts).To(Equal([]corev1.VolumeMount{{Name: "telemetry-client", MountPath: TelemetryMountDir, ReadOnly: true}}))
	})

	It("projects CA and client material together when they share a directory", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef:    &kubernautv1alpha2.CACertSecretRef{Name: "collector-ca"},
				CertFile:           TelemetryMountDir + "/tls.crt",
				KeyFile:            TelemetryMountDir + "/tls.key",
				TLSClientSecretRef: "collector-client",
			},
		}
		volumes, mounts := AppendTelemetrySecretMounts(telemetry, nil, nil)
		Expect(volumes).To(HaveLen(1))
		Expect(volumes[0].Name).To(Equal("telemetry-material"))
		Expect(volumes[0].Projected.Sources).To(HaveLen(2))
		Expect(mounts).To(Equal([]corev1.VolumeMount{{Name: "telemetry-material", MountPath: TelemetryMountDir, ReadOnly: true}}))
	})

	It("keeps CA and client mounts separate when their directories differ", func() {
		telemetry := kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef:    &kubernautv1alpha2.CACertSecretRef{Name: "collector-ca"},
				CertFile:           "/etc/collector/tls.crt",
				KeyFile:            "/etc/collector/tls.key",
				TLSClientSecretRef: "collector-client",
			},
		}
		volumes, mounts := AppendTelemetrySecretMounts(telemetry, nil, nil)
		Expect(volumes).To(HaveLen(2))
		Expect(mounts).To(ConsistOf(
			corev1.VolumeMount{Name: "telemetry-ca", MountPath: TelemetryMountDir, ReadOnly: true},
			corev1.VolumeMount{Name: "telemetry-client", MountPath: "/etc/collector", ReadOnly: true},
		))
	})
})
