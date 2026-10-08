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
	"path/filepath"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	// TelemetryMountDir is the deterministic mount root for operator-managed
	// OTLP CA/client material.
	TelemetryMountDir = "/etc/telemetry"
	telemetryCAFile   = TelemetryMountDir + "/ca.crt"
)

// TelemetrySecretReference describes one administrator-owned Secret used by a
// network telemetry lane. It intentionally contains identity and key names,
// never Secret data.
type TelemetrySecretReference struct {
	Name string
	Keys []string
}

type telemetryMaterial struct {
	network          bool
	caFile           string
	caSecretName     string
	caSecretKey      string
	certFile         string
	keyFile          string
	clientSecretName string
}

func resolveTelemetryMaterial(telemetry kubernautv1alpha2.TelemetrySpec) telemetryMaterial {
	material := telemetryMaterial{
		network:  TelemetryNetworkEnabled(telemetry),
		caFile:   telemetry.TLS.CAFile,
		certFile: telemetry.TLS.CertFile,
		keyFile:  telemetry.TLS.KeyFile,
	}
	if telemetry.TLS.CACertSecretRef != nil {
		material.caFile = telemetryCAFile
		material.caSecretName = telemetry.TLS.CACertSecretRef.Name
		material.caSecretKey = telemetry.TLS.CACertSecretRef.Key
		if material.caSecretKey == "" {
			material.caSecretKey = tlsCACertificateKey
		}
	}
	if telemetry.TLS.TLSClientSecretRef != "" {
		material.clientSecretName = telemetry.TLS.TLSClientSecretRef
	}
	return material
}

// TelemetryNetworkEnabled reports whether a telemetry spec creates a network
// OTLP exporter. Empty and stdout endpoints are local-only modes.
func TelemetryNetworkEnabled(telemetry kubernautv1alpha2.TelemetrySpec) bool {
	return telemetry.Endpoint != "" && telemetry.Endpoint != "stdout"
}

// TelemetrySecretReferences returns the non-sensitive Secret identity used by
// a network telemetry lane. It is shared by controller validation and watch
// mapping so those paths cannot drift from Deployment mounting rules.
func TelemetrySecretReferences(telemetry kubernautv1alpha2.TelemetrySpec) []TelemetrySecretReference {
	if !TelemetryNetworkEnabled(telemetry) {
		return nil
	}
	material := resolveTelemetryMaterial(telemetry)
	refs := make([]TelemetrySecretReference, 0, 2)
	if material.caSecretName != "" {
		refs = append(refs, TelemetrySecretReference{
			Name: material.caSecretName,
			Keys: []string{material.caSecretKey},
		})
	}
	if material.clientSecretName != "" {
		refs = append(refs, TelemetrySecretReference{
			Name: material.clientSecretName,
			Keys: []string{"tls.crt", "tls.key"},
		})
	}
	return refs
}

// AppendTelemetrySecretMounts adds read-only Secret-backed OTLP material to a
// component Pod. If CA and client material share a directory, one projected
// volume is used so neither mount hides the other. External file paths and
// local-only telemetry modes add no volume.
func AppendTelemetrySecretMounts(
	telemetry kubernautv1alpha2.TelemetrySpec,
	volumes []corev1.Volume,
	mounts []corev1.VolumeMount,
) ([]corev1.Volume, []corev1.VolumeMount) {
	material := resolveTelemetryMaterial(telemetry)
	if !material.network || (material.caSecretName == "" && material.clientSecretName == "") {
		return volumes, mounts
	}

	caDir := ""
	if material.caSecretName != "" {
		caDir = filepath.Dir(material.caFile)
	}
	clientDir := ""
	if material.clientSecretName != "" {
		clientDir = filepath.Dir(material.certFile)
	}
	if caDir != "" && clientDir != "" && caDir == clientDir {
		items := []corev1.VolumeProjection{
			{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: material.caSecretName},
				Items:                []corev1.KeyToPath{{Key: material.caSecretKey, Path: filepath.Base(material.caFile)}},
			}},
			{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: material.clientSecretName},
				Items: []corev1.KeyToPath{
					{Key: "tls.crt", Path: filepath.Base(material.certFile)},
					{Key: "tls.key", Path: filepath.Base(material.keyFile)},
				},
			}},
		}
		volumes = append(volumes, corev1.Volume{
			Name: "telemetry-material",
			VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
				Sources: items,
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: "telemetry-material", MountPath: caDir, ReadOnly: true})
		return volumes, mounts
	}

	if material.caSecretName != "" {
		volumes = append(volumes, secretKeyVolume(
			"telemetry-ca", material.caSecretName, material.caSecretKey, filepath.Base(material.caFile)))
		mounts = append(mounts, corev1.VolumeMount{Name: "telemetry-ca", MountPath: caDir, ReadOnly: true})
	}
	if material.clientSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "telemetry-client",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: material.clientSecretName,
				Items: []corev1.KeyToPath{
					{Key: "tls.crt", Path: filepath.Base(material.certFile)},
					{Key: "tls.key", Path: filepath.Base(material.keyFile)},
				},
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: "telemetry-client", MountPath: clientDir, ReadOnly: true})
	}
	return volumes, mounts
}
