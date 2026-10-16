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
	"strings"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	llmTLSRoleInvestigation     = "investigation"
	llmTLSRoleWorkflowDiscovery = "workflow_discovery"
	llmTLSRoleAlignment         = "alignment"
	llmTLSRoleAPIFrontend       = "api_frontend"
	llmTLSRoleSeverityTriage    = "severity_triage"
	llmTLSClientVolumeName      = "llm-tls-client"
	llmTLSClientMountPath       = "/etc/kubernaut-agent/llm-tls-client"
	afLLMTLSClientMountPath     = "/etc/apifrontend/llm-tls-client"
	severityTriageTLSVolumeName = "severity-triage-llm-tls-client"
	severityTriageTLSMountPath  = "/etc/apifrontend/severity-triage-tls"
)

type llmTLSSecretProjection struct {
	name  string
	items []corev1.KeyToPath
}

func llmTLSMaterialMountPath(role string) string {
	switch role {
	case llmTLSRoleWorkflowDiscovery:
		return workflowTLSClientMountPath
	case llmTLSRoleAlignment:
		return alignmentTLSClientMountPath
	case llmTLSRoleAPIFrontend:
		return afLLMTLSClientMountPath
	case llmTLSRoleSeverityTriage:
		return severityTriageTLSMountPath
	default:
		return llmTLSClientMountPath
	}
}

func llmTLSMaterialVolumeName(role string) string {
	switch role {
	case llmTLSRoleWorkflowDiscovery:
		return workflowTLSClientVolumeName
	case llmTLSRoleAlignment:
		return alignmentTLSClientVolumeName
	case llmTLSRoleSeverityTriage:
		return severityTriageTLSVolumeName
	default:
		return llmTLSClientVolumeName
	}
}

func llmTLSCAKey(profile kubernautv1alpha2.LLMProfileSpec) string {
	if profile.TLSCaSecretRef == nil || profile.TLSCaSecretRef.Key == "" {
		return tlsCACertificateKey
	}
	return profile.TLSCaSecretRef.Key
}

func llmTLSMaterialUsesOperatorMount(profile kubernautv1alpha2.LLMProfileSpec) bool {
	return profile.TLSCaSecretRef != nil || profile.TLSClientSecretRef != ""
}

func llmTLSMaterialFileName(path, fallback string) string {
	if path == "" {
		return fallback
	}
	return filepath.Base(path)
}

func llmTLSMaterialIdentity(profile kubernautv1alpha2.LLMProfileSpec) string {
	caSecret := ""
	if profile.TLSCaSecretRef != nil {
		caSecret = profile.TLSCaSecretRef.Name + "#" + llmTLSCAKey(profile)
	}
	return strings.Join([]string{
		caSecret,
		profile.TLSClientSecretRef,
		llmTLSMaterialFileName(profile.TLSCaFile, ""),
		llmTLSMaterialFileName(profile.TLSCertFile, ""),
		llmTLSMaterialFileName(profile.TLSKeyFile, ""),
	}, "\x00")
}

func llmTLSMaterialNeedsDedicatedMount(profile, base kubernautv1alpha2.LLMProfileSpec) bool {
	if !llmTLSMaterialUsesOperatorMount(profile) {
		return false
	}
	return llmTLSMaterialIdentity(profile) != llmTLSMaterialIdentity(base)
}

func llmTLSMaterialPaths(profile, base kubernautv1alpha2.LLMProfileSpec, role string) (string, string, string) {
	caFile := profile.TLSCaFile
	certFile := profile.TLSCertFile
	keyFile := profile.TLSKeyFile
	if !llmTLSMaterialUsesOperatorMount(profile) {
		return caFile, certFile, keyFile
	}

	effectiveRole := role
	if role != llmTLSRoleInvestigation && !llmTLSMaterialNeedsDedicatedMount(profile, base) {
		effectiveRole = llmTLSRoleAPIFrontend
		if role == llmTLSRoleWorkflowDiscovery || role == llmTLSRoleAlignment {
			effectiveRole = llmTLSRoleInvestigation
		}
	}
	mountPath := llmTLSMaterialMountPath(effectiveRole)
	if profile.TLSCaSecretRef != nil && caFile != "" {
		caFile = filepath.Join(mountPath, llmTLSMaterialFileName(caFile, "ca.crt"))
	}
	if profile.TLSClientSecretRef != "" {
		certFile = filepath.Join(mountPath, llmTLSMaterialFileName(certFile, corev1.TLSCertKey))
		keyFile = filepath.Join(mountPath, llmTLSMaterialFileName(keyFile, corev1.TLSPrivateKeyKey))
	}
	return caFile, certFile, keyFile
}

func llmTLSMaterialVolumeFor(profile, base kubernautv1alpha2.LLMProfileSpec, role string) (corev1.Volume, corev1.VolumeMount, bool) {
	if !llmTLSMaterialUsesOperatorMount(profile) {
		return corev1.Volume{}, corev1.VolumeMount{}, false
	}
	effectiveRole := role
	caFile, certFile, keyFile := llmTLSMaterialPaths(profile, base, effectiveRole)
	projections := make([]llmTLSSecretProjection, 0, 2)
	addProjection := func(secretName string, item corev1.KeyToPath) {
		for i := range projections {
			if projections[i].name == secretName {
				projections[i].items = append(projections[i].items, item)
				return
			}
		}
		projections = append(projections, llmTLSSecretProjection{name: secretName, items: []corev1.KeyToPath{item}})
	}
	if profile.TLSCaSecretRef != nil {
		addProjection(profile.TLSCaSecretRef.Name, corev1.KeyToPath{
			Key: llmTLSCAKey(profile), Path: llmTLSMaterialFileName(caFile, "ca.crt"),
		})
	}
	if profile.TLSClientSecretRef != "" {
		addProjection(profile.TLSClientSecretRef, corev1.KeyToPath{
			Key: corev1.TLSCertKey, Path: llmTLSMaterialFileName(certFile, corev1.TLSCertKey),
		})
		addProjection(profile.TLSClientSecretRef, corev1.KeyToPath{
			Key: corev1.TLSPrivateKeyKey, Path: llmTLSMaterialFileName(keyFile, corev1.TLSPrivateKeyKey),
		})
	}

	volumeName := llmTLSMaterialVolumeName(effectiveRole)
	volume := corev1.Volume{Name: volumeName}
	if profile.TLSCaSecretRef == nil && len(projections) == 1 {
		volume.Secret = &corev1.SecretVolumeSource{
			SecretName: projections[0].name,
			Items:      projections[0].items,
		}
	} else {
		sources := make([]corev1.VolumeProjection, 0, len(projections))
		for _, projection := range projections {
			items := append([]corev1.KeyToPath(nil), projection.items...)
			sources = append(sources, corev1.VolumeProjection{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: projection.name},
				Items:                items,
			}})
		}
		volume.Projected = &corev1.ProjectedVolumeSource{Sources: sources}
	}
	return volume, corev1.VolumeMount{
		Name: volumeName, MountPath: llmTLSMaterialMountPath(effectiveRole), ReadOnly: true,
	}, true
}

func llmTLSMaterialPathsForRole(profile, base kubernautv1alpha2.LLMProfileSpec, role string) (string, string, string) {
	return llmTLSMaterialPaths(profile, base, role)
}
