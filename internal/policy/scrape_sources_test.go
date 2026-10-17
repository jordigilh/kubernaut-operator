package policy

import (
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("bounded external metrics scraper authorization", func() {
	It("UT-AUDIT-515-SCRAPE-001: adds only source-qualified TCP metrics ingress for Cilium and Calico", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"datastorage", "authwebhook"}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal([]byte(`{"ScrapeSources":[{"namespace":"monitoring","podLabels":{"app":"prometheus"}}],"MetricsPorts":{"datastorage":9090}}`), &intent)).To(Succeed())
		for _, provider := range []Provider{ProviderCilium, ProviderCalico} {
			objects, renderErr := Render(DetectionResult{Provider: provider, Ready: true}, intent)
			Expect(renderErr).NotTo(HaveOccurred())
			for _, object := range objects {
				ingress, found, readErr := unstructured.NestedSlice(object.Object.Object, "spec", "ingress")
				Expect(readErr).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				if object.Object.GetName() == "kubernaut-authwebhook" {
					Expect(ingress).To(HaveLen(1), "AuthWebhook has no supported metrics endpoint")
					continue
				}
				Expect(ingress).To(HaveLen(2), "an ordinary external Prometheus scraper needs positive ingress authorization")
				rule := ingress[1].(map[string]interface{})
				if provider == ProviderCilium {
					from := rule["fromEndpoints"].([]interface{})[0].(map[string]interface{})
					Expect(from["matchLabels"]).To(Equal(map[string]interface{}{
						"k8s:io.kubernetes.pod.namespace": "monitoring", "k8s:app": "prometheus",
					}))
					ports := rule["toPorts"].([]interface{})[0].(map[string]interface{})["ports"]
					Expect(ports).To(Equal([]interface{}{map[string]interface{}{"port": "9090", "protocol": "TCP"}}))
				} else {
					Expect(rule).To(HaveKeyWithValue("protocol", "TCP"))
					Expect(rule["source"]).To(HaveKeyWithValue("namespaceSelector", "projectcalico.org/name == 'monitoring'"))
					Expect(rule["source"]).To(HaveKeyWithValue("selector", "app == 'prometheus'"))
					Expect(rule["destination"]).To(HaveKeyWithValue("ports", []interface{}{int64(9090)}))
				}
			}
		}
	})

	It("UT-AUDIT-515-SCRAPE-002: rejects empty or malformed scraper identities rather than granting namespace-wide access", func() {
		for _, source := range []kubernautv1alpha2.MetricsScrapeSource{
			{Namespace: "monitoring", PodLabels: nil},
			{Namespace: "*", PodLabels: map[string]string{"app": "prometheus"}},
			{Namespace: "monitoring", PodLabels: map[string]string{"all()": "x"}},
			{Namespace: "monitoring", PodLabels: map[string]string{"app": "x' || all()"}},
			{Namespace: "monitoring", PodLabels: map[string]string{"io.kubernetes.pod.namespace": "other"}},
		} {
			Expect(ValidateNativeOverrides(kubernautv1alpha2.NetworkPoliciesSpec{ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{source}})).To(HaveOccurred())
		}
	})

	It("UT-AUDIT-515-SCRAPE-003: leaves all external scraper ingress absent when the allowlist is empty", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"datastorage"}, nil)
		Expect(err).NotTo(HaveOccurred())
		for _, provider := range []Provider{ProviderCilium, ProviderCalico} {
			objects, renderErr := Render(DetectionResult{Provider: provider, Ready: true}, intent)
			Expect(renderErr).NotTo(HaveOccurred())
			ingress, _, readErr := unstructured.NestedSlice(objects[0].Object.Object, "spec", "ingress")
			Expect(readErr).NotTo(HaveOccurred())
			Expect(ingress).To(HaveLen(1))
		}
	})

	It("UT-AUDIT-515-SCRAPE-004: scopes OVN scraper policies to each metrics component and its own port", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"authwebhook", "datastorage", "apifrontend"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.DNS = ovnDNSDestinationForTest()
		Expect(json.Unmarshal([]byte(`{"ScrapeSources":[{"namespace":"monitoring","podLabels":{"app":"prometheus"}}],"MetricsPorts":{"datastorage":9090,"apifrontend":9180}}`), &intent)).To(Succeed())
		objects, err := Render(DetectionResult{Provider: ProviderOVN, Ready: true}, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(4), "the common policies and two component-scoped scraper policies must be present")
		for _, object := range objects[2:] {
			spec := object.Object.Object["spec"].(map[string]interface{})
			subject := spec["subject"].(map[string]interface{})["pods"].(map[string]interface{})
			labels := subject["podSelector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
			component := labels["app"].(string)
			Expect(component).To(BeElementOf("datastorage", "apifrontend"))
			rules := spec["ingress"].([]interface{})
			Expect(rules).To(HaveLen(1))
			allow := rules[0].(map[string]interface{})
			Expect(allow).To(HaveKeyWithValue("action", "Allow"))
			Expect(allow["from"]).To(Equal([]interface{}{map[string]interface{}{"pods": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{
					"kubernetes.io/metadata.name": "monitoring",
				}},
				"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "prometheus"}},
			}}}))
			port := map[string]int32{"datastorage": 9090, "apifrontend": 9180}[component]
			Expect(allow["ports"]).To(Equal([]interface{}{map[string]interface{}{"portNumber": map[string]interface{}{
				"port": float64(port), "protocol": "TCP",
			}}}))
		}
	})

	It("UT-AUDIT-515-SCRAPE-005: validates bounded limits and rejects unsupported mode/provider contradictions", func() {
		source := kubernautv1alpha2.MetricsScrapeSource{Namespace: "monitoring", PodLabels: map[string]string{"app": "prometheus"}}
		for _, mode := range []string{"invalid", "Inherited"} {
			Expect(ValidateNativeOverrides(kubernautv1alpha2.NetworkPoliciesSpec{Mode: mode,
				ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{source}})).To(HaveOccurred())
		}
		Expect(ValidateNativeOverrides(kubernautv1alpha2.NetworkPoliciesSpec{
			Mode: "Inherited", Provider: kubernautv1alpha2.NetworkPolicyProviderCilium,
		})).To(MatchError(ContainSubstring("unsupported")))
		Expect(ValidateNativeOverrides(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: make([]kubernautv1alpha2.MetricsScrapeSource, 17),
		})).To(MatchError(ContainSubstring("at most 16")))
		intent, err := BuildIntent("kubernaut-system", []string{"datastorage"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring"}}
		_, err = Render(DetectionResult{Provider: ProviderCilium, Ready: true}, intent)
		Expect(err).To(HaveOccurred())
		intent.ScrapeSources = nil
		intent.MetricsPorts = map[string]int32{"datastorage": 65536}
		_, err = Render(DetectionResult{Provider: ProviderCilium, Ready: true}, intent)
		Expect(err).To(MatchError(ContainSubstring("invalid metrics port")))
	})

	It("UT-AUDIT-515-SCRAPE-006: accepts the exact bounded limits and rejects excess labels, reserved namespaces and invalid port boundaries", func() {
		labels := make(map[string]string, 17)
		for index := range 16 {
			labels[fmt.Sprintf("key-%02d", index)] = "prometheus"
		}
		sources := make([]kubernautv1alpha2.MetricsScrapeSource, 16)
		for index := range sources {
			sources[index] = kubernautv1alpha2.MetricsScrapeSource{Namespace: fmt.Sprintf("monitoring-%02d", index), PodLabels: labels}
		}
		Expect(validateScrapeSources(sources)).To(Succeed())
		labels["key-16"] = "prometheus"
		Expect(validateScrapeSources(sources)).To(MatchError(ContainSubstring("1 to 16 exact pod labels")))
		Expect(validateScrapeSources([]kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring",
			PodLabels: map[string]string{"io.cilium.k8s.namespace.labels.team": "platform"},
		}})).To(MatchError(ContainSubstring("reserved namespace identity")))
		intent, err := BuildIntent("kubernaut-system", []string{"apifrontend"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.DNS = ovnDNSDestinationForTest()
		intent.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring", PodLabels: map[string]string{"app": "prometheus"}}}
		for _, provider := range []Provider{ProviderCilium, ProviderCalico, ProviderOVN} {
			for _, port := range []int32{-1, 0, 65536} {
				intent.MetricsPorts = map[string]int32{"apifrontend": port}
				objects, renderErr := Render(DetectionResult{Provider: provider, Ready: true}, intent)
				Expect(objects).To(BeEmpty())
				Expect(renderErr).To(MatchError(ContainSubstring("invalid metrics port")))
			}
			for _, port := range []int32{1, 65535} {
				intent.MetricsPorts = map[string]int32{"apifrontend": port}
				objects, renderErr := Render(DetectionResult{Provider: provider, Ready: true}, intent)
				Expect(renderErr).NotTo(HaveOccurred())
				Expect(objects).NotTo(BeEmpty())
			}
		}
	})
})
