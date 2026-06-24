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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const (
	testSystemNamespace = "kubernaut-system"
	testIngressDomain   = "apps.test.example.com"
)

func testKubernaut() *kubernautv1alpha1.Kubernaut {
	return &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernautv1alpha1.SingletonName,
			Namespace: testSystemNamespace,
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			Image: kubernautv1alpha1.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
			},
			PostgreSQL: kubernautv1alpha1.PostgreSQLSpec{
				SecretName: "postgresql-secret",
				Host:       "pg.example.com",
				Port:       5432,
			},
			Valkey: kubernautv1alpha1.ValkeySpec{
				SecretName: "valkey-secret",
				Host:       "valkey.example.com",
				Port:       6379,
			},
			APIFrontend: kubernautv1alpha1.APIFrontendSpec{
				Auth: kubernautv1alpha1.APIFrontendAuthSpec{
					IssuerURL: "https://login.kubernaut.ai/realms/kubernaut",
					Audience:  "kubernaut-apifrontend",
				},
			},
			KubernautAgent: kubernautv1alpha1.KubernautAgentSpec{
				LLM: kubernautv1alpha1.LLMSpec{
					Provider:              "openai",
					Model:                 "gpt-4o",
					Endpoint:              "http://llm-gateway:8080",
					CredentialsSecretName: "llm-creds",
				},
			},
			AIAnalysis: kubernautv1alpha1.AIAnalysisSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "aianalysis-policies"},
			},
			SignalProcessing: kubernautv1alpha1.SignalProcessingSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
		},
	}
}

func testKubernautWithAF() *kubernautv1alpha1.Kubernaut {
	kn := testKubernaut()
	kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
		Auth: kubernautv1alpha1.APIFrontendAuthSpec{
			IssuerURL: "https://login.kubernaut.ai/realms/kubernaut",
			Audience:  "kubernaut-apifrontend",
		},
	}
	return kn
}

func testKubernautWithValkeyTLS() *kubernautv1alpha1.Kubernaut {
	kn := testKubernaut()
	kn.Spec.Valkey.TLS = &kubernautv1alpha1.ValkeyTLSSpec{
		Enabled:              true,
		CASecretName:         "valkey-ca",
		ClientCertSecretName: "valkey-client-cert",
	}
	return kn
}

var _ = Describe("ResolveImage", func() {
	It("resolves from env var", func() {
		kn := testKubernaut()
		got, err := ResolveImage(kn, "gateway")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("quay.io/kubernaut-ai/gateway:v1.3.0"))
	})

	It("prefers override over env var", func() {
		kn := testKubernaut()
		kn.Spec.Image.Overrides = map[string]string{
			"gateway": "myregistry.internal/custom-gateway:v2.0.0",
		}
		got, err := ResolveImage(kn, "gateway")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("myregistry.internal/custom-gateway:v2.0.0"))
	})

	It("returns error when env var is empty and no override", func() {
		original := os.Getenv("RELATED_IMAGE_GATEWAY")
		Expect(os.Setenv("RELATED_IMAGE_GATEWAY", "")).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Setenv("RELATED_IMAGE_GATEWAY", original)).To(Succeed())
		})

		kn := testKubernaut()
		_, err := ResolveImage(kn, "gateway")
		Expect(err).To(HaveOccurred())
	})

	It("resolves all components", func() {
		kn := testKubernaut()
		components := []string{
			"gateway", "datastorage", "aianalysis", "signalprocessing",
			"remediationorchestrator", "workflowexecution", "effectivenessmonitor",
			"notification", "kubernautagent", "authwebhook",
		}
		for _, c := range components {
			_, err := ResolveImage(kn, c)
			Expect(err).NotTo(HaveOccurred(), "ResolveImage(%q) unexpected error", c)
		}
	})

	It("resolves db-migrate image", func() {
		kn := testKubernaut()
		got, err := ResolveImage(kn, "db-migrate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("quay.io/kubernaut-ai/db-migrate:v1.3.0"))
	})
})

var _ = Describe("Labels", func() {
	It("CommonLabels includes managed-by, part-of, and instance", func() {
		kn := testKubernaut()
		labels := CommonLabels(kn)

		Expect(labels["app.kubernetes.io/managed-by"]).To(Equal("kubernaut-operator"))
		Expect(labels["app.kubernetes.io/part-of"]).To(Equal("kubernaut"))
		Expect(labels["app.kubernetes.io/instance"]).To(Equal("kubernaut"))
	})

	It("ComponentLabels includes app and component labels plus common labels", func() {
		kn := testKubernaut()
		labels := ComponentLabels(kn, ComponentGateway)

		Expect(labels["app"]).To(Equal(ComponentGateway))
		Expect(labels["app.kubernetes.io/component"]).To(Equal(ComponentGateway))
		Expect(labels).To(HaveKey("app.kubernetes.io/managed-by"))
	})

	It("SelectorLabels returns only the app label", func() {
		labels := SelectorLabels(ComponentGateway)
		Expect(labels).To(HaveLen(1))
		Expect(labels["app"]).To(Equal(ComponentGateway))
	})
})

var _ = Describe("ObjectMeta", func() {
	It("sets name, namespace, and labels", func() {
		kn := testKubernaut()
		om := ObjectMeta(kn, "gateway-config", ComponentGateway)

		Expect(om.Name).To(Equal("gateway-config"))
		Expect(om.Namespace).To(Equal(testSystemNamespace))
		Expect(om.Labels["app"]).To(Equal(ComponentGateway))
	})
})

