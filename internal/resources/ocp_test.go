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

	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testGatewayServiceName     = "gateway-service"
	testOperatorManagedByValue = "kubernaut-operator"
)

var _ = Describe("GatewayRoute", func() {
	It("is enabled by default with reencrypt TLS targeting https port", func() {
		kn := testKubernaut()
		route := GatewayRoute(kn)

		Expect(route).NotTo(BeNil(), "GatewayRoute should not be nil when enabled by default")
		Expect(route.Name).To(Equal("gateway-route"), "name = %q, want %q", route.Name, "gateway-route")
		Expect(route.Namespace).To(Equal(testSystemNamespace), "namespace = %q, want %q", route.Namespace, testSystemNamespace)
		Expect(route.Spec.To.Name).To(Equal(testGatewayServiceName), "route target = %q, want %q", route.Spec.To.Name, testGatewayServiceName)
		Expect(route.Spec.Port.TargetPort.StrVal).To(Equal("https"), "target port = %q, want %q", route.Spec.Port.TargetPort.StrVal, "https")
		Expect(route.Spec.TLS).NotTo(BeNil(), "route should have TLS config")
		Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationReencrypt), "TLS termination = %q, want %q", route.Spec.TLS.Termination, routev1.TLSTerminationReencrypt)
		Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect), "insecure policy = %q, want Redirect", route.Spec.TLS.InsecureEdgeTerminationPolicy)
	})

	It("returns nil when explicitly disabled", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.Gateway.Route.Enabled = &disabled
		route := GatewayRoute(kn)

		Expect(route).To(BeNil(), "GatewayRoute should be nil when explicitly disabled")
	})

	It("sets a custom hostname", func() {
		kn := testKubernaut()
		kn.Spec.Gateway.Route.Hostname = "kubernaut.example.com"
		route := GatewayRoute(kn)

		Expect(route.Spec.Host).To(Equal("kubernaut.example.com"), "route host = %q, want %q", route.Spec.Host, "kubernaut.example.com")
	})

	It("has no hostname by default", func() {
		kn := testKubernaut()
		route := GatewayRoute(kn)

		Expect(route.Spec.Host).To(BeEmpty(), "route host should be empty by default, got %q", route.Spec.Host)
	})

	It("sets HAProxy timeout annotation for SSE streaming", func() {
		kn := testKubernaut()
		route := GatewayRoute(kn)

		Expect(route.Annotations).To(HaveKeyWithValue("haproxy.router.openshift.io/timeout", routeSSETimeout),
			"gateway route should have HAProxy timeout annotation for SSE streams")
	})
})

var _ = Describe("GatewayRouteStub", func() {
	It("has minimal metadata", func() {
		kn := testKubernaut()
		stub := GatewayRouteStub(kn)

		Expect(stub.Name).To(Equal("gateway-route"), "Name = %q, want %q", stub.Name, "gateway-route")
		Expect(stub.Namespace).To(Equal(kn.Namespace), "Namespace = %q, want %q", stub.Namespace, kn.Namespace)
	})
})

var _ = Describe("APIFrontendRoute", func() {
	It("SC-8: is disabled by default (opt-in external access)", func() {
		kn := testKubernaut()
		route := APIFrontendRoute(kn)

		Expect(route).To(BeNil(), "AF Route should be nil by default (opt-in)")
	})

	It("SC-8: uses reencrypt TLS termination for end-to-end encryption", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.APIFrontend.Route.Enabled = &enabled
		route := APIFrontendRoute(kn)

		Expect(route).NotTo(BeNil())
		Expect(route.Spec.TLS).NotTo(BeNil())
		Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationReencrypt),
			"AF Route must use reencrypt TLS termination (SC-8)")
		Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect),
			"insecure edge traffic must redirect to HTTPS (SC-8)")
	})

	It("SC-8: targets apifrontend service on HTTPS port", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.APIFrontend.Route.Enabled = &enabled
		route := APIFrontendRoute(kn)

		Expect(route).NotTo(BeNil())
		Expect(route.Spec.To.Kind).To(Equal("Service"))
		Expect(route.Spec.To.Name).To(Equal("apifrontend"))
		Expect(route.Spec.Port.TargetPort.StrVal).To(Equal("https"))
	})

	It("SC-8: sets custom hostname when configured", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.APIFrontend.Route.Enabled = &enabled
		kn.Spec.APIFrontend.Route.Hostname = "af.kubernaut.example.com"
		route := APIFrontendRoute(kn)

		Expect(route).NotTo(BeNil())
		Expect(route.Spec.Host).To(Equal("af.kubernaut.example.com"))
	})

	It("SC-8: auto-generates hostname when not configured", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.APIFrontend.Route.Enabled = &enabled
		route := APIFrontendRoute(kn)

		Expect(route).NotTo(BeNil())
		Expect(route.Spec.Host).To(BeEmpty(), "hostname should be empty for OCP auto-generation")
	})

	It("sets HAProxy timeout annotation for SSE streaming", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.APIFrontend.Route.Enabled = &enabled
		route := APIFrontendRoute(kn)

		Expect(route).NotTo(BeNil())
		Expect(route.Annotations).To(HaveKeyWithValue("haproxy.router.openshift.io/timeout", routeSSETimeout),
			"AF route should have HAProxy timeout annotation for SSE streams")
	})
})

