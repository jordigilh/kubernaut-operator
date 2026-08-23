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
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

var trustBundleLog = logf.Log.WithName("trustbundle")

const (
	// defaultIngressCertNamespace/Name/Key locate the well-known ConfigMap
	// OCP's cluster-ingress-operator publishes specifically so other
	// operators can incorporate the default IngressController's CA into
	// their own trust bundles (Bugzilla #1788711/#1788712) -- Routes
	// (*.apps.<cluster-domain>) are signed by this CA, a separate chain
	// from the service-ca-operator's inter-service CA.
	defaultIngressCertNamespace = "openshift-config-managed"
	defaultIngressCertName      = "default-ingress-cert"
	defaultIngressCertKey       = "ca-bundle.crt"

	trustBundleServiceCAKey = "service-ca.crt"
)

// readServiceCAFunc and readDefaultIngressCertFunc are seams over the live
// cluster reads below so unit tests (internal/resources/*_test.go, the pure
// Go tier -- no envtest/live cluster per AGENTS.md Testing Requirements) can
// simulate success/failure without a real in-cluster config.
var (
	readServiceCAFunc          = readServiceCA
	readDefaultIngressCertFunc = readDefaultIngressCert
)

// readServiceCA reads the OCP service-ca-operator-injected CA bundle from
// the inter-service-ca ConfigMap in the operator's own namespace. Uses a
// direct (uncached) clientset read rather than the reconciler's cached
// client, mirroring liveResolveAPIServerIPs (networkpolicies.go), so the
// corresponding RBAC only needs the `get` verb.
func readServiceCA(namespace string) (string, error) {
	cs, err := trustBundleClientset()
	if err != nil {
		return "", err
	}
	cm, err := cs.CoreV1().ConfigMaps(namespace).Get(context.Background(), InterServiceCAConfigMapName, metav1.GetOptions{}) //nolint:contextcheck // live read outside the reconcile ctx, matching liveResolveAPIServerIPs
	if err != nil {
		return "", fmt.Errorf("get %s/%s configmap: %w", namespace, InterServiceCAConfigMapName, err)
	}
	return cm.Data[trustBundleServiceCAKey], nil
}

// readDefaultIngressCert reads the cluster's default IngressController
// certificate from openshift-config-managed/default-ingress-cert. Absence
// (older clusters, or an admin-managed custom trustedCA) is not treated
// specially here -- callers must fail open on any error.
func readDefaultIngressCert() (string, error) {
	cs, err := trustBundleClientset()
	if err != nil {
		return "", err
	}
	cm, err := cs.CoreV1().ConfigMaps(defaultIngressCertNamespace).Get(context.Background(), defaultIngressCertName, metav1.GetOptions{}) //nolint:contextcheck // live read outside the reconcile ctx, matching liveResolveAPIServerIPs
	if err != nil {
		return "", fmt.Errorf("get %s/%s configmap: %w", defaultIngressCertNamespace, defaultIngressCertName, err)
	}
	return cm.Data[defaultIngressCertKey], nil
}

// trustBundleClientset builds a fresh, uncached Kubernetes clientset from
// the in-cluster config, matching the pattern already proven by
// liveResolveAPIServerIPs (networkpolicies.go): bypassing the manager's
// cached client keeps the RBAC surface to `get` only, no `list`/`watch`.
func trustBundleClientset() (*kubernetes.Clientset, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}
	return cs, nil
}

// MergeTrustBundle concatenates the OCP service-ca bundle with the
// cluster's default ingress/router CA into a single PEM trust bundle.
// Either input may be empty (fail-open: service-ca not yet injected, or
// default-ingress-cert absent on this cluster) -- the result is simply the
// non-empty inputs concatenated, never an error.
func MergeTrustBundle(serviceCA, routerCA string) string {
	parts := make([]string, 0, 2)
	if s := strings.TrimSpace(serviceCA); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(routerCA); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// TrustBundleConfigMap builds the operator-managed ConfigMap that merges the
// OCP service-ca bundle with the cluster's default ingress/router CA so
// clients that verify Route-based TLS (MCP Gateway, Keycloak OIDC) and
// clients that verify internal Service TLS can both trust a single file.
//
// Unlike most resource builders, this performs live (uncached) cluster
// reads internally -- mirroring resolveAPIServerIPs/apiServerEgressRule in
// networkpolicies.go -- because its content cannot be derived from the
// Kubernaut spec alone. Both reads fail open: a read error or missing
// ConfigMap degrades to "not included" rather than blocking reconciliation,
// since the historical inter-service-ca-only bundle remains valid on its
// own.
func TrustBundleConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	serviceCA, err := readServiceCAFunc(kn.Namespace)
	if err != nil {
		trustBundleLog.Info("inter-service-ca not yet available for trust bundle merge; continuing without it", "reason", err.Error())
		serviceCA = ""
	}

	routerCA, err := readDefaultIngressCertFunc()
	if err != nil {
		trustBundleLog.Info("default-ingress-cert not available; trust bundle will not include the cluster's ingress CA", "reason", err.Error())
		routerCA = ""
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, TrustBundleConfigMapName, "inter-service-tls"),
		Data: map[string]string{
			trustBundleServiceCAKey: MergeTrustBundle(serviceCA, routerCA),
		},
	}
}