var _ = Describe("URL helpers", func() {
	It("DataStorageURL returns correct HTTPS URL", func() {
		got := DataStorageURL(testSystemNamespace)
		Expect(got).To(Equal("https://data-storage-service.kubernaut-system.svc.cluster.local:8443"))
	})

	It("GatewayURL returns correct HTTPS URL", func() {
		got := GatewayURL(testSystemNamespace)
		Expect(got).To(Equal("https://gateway-service.kubernaut-system.svc.cluster.local:8443"))
	})
})

var _ = Describe("ActiveComponents", func() {
	It("includes gateway when enabled by default", func() {
		kn := testKubernaut()
		Expect(ActiveComponents(kn)).To(ContainElement(ComponentGateway))
	})

	It("excludes gateway when disabled", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.Gateway.Enabled = &disabled
		Expect(ActiveComponents(kn)).NotTo(ContainElement(ComponentGateway))
	})

	It("includes apifrontend when enabled by default", func() {
		kn := testKubernautWithAF()
		Expect(ActiveComponents(kn)).To(ContainElement(ComponentAPIFrontend))
	})

	It("excludes apifrontend when disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		Expect(ActiveComponents(kn)).NotTo(ContainElement(ComponentAPIFrontend))
	})
})

var _ = Describe("InterServiceTLSCAFile", func() {
	It("matches OCP service-ca path", func() {
		Expect(InterServiceTLSCAFile).To(Equal("/etc/tls-ca/service-ca.crt"))
	})
})

var _ = Describe("ValkeyAddr", func() {
	DescribeTable("formats host:port",
		func(spec kubernautv1alpha1.ValkeySpec, want string) {
			Expect(ValkeyAddr(&spec)).To(Equal(want))
		},
		Entry("explicit port", kubernautv1alpha1.ValkeySpec{Host: "valkey.local", Port: 6380}, "valkey.local:6380"),
		Entry("default port", kubernautv1alpha1.ValkeySpec{Host: "valkey.local"}, "valkey.local:6379"),
	)
})

var _ = Describe("ValidateHostname", func() {
	It("accepts valid hostnames", func() {
		for _, host := range []string{"pg.local", "192.168.1.1", "my-host.example.com", "[::1]"} {
			Expect(ValidateHostname(host)).To(Succeed(), "ValidateHostname(%q) should be valid", host)
		}
	})

	It("rejects invalid hostnames", func() {
		for _, host := range []string{"", "host;rm -rf /", "host user=admin", "a b"} {
			Expect(ValidateHostname(host)).To(HaveOccurred(), "ValidateHostname(%q) should be invalid", host)
		}
	})
})

var _ = Describe("Security contexts", func() {
	It("PodSecurityContext sets restricted profile", func() {
		psc := PodSecurityContext()
		Expect(psc.RunAsNonRoot).NotTo(BeNil())
		Expect(*psc.RunAsNonRoot).To(BeTrue())
		Expect(psc.SeccompProfile).NotTo(BeNil())
		Expect(psc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
	})

	It("ContainerSecurityContext sets restricted profile", func() {
		csc := ContainerSecurityContext()
		Expect(csc.AllowPrivilegeEscalation).NotTo(BeNil())
		Expect(*csc.AllowPrivilegeEscalation).To(BeFalse())
		Expect(csc.ReadOnlyRootFilesystem).NotTo(BeNil())
		Expect(*csc.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(csc.Capabilities).NotTo(BeNil())
		Expect(csc.Capabilities.Drop).NotTo(BeEmpty())
		Expect(csc.Capabilities.Drop[0]).To(Equal(corev1.Capability("ALL")))
	})
})

var _ = Describe("MergeResources", func() {
	It("uses defaults when user spec is empty", func() {
		res := MergeResources(corev1.ResourceRequirements{})
		Expect(res.Requests.Cpu().IsZero()).To(BeFalse())
	})

	It("uses user spec when provided", func() {
		cpu := resource.MustParse("100m")
		userSpec := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: cpu,
			},
		}
		res := MergeResources(userSpec)
		Expect(res.Requests.Cpu().String()).To(Equal("100m"))
	})
})

var _ = Describe("AllComponents", func() {
	It("returns 11 components", func() {
		Expect(AllComponents()).To(HaveLen(11))
	})
})

var _ = Describe("SetOwnerReference", func() {
	It("sets controller reference", func() {
		scheme := runtime.NewScheme()
		Expect(kubernautv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		kn := testKubernaut()
		kn.UID = types.UID("test-uid-1234")

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: kn.Namespace},
		}
		Expect(SetOwnerReference(kn, cm, scheme)).To(Succeed())

		refs := cm.GetOwnerReferences()
		Expect(refs).To(HaveLen(1))
		Expect(refs[0].Kind).To(Equal("Kubernaut"))
		Expect(refs[0].UID).To(Equal(kn.UID))
		Expect(refs[0].Controller).NotTo(BeNil())
		Expect(*refs[0].Controller).To(BeTrue())
	})
})

var _ = Describe("ServiceAccountName", func() {
	It("returns component name for unknown component", func() {
		Expect(ServiceAccountName("custom-thing")).To(Equal("custom-thing"))
	})
})

var _ = Describe("intPtrDefault", func() {
	It("returns value when pointer is non-nil", func() {
		val := 0
		Expect(intPtrDefault(&val, 42)).To(Equal(0))
	})

	It("returns default when pointer is nil", func() {
		Expect(intPtrDefault(nil, 42)).To(Equal(42))
	})
})