var _ = Describe("APIFrontendRouteStub", func() {
	It("has minimal metadata for deletion lookup", func() {
		kn := testKubernaut()
		stub := APIFrontendRouteStub(kn)

		Expect(stub.Name).To(Equal("apifrontend-route"))
		Expect(stub.Namespace).To(Equal(kn.Namespace))
	})
})

var _ = Describe("DataStorageDBSecret", func() {
	It("derives from the PostgreSQL secret", func() {
		kn := testKubernaut()
		pgSecret := &corev1.Secret{
			Data: map[string][]byte{
				"POSTGRES_USER":     []byte("kubernaut"),
				"POSTGRES_PASSWORD": []byte("s3cret"),
				"POSTGRES_DB":       []byte("action_history"),
			},
		}

		dsSecret, err := DataStorageDBSecret(kn, pgSecret)
		Expect(err).NotTo(HaveOccurred())

		Expect(dsSecret.Name).To(Equal("datastorage-db-secret"), "name = %q, want %q", dsSecret.Name, "datastorage-db-secret")

		yamlContent := string(dsSecret.Data["db-secrets.yaml"])
		Expect(yamlContent).To(ContainSubstring("host: pg.example.com"), "db-secrets.yaml should contain PG host, got:\n%s", yamlContent)
		Expect(yamlContent).To(ContainSubstring("port: 5432"), "db-secrets.yaml should contain PG port, got:\n%s", yamlContent)
		Expect(yamlContent).To(ContainSubstring("dbname: action_history"), "db-secrets.yaml should contain dbname, got:\n%s", yamlContent)
		Expect(yamlContent).To(ContainSubstring("user: kubernaut"), "db-secrets.yaml should contain user, got:\n%s", yamlContent)
		Expect(yamlContent).To(ContainSubstring("password: s3cret"), "db-secrets.yaml should contain password, got:\n%s", yamlContent)
	})

	It("defaults the port to 5432", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.Port = 0
		pgSecret := &corev1.Secret{
			Data: map[string][]byte{
				"POSTGRES_USER":     []byte("u"),
				"POSTGRES_PASSWORD": []byte("p"),
				"POSTGRES_DB":       []byte("d"),
			},
		}

		dsSecret, err := DataStorageDBSecret(kn, pgSecret)
		Expect(err).NotTo(HaveOccurred())
		yamlContent := string(dsSecret.Data["db-secrets.yaml"])
		Expect(yamlContent).To(ContainSubstring("port: 5432"), "should default to port 5432, got:\n%s", yamlContent)
	})

	It("returns an error when required keys are missing", func() {
		kn := testKubernaut()
		pgSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pg-secret"},
			Data: map[string][]byte{
				"POSTGRES_USER": []byte("u"),
			},
		}

		_, err := DataStorageDBSecret(kn, pgSecret)
		Expect(err).To(HaveOccurred(), "DataStorageDBSecret should return error when required keys are missing")
	})
})

