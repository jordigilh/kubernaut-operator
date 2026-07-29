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

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"

	kubernautaiv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/controller"
	"github.com/jordigilh/kubernaut-operator/internal/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	// +kubebuilder:scaffold:imports
)

// Populated at build time via -ldflags.
var (
	Version   = "dev"
	GitCommit = "unknown"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(configv1.Install(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(kubernautaiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	f := parseFlags()

	metricsServerOptions, metricsCertWatcher, err := buildMetricsServerOptions(f)
	if err != nil {
		setupLog.Error(err, "failed to initialize metrics certificate watcher")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: f.probeAddr,
		LeaderElection:         f.enableLeaderElection,
		LeaderElectionID:       "kubernaut-operator.kubernaut.ai",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.KubernautReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		RestCfg: mgr.GetConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Kubernaut")
		os.Exit(1)
	}
	registerSingletonWebhook(mgr)
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := setupHealthChecks(mgr); err != nil {
		setupLog.Error(err, "unable to set up health checks")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "version", Version, "commit", GitCommit)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// operatorFlags holds the operator's command-line flags, parsed once in
// parseFlags and threaded through manager setup.
type operatorFlags struct {
	metricsAddr          string
	probeAddr            string
	metricsCertPath      string
	metricsCertName      string
	metricsCertKey       string
	enableLeaderElection bool
	secureMetrics        bool
	enableHTTP2          bool
}

// parseFlags registers and parses the operator's command-line flags and zap
// logging options, then configures the global controller-runtime logger.
func parseFlags() *operatorFlags {
	f := &operatorFlags{}
	flag.StringVar(&f.metricsAddr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable.")
	flag.StringVar(&f.probeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	flag.BoolVar(&f.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager.")
	flag.BoolVar(&f.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS.")
	flag.StringVar(&f.metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&f.metricsCertName, "metrics-cert-name", "tls.crt",
		"The name of the metrics server certificate file.")
	flag.StringVar(&f.metricsCertKey, "metrics-cert-key", "tls.key",
		"The name of the metrics server key file.")
	flag.BoolVar(&f.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	return f
}

// buildMetricsServerOptions assembles the controller-runtime metrics server
// options from f, initializing a certificate watcher when a cert path is
// configured. The returned watcher, when non-nil, must be registered with
// the manager via mgr.Add so it starts/stops with the manager's lifecycle.
func buildMetricsServerOptions(f *operatorFlags) (metricsserver.Options, *certwatcher.CertWatcher, error) {
	var tlsOpts []func(*tls.Config)
	if !f.enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	opts := metricsserver.Options{
		BindAddress:   f.metricsAddr,
		SecureServing: f.secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if f.secureMetrics {
		opts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(f.metricsCertPath) == 0 {
		return opts, nil, nil
	}
	watcher, err := setupMetricsCertWatcher(f.metricsCertPath, f.metricsCertName, f.metricsCertKey, &opts)
	return opts, watcher, err
}

// setupMetricsCertWatcher initializes a certificate watcher for the metrics
// server and wires its GetCertificate callback into opts.TLSOpts.
func setupMetricsCertWatcher(certPath, certName, certKey string,
	opts *metricsserver.Options) (*certwatcher.CertWatcher, error) {
	setupLog.Info("Initializing metrics certificate watcher", "metrics-cert-path", certPath)
	watcher, err := certwatcher.New(
		filepath.Join(certPath, certName),
		filepath.Join(certPath, certKey),
	)
	if err != nil {
		return nil, err
	}
	opts.TLSOpts = append(opts.TLSOpts, func(config *tls.Config) {
		config.GetCertificate = watcher.GetCertificate
	})
	return watcher, nil
}

// registerSingletonWebhook registers the Kubernaut singleton validating
// webhook when webhook serving certs are present on disk. In environments
// without cert-manager-provisioned certs (e.g. some local/dev setups), the
// webhook is skipped so the manager can still start.
func registerSingletonWebhook(mgr ctrl.Manager) {
	webhookCertDir := filepath.Join(os.TempDir(), "k8s-webhook-server", "serving-certs")
	if _, err := os.Stat(filepath.Join(webhookCertDir, "tls.crt")); err != nil {
		setupLog.Info("webhook TLS certs not found, skipping singleton webhook registration")
		return
	}
	setupLog.Info("webhook TLS certs found, registering singleton validating webhook")
	mgr.GetWebhookServer().Register("/validate-kubernaut-singleton", &admission.Webhook{
		Handler: &webhook.SingletonValidator{
			Client: mgr.GetClient(),
		},
	})
}

// setupHealthChecks registers the liveness and readiness probes on mgr.
func setupHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up ready check: %w", err)
	}
	return nil
}
