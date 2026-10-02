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

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Ingresses builds the explicitly enabled generic Kubernetes Ingress objects.
// OpenShift Route objects are deliberately not emitted here; the controller
// selects that adapter only when Route discovery succeeds.
func Ingresses(kn *kubernautv1alpha2.Kubernaut) ([]*networkingv1.Ingress, error) {
	definitions := []struct {
		component string
		name      string
		service   string
		port      string
		spec      kubernautv1alpha2.IngressSpec
		enabled   bool
	}{
		{ComponentGateway, "gateway-ingress", "gateway-service", "https", kn.Spec.Gateway.Ingress, kn.Spec.GatewayEnabled()},
		{ComponentAPIFrontend, "apifrontend-ingress", "apifrontend", "https", kn.Spec.APIFrontend.Ingress, kn.Spec.APIFrontendEnabled()},
		{ComponentConsole, "console-ingress", "console", "http", kn.Spec.Console.Ingress, kn.Spec.ConsoleEnabled()},
	}

	ingresses := make([]*networkingv1.Ingress, 0, len(definitions))
	for _, definition := range definitions {
		if !definition.enabled || !definition.spec.IngressEnabled() {
			continue
		}
		if err := validateIngressSpec(definition.component, definition.spec); err != nil {
			return nil, err
		}
		ingresses = append(ingresses, buildIngress(kn, definition.component, definition.name, definition.service, definition.port, definition.spec))
	}
	return ingresses, nil
}

func validateIngressSpec(component string, spec kubernautv1alpha2.IngressSpec) error {
	if spec.IngressClassName == "" {
		return fmt.Errorf("%s ingressClassName is required when generic ingress is enabled", component)
	}
	if spec.Host == "" {
		return fmt.Errorf("%s ingress host is required when generic ingress is enabled", component)
	}
	if spec.TLSSecretName == "" {
		return fmt.Errorf("%s ingress tlsSecretName is required when generic ingress is enabled", component)
	}
	return nil
}

func buildIngress(
	kn *kubernautv1alpha2.Kubernaut,
	component, name, service, port string,
	spec kubernautv1alpha2.IngressSpec,
) *networkingv1.Ingress {
	className := spec.IngressClassName
	pathType := networkingv1.PathTypePrefix

	annotations := make(map[string]string, len(spec.Annotations))
	for key, value := range spec.Annotations {
		annotations[key] = value
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   kn.Namespace,
			Labels:      ComponentLabels(kn, component),
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{spec.Host},
				SecretName: spec.TLSSecretName,
			}},
			Rules: []networkingv1.IngressRule{{
				Host: spec.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: service,
									Port: networkingv1.ServiceBackendPort{Name: port},
								},
							},
						}},
					},
				},
			}},
		},
	}
}
