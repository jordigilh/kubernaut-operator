package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/version"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/cert"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBootstrap(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	if os.Getenv("KUBERNAUT_BOOTSTRAP_TEST_TIER") == "integration" {
		suiteConfig.LabelFilter = "integration"
	}
	RunSpecs(t, "Bootstrap security suite", suiteConfig, reporterConfig)
}

var _ = Describe("bootstrap protocol security", Label("unit"), func() {
	It("UT-AUDIT-515-BOOTSTRAP-001: parses secure defaults and explicit flags", func() {
		originalFlags, originalArgs := flag.CommandLine, os.Args
		DeferCleanup(func() { flag.CommandLine, os.Args = originalFlags, originalArgs })
		for _, explicit := range []bool{false, true} {
			flag.CommandLine = flag.NewFlagSet("bootstrap-unit", flag.ContinueOnError)
			os.Args = []string{"bootstrap-unit"}
			if explicit {
				os.Args = append(os.Args, "--enable-http2", "--metrics-bind-address=:8443",
					"--leader-elect", "--metrics-cert-path=certificates")
			}
			flags := parseFlags()
			Expect(flags.secureMetrics).To(BeTrue())
			Expect(flags.enableHTTP2).To(Equal(explicit))
			Expect(flags.enableLeaderElection).To(Equal(explicit))
			Expect(flags.probeAddr).To(Equal(":8081"))
			if explicit {
				Expect(flags.metricsAddr).To(Equal(":8443"))
				Expect(flags.metricsCertPath).To(Equal("certificates"))
			} else {
				Expect(flags.metricsAddr).To(Equal("0"))
			}
		}
	})

	It("UT-AUDIT-515-BOOTSTRAP-002: wires a valid certificate watcher and returns missing-certificate errors", func() {
		directory, err := os.MkdirTemp("", "bootstrap-unit-cert-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(os.RemoveAll(directory)).To(Succeed()) })
		flags := &operatorFlags{
			secureMetrics: true, metricsCertPath: directory, metricsCertName: "tls.crt", metricsCertKey: "tls.key",
		}
		_, watcher, err := buildMetricsServerOptions(flags)
		Expect(err).To(HaveOccurred())
		Expect(watcher).To(BeNil())
		certificate, key, err := cert.GenerateSelfSignedCertKey("127.0.0.1", []net.IP{net.ParseIP("127.0.0.1")}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(directory, "tls.crt"), certificate, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(directory, "tls.key"), key, 0o600)).To(Succeed())
		options, watcher, err := buildMetricsServerOptions(flags)
		Expect(err).NotTo(HaveOccurred())
		Expect(watcher).NotTo(BeNil())
		Expect(options.FilterProvider).NotTo(BeNil())
		config := &tls.Config{}
		for _, apply := range options.TLSOpts {
			apply(config)
		}
		Expect(config.GetCertificate).NotTo(BeNil())
		pair, err := config.GetCertificate(&tls.ClientHelloInfo{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pair.Certificate).NotTo(BeEmpty())
	})

	It("UT-AUDIT-515-BOOTSTRAP-003: surfaces health registration failures and registers both checks", func() {
		for _, failure := range []string{"healthz", "readyz", ""} {
			manager := &bootstrapHealthManager{failure: failure}
			err := setupHealthChecks(manager)
			if failure == "" {
				Expect(err).NotTo(HaveOccurred())
				Expect(manager.registered).To(Equal([]string{"healthz", "readyz"}))
			} else {
				Expect(err).To(MatchError(ContainSubstring("setting up")))
			}
		}
	})

	It("UT-AUDIT-515-BOOTSTRAP-004: conditionally registers a working singleton admission handler", func() {
		directory, err := os.MkdirTemp("", "bootstrap-webhook-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(os.RemoveAll(directory)).To(Succeed()) })
		oldTemp, existed := os.LookupEnv("TMPDIR")
		Expect(os.Setenv("TMPDIR", directory)).To(Succeed())
		DeferCleanup(func() {
			if existed {
				Expect(os.Setenv("TMPDIR", oldTemp)).To(Succeed())
			} else {
				Expect(os.Unsetenv("TMPDIR")).To(Succeed())
			}
		})
		server := webhookserver.NewServer(webhookserver.Options{})
		manager := &bootstrapWebhookManager{server: server, reader: fake.NewClientBuilder().WithScheme(scheme).Build()}
		registerSingletonWebhook(manager)
		certDirectory := filepath.Join(directory, "k8s-webhook-server", "serving-certs")
		Expect(os.MkdirAll(certDirectory, 0o700)).To(Succeed())
		certificate := []byte("existence-only fixture; server is not started")
		Expect(os.WriteFile(filepath.Join(certDirectory, "tls.crt"), certificate, 0o600)).To(Succeed())
		registerSingletonWebhook(manager)
		review := admissionv1.AdmissionReview{Request: &admissionv1.AdmissionRequest{
			UID: "bootstrap-request", Operation: admissionv1.Create,
			Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"kubernaut.ai/v1alpha2","kind":"Kubernaut",` +
				`"metadata":{"name":"kubernaut"}}`)},
		}}
		review.APIVersion, review.Kind = "admission.k8s.io/v1", "AdmissionReview"
		body, err := json.Marshal(review)
		Expect(err).NotTo(HaveOccurred())
		request := httptest.NewRequest("POST", "/validate-kubernaut-singleton", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.WebhookMux().ServeHTTP(response, request)
		var result admissionv1.AdmissionReview
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		Expect(result.Response).NotTo(BeNil())
		Expect(result.Response.Allowed).To(BeTrue())
		Expect(result.Response.UID).To(Equal(review.Request.UID))
	})
	It("UT-AUDIT-515-TOOLCHAIN-001: requires the audited standard-library security-fix floor", func() {
		module, err := os.ReadFile("../go.mod")
		Expect(err).NotTo(HaveOccurred())
		declared := ""
		for _, line := range strings.Split(string(module), "\n") {
			if strings.HasPrefix(line, "go ") {
				declared = "go" + strings.TrimPrefix(line, "go ")
			}
		}
		Expect(version.IsValid(declared)).To(BeTrue())
		Expect(version.Compare(declared, "go1.26.9")).To(BeNumerically(">=", 0))
	})
	It("UT-AUDIT-515-TLS-001: disables HTTP/2 in metrics options by default and honors explicit opt-in", func() {
		for _, enabled := range []bool{false, true} {
			options, watcher, err := buildMetricsServerOptions(&operatorFlags{enableHTTP2: enabled})
			Expect(err).NotTo(HaveOccurred())
			Expect(watcher).To(BeNil())
			config := &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
			for _, apply := range options.TLSOpts {
				apply(config)
			}
			if enabled {
				Expect(config.NextProtos).To(ContainElement("h2"))
			} else {
				Expect(config.NextProtos).To(Equal([]string{"http/1.1"}))
			}
		}
	})

	It("UT-AUDIT-515-TLS-002: supplies an explicit webhook server in the actual manager construction", func() {
		file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
		Expect(err).NotTo(HaveOccurred())
		wired := false
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := field.Key.(*ast.Ident)
			if ok && key.Name == "WebhookServer" {
				wired = true
			}
			return true
		})
		Expect(wired).To(BeTrue(), "metrics TLS options alone do not configure the manager's default webhook server")
	})

	It("UT-AUDIT-515-TLS-003: uses the tested manager options builder at the process entry point", func() {
		file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
		Expect(err).NotTo(HaveOccurred())
		wired := false
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "NewManager" {
				return true
			}
			options, ok := call.Args[1].(*ast.CallExpr)
			if !ok {
				return true
			}
			builder, ok := options.Fun.(*ast.Ident)
			wired = ok && builder.Name == "buildManagerOptions"
			return true
		})
		Expect(wired).To(BeTrue())
	})

	It("UT-AUDIT-515-TLS-004: threads explicit flags to the actual manager's webhook and metrics servers", func() {
		for _, enabled := range []bool{false, true} {
			flags := &operatorFlags{probeAddr: ":12345", enableHTTP2: enabled, enableLeaderElection: true}
			metrics := metricsserver.Options{BindAddress: "0"}
			options := buildManagerOptions(flags, metrics)
			Expect(options.Scheme).To(BeIdenticalTo(scheme))
			Expect(options.LeaderElection).To(BeTrue())
			Expect(options.HealthProbeBindAddress).To(Equal(":12345"))
			Expect(options.Metrics.BindAddress).To(Equal("0"))
			server, ok := options.WebhookServer.(*webhookserver.DefaultServer)
			Expect(ok).To(BeTrue())
			config := &tls.Config{NextProtos: []string{"h2"}}
			for _, apply := range server.Options.TLSOpts {
				apply(config)
			}
			Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			if enabled {
				Expect(config.NextProtos).To(Equal([]string{"h2"}))
			} else {
				Expect(config.NextProtos).To(Equal([]string{"http/1.1"}))
			}
		}
	})
})

type bootstrapHealthManager struct {
	ctrl.Manager
	failure    string
	registered []string
}

func (m *bootstrapHealthManager) AddHealthzCheck(name string, _ healthz.Checker) error {
	if m.failure == name {
		return os.ErrPermission
	}
	m.registered = append(m.registered, name)
	return nil
}

func (m *bootstrapHealthManager) AddReadyzCheck(name string, checker healthz.Checker) error {
	return m.AddHealthzCheck(name, checker)
}

type bootstrapWebhookManager struct {
	ctrl.Manager
	server webhookserver.Server
	reader client.Reader
}

func (m *bootstrapWebhookManager) GetWebhookServer() webhookserver.Server { return m.server }
func (m *bootstrapWebhookManager) GetAPIReader() client.Reader            { return m.reader }
func (*bootstrapWebhookManager) GetScheme() *runtime.Scheme               { return scheme }