var _ = Describe("GatewayAlertManagerConfig", func() {
	It("creates an AlertmanagerConfig when monitoring is enabled", func() {
		kn := testKubernaut()
		amcfg := GatewayAlertManagerConfig(kn)

		Expect(amcfg).NotTo(BeNil(), "AlertmanagerConfig should not be nil when monitoring is enabled")
		Expect(amcfg.Name).To(Equal("kubernaut-gateway-alerts"))
		Expect(amcfg.Namespace).To(Equal(testSystemNamespace))

		Expect(amcfg.Spec.Route).NotTo(BeNil())
		Expect(amcfg.Spec.Route.Receiver).To(Equal("gateway-webhook"))
		Expect(amcfg.Spec.Route.GroupBy).To(ContainElements("alertname", "namespace"))
		Expect(amcfg.Spec.Route.GroupWait).To(Equal(nonEmptyDurationPtr(monitoringv1.NonEmptyDuration("5s"))))
		Expect(amcfg.Spec.Route.GroupInterval).To(Equal(nonEmptyDurationPtr(monitoringv1.NonEmptyDuration("5s"))))

		Expect(amcfg.Spec.Receivers).To(HaveLen(1))
		recv := amcfg.Spec.Receivers[0]
		Expect(recv.Name).To(Equal("gateway-webhook"))
		Expect(recv.WebhookConfigs).To(HaveLen(1))

		wh := recv.WebhookConfigs[0]
		Expect(*wh.URL).To(ContainSubstring("gateway-service"))
		Expect(*wh.URL).To(ContainSubstring("/api/v1/signals/prometheus"))
		Expect(*wh.URL).To(HavePrefix("https://"))
		Expect(*wh.SendResolved).To(BeFalse())

		Expect(wh.HTTPConfig).NotTo(BeNil())
		Expect(wh.HTTPConfig.Authorization).NotTo(BeNil())
		Expect(wh.HTTPConfig.Authorization.Type).To(Equal("Bearer"))
		Expect(wh.HTTPConfig.Authorization.Credentials.Name).To(Equal("alertmanager-gateway-token"))
		Expect(wh.HTTPConfig.Authorization.Credentials.Key).To(Equal("token"))
		Expect(wh.HTTPConfig.TLSConfig).NotTo(BeNil())
		Expect(wh.HTTPConfig.TLSConfig.CA.ConfigMap).NotTo(BeNil(),
			"TLS CA should reference the inter-service CA ConfigMap")
		Expect(wh.HTTPConfig.TLSConfig.CA.ConfigMap.Name).To(Equal(InterServiceCAConfigMapName))
		Expect(wh.HTTPConfig.TLSConfig.CA.ConfigMap.Key).To(Equal("service-ca.crt"))
	})

	It("returns nil when monitoring is disabled", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.Monitoring.Enabled = &disabled
		amcfg := GatewayAlertManagerConfig(kn)

		Expect(amcfg).To(BeNil(), "AlertmanagerConfig should be nil when monitoring is disabled")
	})
})

var _ = Describe("GatewayAlertManagerTokenSecret", func() {
	It("creates a token Secret when monitoring is enabled", func() {
		kn := testKubernaut()
		secret := GatewayAlertManagerTokenSecret(kn)

		Expect(secret).NotTo(BeNil())
		Expect(secret.Name).To(Equal("alertmanager-gateway-token"))
		Expect(secret.Namespace).To(Equal(testSystemNamespace))
		Expect(secret.Type).To(Equal(corev1.SecretTypeOpaque))
	})

	It("returns nil when monitoring is disabled", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.Monitoring.Enabled = &disabled
		secret := GatewayAlertManagerTokenSecret(kn)

		Expect(secret).To(BeNil())
	})
})

var _ = Describe("WorkflowNamespace", func() {
	It("uses the default name and labels", func() {
		kn := testKubernaut()
		ns := WorkflowNamespace(kn)

		Expect(ns.Name).To(Equal(DefaultWorkflowNamespace), "name = %q, want %q", ns.Name, DefaultWorkflowNamespace)
		Expect(ns.Labels["app.kubernetes.io/managed-by"]).To(Equal(testOperatorManagedByValue), "workflow namespace should have managed-by label")
	})

	It("uses a custom name from the spec", func() {
		kn := testKubernaut()
		kn.Spec.WorkflowExecution.WorkflowNamespace = "my-workflows"
		ns := WorkflowNamespace(kn)

		Expect(ns.Name).To(Equal("my-workflows"), "name = %q, want %q", ns.Name, "my-workflows")
	})
})
