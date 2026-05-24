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
	"fmt"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	kagentiAPIGroup   = "kagenti.dev"
	kagentiAPIVersion = "v1alpha1"
	gatewayAPIGroup   = "gateway.networking.k8s.io"
	gatewayAPIVersion = "v1"
)

// MCPGatewayHTTPRoute builds an unstructured HTTPRoute that routes MCP traffic
// to the apifrontend Service. Returns nil if AF is not enabled.
func MCPGatewayHTTPRoute(kn *kubernautv1alpha1.Kubernaut) *unstructured.Unstructured {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	ns := kn.Namespace
	svcName := ComponentAPIFrontend
	port := int64(PortHTTPS)

	route := &unstructured.Unstructured{}
	route.SetAPIVersion(gatewayAPIGroup + "/" + gatewayAPIVersion)
	route.SetKind("HTTPRoute")
	route.SetName("apifrontend-mcp")
	route.SetNamespace(ns)
	route.SetLabels(ComponentLabels(kn, ComponentAPIFrontend))

	_ = unstructured.SetNestedSlice(route.Object, []interface{}{
		map[string]interface{}{
			"group": gatewayAPIGroup,
			"kind":  "Gateway",
			"name":  "kagenti-gateway",
		},
	}, "spec", "parentRefs")

	_ = unstructured.SetNestedSlice(route.Object, []interface{}{
		map[string]interface{}{
			"matches": []interface{}{
				map[string]interface{}{
					"path": map[string]interface{}{
						"type":  "PathPrefix",
						"value": "/mcp",
					},
				},
			},
			"backendRefs": []interface{}{
				map[string]interface{}{
					"name": svcName,
					"port": port,
				},
			},
		},
	}, "spec", "rules")

	return route
}

// MCPServerRegistration builds an unstructured MCPServerRegistration CR that
// registers the apifrontend with the kagenti MCP Gateway. Returns nil if AF
// is not enabled.
func MCPServerRegistration(kn *kubernautv1alpha1.Kubernaut) *unstructured.Unstructured {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	ns := kn.Namespace
	endpointURL := fmt.Sprintf("https://%s-service.%s.svc.cluster.local:%d/mcp",
		ComponentAPIFrontend, ns, PortHTTPS)

	reg := &unstructured.Unstructured{}
	reg.SetAPIVersion(kagentiAPIGroup + "/" + kagentiAPIVersion)
	reg.SetKind("MCPServerRegistration")
	reg.SetName("kubernaut-apifrontend")
	reg.SetNamespace(ns)
	reg.SetLabels(ComponentLabels(kn, ComponentAPIFrontend))

	spec := map[string]interface{}{
		"serverName":  "kubernaut-apifrontend",
		"transport":   "streamable-http",
		"endpointURL": endpointURL,
	}

	af := kn.Spec.APIFrontend
	if af.Auth.IssuerURL != "" {
		spec["authentication"] = map[string]interface{}{
			"type":      "bearer-jwt",
			"issuerURL": af.Auth.IssuerURL,
			"audience":  withDefault(af.Auth.Audience, "kubernaut-apifrontend"),
		}
	}

	_ = unstructured.SetNestedMap(reg.Object, spec, "spec")
	return reg
}
