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
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	EffectivenessMonitorServiceCAConfigMapName = "effectivenessmonitor-service-ca"
	KubernautAgentServiceCAConfigMapName       = "kubernaut-agent-service-ca"
	APIFrontendServiceCAConfigMapName          = "apifrontend-service-ca"
)

// GenericTLSConfigMaps builds the trust ConfigMaps used when the selected TLS
// source is administrator-managed, cert-manager, or explicit development
// self-signed. It deliberately has no OpenShift injection annotations. The
// controller supplies the validated CA PEM after reading or generating the
// selected source.
func GenericTLSConfigMaps(kn *kubernautv1alpha2.Kubernaut, caPEM []byte) []*corev1.ConfigMap {
	data := map[string]string{"service-ca.crt": string(caPEM)}
	definitions := []struct {
		name      string
		component string
	}{
		{InterServiceCAConfigMapName, "inter-service-tls"},
		{TrustBundleConfigMapName, "inter-service-tls"},
		{EffectivenessMonitorServiceCAConfigMapName, ComponentEffectivenessMonitor},
		{KubernautAgentServiceCAConfigMapName, ComponentKubernautAgent},
		{APIFrontendServiceCAConfigMapName, ComponentAPIFrontend},
	}

	configMaps := make([]*corev1.ConfigMap, 0, len(definitions))
	for _, definition := range definitions {
		configMaps = append(configMaps, &corev1.ConfigMap{
			ObjectMeta: ObjectMeta(kn, definition.name, definition.component),
			Data:       copyStringMap(data),
		})
	}
	return configMaps
}

func copyStringMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
