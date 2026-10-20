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
	"math"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	maxJWKSURLLength   = 2048
	httpURLScheme      = "http"
	httpsURLScheme     = "https"
	idTokenPlaceholder = "{id_token}"
)

// ValidateKubernaut runs all CR-level validations and returns accumulated errors.
func ValidateKubernaut(kn *kubernautv1alpha2.Kubernaut) []error {
	errs := make([]error, 0, 8)
	errs = append(errs, validatePostgreSQLSSLMode(kn)...)
	errs = append(errs, validatePolicyPrerequisites(kn)...)
	errs = append(errs, validateLLMProfiles(kn)...)
	errs = append(errs, validateNotificationCredentials(kn)...)
	errs = append(errs, validateTelemetry(kn)...)
	errs = append(errs, validateMonitoringEndpoints(kn)...)
	errs = append(errs, validateMonitoringTLS(kn)...)
	errs = append(errs, validateAPIFrontend(kn)...)
	if err := validateConsoleProviderLogoutURL(kn); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, validateAlignmentCheck(kn)...)
	errs = append(errs, validateDryRun(kn)...)
	errs = append(errs, validateInteractive(kn)...)
	errs = append(errs, validateDataStorage(kn)...)
	return errs
}

func validateDataStorage(kn *kubernautv1alpha2.Kubernaut) []error {
	ds := kn.Spec.DataStorage
	errs := validatePositiveDuration("spec.dataStorage.endpointPropagationDelay", ds.EndpointPropagationDelay)
	errs = append(errs, validateDataStorageServer(ds.Server)...)
	errs = append(errs, validateDataStorageDatabase(ds.Database)...)
	errs = append(errs, validateDataStorageRedis(ds.Redis)...)
	errs = append(errs, validateDataStorageAudit(ds.Audit)...)
	return errs
}

func validateDataStorageServer(server *kubernautv1alpha2.DataStorageServerSpec) []error {
	if server == nil {
		return nil
	}
	errs := validatePositiveDuration("spec.dataStorage.server.readTimeout", server.ReadTimeout)
	errs = append(errs, validatePositiveDuration("spec.dataStorage.server.writeTimeout", server.WriteTimeout)...)
	errs = append(errs, validateNonNegativeInt("spec.dataStorage.server.maxConcurrentRequests", server.MaxConcurrentRequests)...)
	if server.RateLimit == nil {
		return errs
	}
	errs = append(errs, validateNonNegativeInt("spec.dataStorage.server.rateLimit.requestsPerSecond", server.RateLimit.RequestsPerSecond)...)
	return append(errs, validateNonNegativeInt("spec.dataStorage.server.rateLimit.burst", server.RateLimit.Burst)...)
}

func validateDataStorageDatabase(database *kubernautv1alpha2.DataStorageDatabaseSpec) []error {
	if database == nil {
		return nil
	}
	errs := validatePositiveDuration("spec.dataStorage.database.connMaxLifetime", database.ConnMaxLifetime)
	errs = append(errs, validatePositiveDuration("spec.dataStorage.database.connMaxIdleTime", database.ConnMaxIdleTime)...)
	errs = append(errs, validateNonNegativeInt("spec.dataStorage.database.maxOpenConns", database.MaxOpenConns)...)
	return append(errs, validateNonNegativeInt("spec.dataStorage.database.maxIdleConns", database.MaxIdleConns)...)
}

func validateDataStorageRedis(redis *kubernautv1alpha2.DataStorageRedisSpec) []error {
	if redis == nil {
		return nil
	}
	return validateNonNegativeInt("spec.dataStorage.redis.dlqMaxLen", redis.DLQMaxLen)
}

func validateDataStorageAudit(audit *kubernautv1alpha2.DataStorageAuditSpec) []error {
	if audit == nil || audit.Retention == nil {
		return nil
	}
	retention := audit.Retention
	errs := validatePositiveDuration("spec.dataStorage.audit.retention.interval", retention.Interval)
	errs = append(errs, validatePositiveInt("spec.dataStorage.audit.retention.batchSize", retention.BatchSize)...)
	errs = append(errs, validatePositiveInt("spec.dataStorage.audit.retention.defaultDays", retention.DefaultDays)...)
	return append(errs, validateMaximumInt("spec.dataStorage.audit.retention.defaultDays", retention.DefaultDays, 2555)...)
}

func validatePositiveDuration(path, value string) []error {
	if value == "" {
		return nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return []error{fmt.Errorf("%s: invalid duration %q: %w", path, value, err)}
	}
	if duration <= 0 {
		return []error{fmt.Errorf("%s: duration must be positive, got %q", path, value)}
	}
	return nil
}

func validateNonNegativeInt(path string, value *int) []error {
	if value == nil || *value >= 0 {
		return nil
	}
	return []error{fmt.Errorf("%s must be non-negative, got %d", path, *value)}
}

func validatePositiveInt(path string, value *int) []error {
	if value == nil || *value >= 1 {
		return nil
	}
	return []error{fmt.Errorf("%s must be positive, got %d", path, *value)}
}

func validateMaximumInt(path string, value *int, maximum int) []error {
	if value == nil || *value <= maximum {
		return nil
	}
	return []error{fmt.Errorf("%s must be at most %d, got %d", path, maximum, *value)}
}

// validateNotificationCredentials validates credential identities and namespace-local Secret references.
func validateNotificationCredentials(kn *kubernautv1alpha2.Kubernaut) []error {
	var errs []error
	seen := make(map[string]struct{}, len(kn.Spec.Notification.Credentials))
	for index, credential := range kn.Spec.Notification.Credentials {
		base := fmt.Sprintf("spec.notification.credentials[%d]", index)
		if len(k8svalidation.IsValidLabelValue(credential.Name)) > 0 {
			errs = append(errs, fmt.Errorf("%s.name must be a valid credential file name", base))
		}
		if _, exists := seen[credential.Name]; exists {
			errs = append(errs, fmt.Errorf("%s.name must be unique", base))
		}
		seen[credential.Name] = struct{}{}

		secretPath := base + ".secretRef"
		if len(k8svalidation.IsDNS1123Subdomain(credential.SecretRef.Name)) > 0 {
			errs = append(errs, fmt.Errorf("%s.name must be a valid Kubernetes object name", secretPath))
		}
		if len(k8svalidation.IsConfigMapKey(credential.SecretRef.Key)) > 0 {
			errs = append(errs, fmt.Errorf("%s.key must be a valid Secret key", secretPath))
		}
	}
	return errs
}

// validateMonitoringEndpoints rejects plaintext monitoring connections before
// any endpoint is rendered into a client configuration. A CA trust source
// cannot protect an HTTP connection, so every configured monitoring endpoint
// must use HTTPS; an omitted URL remains valid for capability-based defaults.
func validateMonitoringEndpoints(kn *kubernautv1alpha2.Kubernaut) []error {
	endpoints := []struct {
		path string
		url  string
	}{
		{path: "spec.monitoring.prometheus.url", url: kn.Spec.Monitoring.Prometheus.URL},
		{path: "spec.monitoring.alertManager.url", url: kn.Spec.Monitoring.AlertManager.URL},
	}
	var errs []error
	for _, endpoint := range endpoints {
		value := strings.TrimSpace(endpoint.url)
		if value == "" {
			continue
		}
		if value != endpoint.url {
			errs = append(errs, fmt.Errorf("%s must not contain leading or trailing whitespace", endpoint.path))
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != httpsURLScheme || parsed.Host == "" || parsed.User != nil {
			errs = append(errs, fmt.Errorf("%s must be an https URL without user information", endpoint.path))
		}
	}
	return errs
}

// validateMonitoringTLS validates the source-specific shape of the shared
// monitoring trust contract. Existence and PEM contents are checked later by
// the controller because those checks require a Kubernetes client.
func validateMonitoringTLS(kn *kubernautv1alpha2.Kubernaut) []error {
	tls := kn.Spec.Monitoring.TLS
	if tls == nil {
		return nil
	}

	const base = "spec.monitoring.tls"
	var errs []error
	switch tls.Source {
	case "", kubernautv1alpha2.MonitoringTLSSourceInterService, kubernautv1alpha2.MonitoringTLSSourceSystem:
		if tls.CACertSecretRef != nil {
			errs = append(errs, fmt.Errorf("%s.caCertSecretRef must be omitted when source is %q", base, defaultMonitoringTLSSource(tls.Source)))
		}
		if tls.CACertConfigMapRef != nil {
			errs = append(errs, fmt.Errorf("%s.caCertConfigMapRef must be omitted when source is %q", base, defaultMonitoringTLSSource(tls.Source)))
		}
	case kubernautv1alpha2.MonitoringTLSSourceSecret:
		if tls.CACertConfigMapRef != nil {
			errs = append(errs, fmt.Errorf("%s.caCertConfigMapRef must be omitted when source is %q", base, tls.Source))
		}
		if tls.CACertSecretRef == nil {
			errs = append(errs, fmt.Errorf("%s.caCertSecretRef is required when source is %q", base, tls.Source))
		} else {
			errs = append(errs, validateMonitoringCARef(base+".caCertSecretRef", tls.CACertSecretRef.Name, tls.CACertSecretRef.Key)...)
		}
	case kubernautv1alpha2.MonitoringTLSSourceConfigMap:
		if tls.CACertSecretRef != nil {
			errs = append(errs, fmt.Errorf("%s.caCertSecretRef must be omitted when source is %q", base, tls.Source))
		}
		if tls.CACertConfigMapRef == nil {
			errs = append(errs, fmt.Errorf("%s.caCertConfigMapRef is required when source is %q", base, tls.Source))
		} else {
			errs = append(errs, validateMonitoringCARef(base+".caCertConfigMapRef", tls.CACertConfigMapRef.Name, tls.CACertConfigMapRef.Key)...)
		}
	default:
		errs = append(errs, fmt.Errorf("%s.source %q is invalid", base, tls.Source))
	}
	return errs
}

func defaultMonitoringTLSSource(source kubernautv1alpha2.MonitoringTLSSource) kubernautv1alpha2.MonitoringTLSSource {
	if source == "" {
		return kubernautv1alpha2.MonitoringTLSSourceInterService
	}
	return source
}

func validateMonitoringCARef(base, name, key string) []error {
	var errs []error
	if name == "" {
		errs = append(errs, fmt.Errorf("%s.name must not be empty", base))
	} else if len(k8svalidation.IsDNS1123Subdomain(name)) > 0 {
		errs = append(errs, fmt.Errorf("%s.name must be a valid Kubernetes object name", base))
	}
	if key == "" {
		key = tlsCACertificateKey
	}
	if len(k8svalidation.IsConfigMapKey(key)) > 0 {
		errs = append(errs, fmt.Errorf("%s.key must be a valid ConfigMap or Secret key", base))
	}
	return errs
}

// validateTelemetry validates the shared OTLP contract for every telemetry
// producer. Gateway is optional, so its lane is checked only when the
// component is enabled; DataStorage and Kubernaut Agent are always deployed.
func validateTelemetry(kn *kubernautv1alpha2.Kubernaut) []error {
	var errs []error
	if kn.Spec.GatewayEnabled() {
		errs = append(errs, validateTelemetrySpec("spec.gateway.config.telemetry", kn.Spec.Gateway.Config.Telemetry)...)
	}
	errs = append(errs, validateTelemetrySpec("spec.dataStorage.telemetry", kn.Spec.DataStorage.Telemetry)...)
	errs = append(errs, validateTelemetrySpec("spec.kubernautAgent.telemetry", kn.Spec.KubernautAgent.Telemetry)...)
	return errs
}

func validateTelemetrySpec(base string, telemetry kubernautv1alpha2.TelemetrySpec) []error {
	var errs []error
	if err := validateTelemetryEndpoint(telemetry.Endpoint); err != nil {
		errs = append(errs, fmt.Errorf("%s.endpoint: %w", base, err))
	}
	errs = append(errs, validateTelemetryCA(base, telemetry.TLS)...)
	errs = append(errs, validateTelemetryClient(base, telemetry.TLS)...)
	return errs
}

func validateTelemetryCA(base string, tls kubernautv1alpha2.TelemetryTLSConfig) []error {
	var errs []error
	if tls.CAFile != "" && tls.CACertSecretRef != nil {
		errs = append(errs, fmt.Errorf("%s.tls.caFile and %s.tls.caCertSecretRef are mutually exclusive", base, base))
	}
	if tls.CAFile != "" && !filepath.IsAbs(tls.CAFile) {
		errs = append(errs, fmt.Errorf("%s.tls.caFile must be an absolute path", base))
	}
	if tls.CACertSecretRef != nil {
		if tls.CACertSecretRef.Name == "" {
			errs = append(errs, fmt.Errorf("%s.tls.caCertSecretRef.name must not be empty", base))
		}
		if key := tls.CACertSecretRef.Key; key != "" && (filepath.Base(key) != key || strings.ContainsAny(key, `\\/`)) {
			errs = append(errs, fmt.Errorf("%s.tls.caCertSecretRef.key must be a Secret key name without path separators", base))
		}
	}
	return errs
}

func validateTelemetryClient(base string, tls kubernautv1alpha2.TelemetryTLSConfig) []error {
	var errs []error
	certSet := tls.CertFile != "" || tls.KeyFile != ""
	if certSet && (tls.CertFile == "" || tls.KeyFile == "") {
		errs = append(errs, fmt.Errorf("%s.tls.certFile and %s.tls.keyFile must be set together", base, base))
	}
	if tls.TLSClientSecretRef != "" && !certSet {
		errs = append(errs, fmt.Errorf("%s.tls.tlsClientSecretRef requires both certFile and keyFile", base))
	}
	if !certSet {
		return errs
	}
	errs = append(errs, validateTelemetryClientPaths(base, tls)...)
	if tls.TLSClientSecretRef != "" {
		errs = append(errs, validateTelemetrySecretClientPaths(base, tls)...)
	}
	return errs
}

func validateTelemetryClientPaths(base string, tls kubernautv1alpha2.TelemetryTLSConfig) []error {
	var errs []error
	if !filepath.IsAbs(tls.CertFile) {
		errs = append(errs, fmt.Errorf("%s.tls.certFile must be an absolute path", base))
	}
	if !filepath.IsAbs(tls.KeyFile) {
		errs = append(errs, fmt.Errorf("%s.tls.keyFile must be an absolute path", base))
	}
	if filepath.Clean(tls.CertFile) == filepath.Clean(tls.KeyFile) {
		errs = append(errs, fmt.Errorf("%s.tls.certFile and %s.tls.keyFile must be different paths", base, base))
	}
	return errs
}

func validateTelemetrySecretClientPaths(base string, tls kubernautv1alpha2.TelemetryTLSConfig) []error {
	var errs []error
	if filepath.Dir(tls.CertFile) != filepath.Dir(tls.KeyFile) {
		errs = append(errs, fmt.Errorf("%s.tls.certFile and %s.tls.keyFile must use the same directory for Secret-backed mTLS", base, base))
	}
	if filepath.Dir(filepath.Clean(tls.CertFile)) != TelemetryMountDir {
		errs = append(errs, fmt.Errorf("%s.tls.certFile must be mounted below %s for Secret-backed mTLS", base, TelemetryMountDir))
	}
	if filepath.Base(tls.CertFile) == filepath.Base(tls.KeyFile) {
		errs = append(errs, fmt.Errorf("%s.tls.certFile and %s.tls.keyFile must have different filenames for Secret-backed mTLS", base, base))
	}
	if tls.CACertSecretRef != nil && (filepath.Clean(tls.CertFile) == telemetryCAFile || filepath.Clean(tls.KeyFile) == telemetryCAFile) {
		errs = append(errs, fmt.Errorf("%s.tls client material must not overwrite the Secret-backed CA path %s", base, telemetryCAFile))
	}
	return errs
}

func validateTelemetryEndpoint(endpoint string) error {
	_, err := normalizeTelemetryEndpoint(endpoint)
	return err
}

// normalizeTelemetryEndpoint accepts the CR's backwards-compatible host:port
// form and an explicit https://host:port form. The rendered upstream contract
// remains host:port because the exporter enables certificate-verifying TLS for
// every non-local endpoint.
func normalizeTelemetryEndpoint(endpoint string) (string, error) {
	if endpoint == "" || endpoint == "stdout" {
		return endpoint, nil
	}
	if strings.TrimSpace(endpoint) != endpoint || strings.ContainsAny(endpoint, " \t\n\r") {
		return "", fmt.Errorf("must be a host:port or https://host:port value without whitespace; TLS is mandatory")
	}
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return "", fmt.Errorf("must use an https:// URI or host:port value; plaintext HTTP and other URI forms are unsupported")
		}
		endpoint = parsed.Host
	}
	if err := validateTelemetryHostPort(endpoint); err != nil {
		return "", err
	}
	return endpoint, nil
}

func validateTelemetryHostPort(endpoint string) error {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("must be a host:port or https://host:port value; TLS is mandatory")
	}
	host = strings.Trim(host, "[]")
	if telemetryEndpointHostBlocked(host) {
		return fmt.Errorf("must not target a loopback or cloud metadata hostname; use an approved collector endpoint")
	}
	if err := validateTelemetryPort(port); err != nil {
		return err
	}
	return nil
}

func telemetryEndpointHostBlocked(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "metadata.google.internal") || strings.EqualFold(host, "instance-data") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast())
}

func validateTelemetryPort(port string) error {
	if _, err := strconv.Atoi(port); err != nil || !allASCIIDigits(port) {
		return fmt.Errorf("must use a numeric TCP port; TLS is mandatory")
	}
	portNumber, _ := strconv.Atoi(port)
	if portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("must use a TCP port from 1 to 65535; TLS is mandatory")
	}
	return nil
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// ValidateFleet runs Fleet-specific validations against the v1alpha2 CR. It is
// called alongside, rather than merged into, ValidateKubernaut so Fleet
// validation remains independently testable and scoped.
func ValidateFleet(knV2 *kubernautv1alpha2.Kubernaut) []error {
	errs := make([]error, 0, 2)
	errs = append(errs, validateFleetConfig(knV2)...)
	errs = append(errs, validateFleetMetadataCache(knV2)...)
	return errs
}

func validatePostgreSQLSSLMode(kn *kubernautv1alpha2.Kubernaut) []error {
	mode := strings.ToLower(kn.Spec.PostgreSQL.SSLMode)
	if mode == "disable" {
		return []error{fmt.Errorf(
			"spec.postgresql.sslMode: \"disable\" is rejected (FedRAMP SC-8); use \"verify-full\" or \"verify-ca\"")}
	}
	return nil
}

func validatePolicyPrerequisites(_ *kubernautv1alpha2.Kubernaut) []error {
	return nil
}

// validateLLMProfiles validates every named profile's own content, then the
// referential integrity and same-credentialsSecretName constraints for every
// component that references a profile by name.
func validateLLMProfiles(kn *kubernautv1alpha2.Kubernaut) []error {
	errs := make([]error, 0, len(kn.Spec.LLMProfiles))
	for name, profile := range kn.Spec.LLMProfiles {
		errs = append(errs, validateLLMProfileContent(name, &profile)...)
	}
	errs = append(errs, validateLLMProfileRefs(kn)...)
	return errs
}

// validateLLMProfileContent validates a single named profile's own fields.
// These checks mirror the pre-refactor validateLLMPrerequisites, now scoped
// per-profile instead of to the single spec.kubernautAgent.llm block.
func validateLLMProfileContent(name string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	var errs []error
	base := fmt.Sprintf("spec.llmProfiles[%q]", name)
	if llm.Provider == "" {
		errs = append(errs, fmt.Errorf("%s.provider: required — specify the LLM provider (e.g. \"openai\", \"vertexai\")", base))
	}
	if llm.Model == "" {
		errs = append(errs, fmt.Errorf("%s.model: required — specify the LLM model name (e.g. \"gpt-4o\" for provider \"openai\", \"claude-sonnet-5\" for provider \"vertexai\"/\"anthropic\")", base))
	}
	if llm.CredentialsSecretName == "" {
		errs = append(errs, fmt.Errorf("%s.credentialsSecretName: required — provide a Secret with LLM API credentials", base))
	}
	errs = append(errs, validateLLMProfileTuning(base, llm)...)
	if llm.Provider == LLMProviderOpenAI && llm.Endpoint == "" {
		errs = append(errs, fmt.Errorf("%s.endpoint: required when provider is %q — both KA and AF need an explicit endpoint for OpenAI", base, LLMProviderOpenAI))
	}
	errs = append(errs, validateLLMProfileTLS(base, llm)...)
	errs = append(errs, validateLLMProfileOAuth2(base, llm)...)
	if err := validateLLMReasoning(base, llm.Reasoning, llm.Provider); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func validateLLMProfileTuning(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	var errs []error
	if llm.MaxRetries != nil && *llm.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("%s.maxRetries: must be non-negative", base))
	}
	if llm.TimeoutSeconds != nil && *llm.TimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("%s.timeoutSeconds: must be positive", base))
	}
	if llm.Temperature != "" {
		value, err := strconv.ParseFloat(llm.Temperature, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			errs = append(errs, fmt.Errorf("%s.temperature: must be a finite number", base))
		}
	}
	return errs
}

func validateLLMProfileTLS(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	errs := validateLLMProfileCA(base, llm)

	certSet := llm.TLSCertFile != "" || llm.TLSKeyFile != ""
	if certSet && (llm.TLSCertFile == "" || llm.TLSKeyFile == "") {
		errs = append(errs, fmt.Errorf("%s.tlsCertFile and %s.tlsKeyFile must both be set or both empty for mTLS", base, base))
	}
	if !certSet && llm.TLSClientSecretRef != "" {
		errs = append(errs, fmt.Errorf("%s.tlsClientSecretRef: set but tlsCertFile/tlsKeyFile are empty — both pairs are required for mTLS", base))
	}
	errs = append(errs, validateLLMProfileTLSMaterialNames(base, llm)...)
	if !certSet {
		return errs
	}
	if llm.TLSClientSecretRef == "" {
		errs = append(errs, fmt.Errorf("%s.tlsClientSecretRef: required when mTLS cert/key paths are configured", base))
	}
	if !filepath.IsAbs(llm.TLSCertFile) || !filepath.IsAbs(llm.TLSKeyFile) {
		errs = append(errs, fmt.Errorf("%s.tlsCertFile and %s.tlsKeyFile must be absolute paths for mTLS", base, base))
	}
	if filepath.Dir(llm.TLSCertFile) != filepath.Dir(llm.TLSKeyFile) {
		errs = append(errs, fmt.Errorf("%s.tlsCertFile and %s.tlsKeyFile must share one parent directory for mTLS", base, base))
	}
	errs = append(errs, validateLLMProfileTLSPathCollisions(base, llm)...)
	return errs
}

func validateLLMProfileCA(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	var errs []error
	if llm.TLSCaFile != "" && !filepath.IsAbs(llm.TLSCaFile) {
		errs = append(errs, fmt.Errorf("%s.tlsCaFile must be an absolute path", base))
	}
	if llm.TLSCaSecretRef == nil {
		return errs
	}
	if llm.TLSCaFile == "" {
		errs = append(errs, fmt.Errorf("%s.tlsCaFile is required when tlsCaSecretRef is configured", base))
	}
	if llm.TLSCaSecretRef.Name == "" {
		errs = append(errs, fmt.Errorf("%s.tlsCaSecretRef.name must not be empty", base))
	} else if len(k8svalidation.IsDNS1123Subdomain(llm.TLSCaSecretRef.Name)) > 0 {
		errs = append(errs, fmt.Errorf("%s.tlsCaSecretRef.name must be a valid Kubernetes object name", base))
	}
	if key := llmTLSCAKey(*llm); filepath.Base(key) != key || strings.ContainsAny(key, `\/`) {
		errs = append(errs, fmt.Errorf("%s.tlsCaSecretRef.key must be a Secret key name without path separators", base))
	}
	return errs
}

func validateLLMProfileTLSPathCollisions(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	if llm.TLSCaSecretRef == nil {
		return nil
	}
	var errs []error
	if filepath.Clean(llm.TLSCaFile) == filepath.Clean(llm.TLSCertFile) {
		errs = append(errs, fmt.Errorf("%s.tlsCaFile and %s.tlsCertFile must be different paths", base, base))
	}
	if filepath.Clean(llm.TLSCaFile) == filepath.Clean(llm.TLSKeyFile) {
		errs = append(errs, fmt.Errorf("%s.tlsCaFile and %s.tlsKeyFile must be different paths", base, base))
	}
	return errs
}

func validateLLMProfileTLSMaterialNames(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	paths := make(map[string]string, 3)
	addPath := func(field, value string) error {
		if value == "" {
			return nil
		}
		name := filepath.Base(value)
		if name == "." || name == ".." || name == string(filepath.Separator) {
			return fmt.Errorf("%s.%s must name a file below the deterministic TLS mount", base, field)
		}
		if previous, exists := paths[name]; exists {
			return fmt.Errorf("%s.%s and %s.%s must use different filenames for projected TLS material", base, previous, base, field)
		}
		paths[name] = field
		return nil
	}

	var errs []error
	if llm.TLSCaSecretRef != nil {
		if err := addPath("tlsCaFile", llm.TLSCaFile); err != nil {
			errs = append(errs, err)
		}
	}
	if llm.TLSClientSecretRef != "" {
		if err := addPath("tlsCertFile", llm.TLSCertFile); err != nil {
			errs = append(errs, err)
		}
		if err := addPath("tlsKeyFile", llm.TLSKeyFile); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validateLLMProfileOAuth2(base string, llm *kubernautv1alpha2.LLMProfileSpec) []error {
	if !llm.OAuth2.Enabled {
		return nil
	}
	var errs []error
	if llm.OAuth2.TokenURL == "" {
		errs = append(errs, fmt.Errorf("%s.oauth2.tokenURL: required when OAuth2 is enabled", base))
	} else if err := validateFleetHTTPSURL(base+".oauth2.tokenURL", llm.OAuth2.TokenURL); err != nil {
		errs = append(errs, err)
	}
	if llm.OAuth2.CredentialsSecretRef == "" {
		errs = append(errs, fmt.Errorf("%s.oauth2.credentialsSecretRef: required when OAuth2 is enabled", base))
	}
	return errs
}

// validateLLMReasoning rejects effort: "none" combined with enabled: true
// for Anthropic-family providers, mirroring upstream's
// pkg/shared/types.ValidateReasoningConfig verbatim (verified against
// kubernaut@v1.6.0-rc1.0.20260723152521-56562f9f1adb) — Anthropic has no
// "thinking enabled, zero effort" wire state, so this is a genuine
// contradiction rather than a gracefully-degradable value. Left undetected,
// the CR reconciles cleanly but every affected LLM call fails at runtime.
//
// Unlike upstream, this does not re-validate r.Effort against the allowed
// set: the CRD's kubebuilder Enum marker on LLMReasoningSpec.Effort already
// rejects out-of-set values at admission, before this function ever runs.
//
// r may be nil (no reasoning configured on this profile).
func validateLLMReasoning(base string, r *kubernautv1alpha2.LLMReasoningSpec, provider string) error {
	if r == nil {
		return nil
	}
	if r.Enabled && r.Effort == "none" && anthropicFamilyReasoningProviders[provider] {
		return fmt.Errorf(
			"%s.reasoning: effort: \"none\" is not supported for provider %q while reasoning is enabled "+
				"(Anthropic has no \"thinking enabled with zero effort\" state) — use enabled: false to fully "+
				"disable reasoning, or effort: \"minimal\" for Anthropic's lowest real tier",
			base, provider)
	}
	return nil
}

// lookupProfileRef checks that ref matches a key in profiles for the field
// at fieldPath, and produces the standard "undefined profile" error shared
// by every llmProfileRef field when it doesn't.
//
// allowEmpty controls whether the caller handles empty references through
// sole-profile inference or an optional feature gate.
func lookupProfileRef(profiles map[string]kubernautv1alpha2.LLMProfileSpec, ref, fieldPath string, allowEmpty bool) error {
	if ref == "" && allowEmpty {
		return nil
	}
	if _, ok := profiles[ref]; !ok {
		return fmt.Errorf(
			"%s: references undefined profile %q — must match a key in spec.llmProfiles", fieldPath, ref)
	}
	return nil
}

func validateRequiredEffectiveProfile(profiles map[string]kubernautv1alpha2.LLMProfileSpec, ref, fieldPath string) []error {
	if ref != "" {
		if err := lookupProfileRef(profiles, ref, fieldPath, false); err != nil {
			return []error{err}
		}
		return nil
	}
	if len(profiles) == 0 {
		return []error{fmt.Errorf("%s: required — spec.llmProfiles defines no profiles to infer from", fieldPath)}
	}
	return []error{fmt.Errorf(
		"%s: required — spec.llmProfiles defines %d profiles, so the profile is ambiguous without an explicit reference (auto-inferred only when exactly one profile is defined)",
		fieldPath, len(profiles))}
}

// validateLLMProfileRefs validates that every enabled logical LLM consumer
// (KA investigation, workflow discovery, alignment, and API Frontend) resolves
// to a profile defined in spec.llmProfiles. Sole-profile inference is accepted;
// multiple profiles require an explicit reference for each enabled consumer.
func validateLLMProfileRefs(kn *kubernautv1alpha2.Kubernaut) []error {
	var errs []error
	profiles := kn.Spec.LLMProfiles

	errs = append(errs, validateRequiredEffectiveProfile(
		profiles,
		EffectiveInvestigationLLMProfileRef(kn),
		"spec.kubernautAgent.investigation.llmProfileRef",
	)...)
	errs = append(errs, validateRequiredEffectiveProfile(
		profiles,
		EffectiveWorkflowDiscoveryLLMProfileRef(kn),
		"spec.kubernautAgent.workflowDiscovery.llmProfileRef",
	)...)

	if kn.Spec.KubernautAgent.AlignmentCheck.Enabled {
		errs = append(errs, validateRequiredEffectiveProfile(
			profiles,
			EffectiveAlignmentCheckLLMProfileRef(kn),
			"spec.kubernautAgent.alignmentCheck.llmProfileRef",
		)...)
	}

	if kn.Spec.APIFrontendEnabled() {
		errs = append(errs, validateAFLLMProfileRefs(kn, profiles)...)
	}

	return errs
}

// validateAFLLMProfileRefs validates that both of API Frontend's own
// llmProfileRef fields resolve to defined profiles: its main agent
// connection (apiFrontend.llmProfileRef) and its independent
// severity-triage override (apiFrontend.severityTriage.llmProfileRef).
//
// #234 unblocked cross-credential severity-triage overrides for every
// provider except vertex_ai-vs-vertex_ai, because AF's Vertex AI client
// construction relied solely on ambient Application Default Credentials
// rather than a profile's own APIKey/APIKeyFile (kubernaut#1731). #279
// removes that restriction now that kubernaut#1731 is fixed upstream and
// afAgentLLMConfig/afSeverityTriageConfig (configmaps.go) render an
// explicit apiKeyFile for vertex_ai profiles too, mirroring every other
// provider -- cross-credential vertex_ai-vs-vertex_ai overrides are safe
// like any other combination.
func validateAFLLMProfileRefs(kn *kubernautv1alpha2.Kubernaut, profiles map[string]kubernautv1alpha2.LLMProfileSpec) []error {
	var errs []error

	errs = append(errs, validateRequiredEffectiveProfile(
		profiles,
		AFLLMProfileRef(kn),
		"spec.apiFrontend.llmProfileRef",
	)...)

	if st := kn.Spec.APIFrontend.SeverityTriage; st != nil {
		if err := lookupProfileRef(profiles, st.LLMProfileRef, "spec.apiFrontend.severityTriage.llmProfileRef", true); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// validateJWTProviderList validates a slice of JWT providers, checking issuerURL,
// audiences, name uniqueness, and JWKS URL validity. The insecureFlagPath
// parameter is used in error messages to reference the correct parent flag.
func validateJWTProviderList(providers []kubernautv1alpha2.JWTProviderSpec, basePath string, allowInsecure bool, insecureFlagPath string) []error {
	var errs []error
	seen := make(map[string]bool, len(providers))

	for i, p := range providers {
		path := fmt.Sprintf("%s.jwtProviders[%d]", basePath, i)
		if seen[p.Name] {
			errs = append(errs, fmt.Errorf("%s.name: duplicate provider name %q", path, p.Name))
		}
		seen[p.Name] = true
		errs = append(errs, validateJWTProvider(p, path, allowInsecure, insecureFlagPath)...)
	}
	return errs
}

func validateJWTProvider(provider kubernautv1alpha2.JWTProviderSpec, path string, allowInsecure bool, insecureFlagPath string) []error {
	var errs []error
	if provider.IssuerURL == "" {
		errs = append(errs, fmt.Errorf("%s.issuerURL: required", path))
	} else if err := validateOIDCURL(
		provider.IssuerURL,
		path+".issuerURL",
		allowInsecure,
		insecureFlagPath,
	); err != nil {
		errs = append(errs, err)
	}

	if len(provider.Audiences) == 0 {
		errs = append(errs, fmt.Errorf("%s.audiences: at least one audience required", path))
	}

	if provider.JWKSURL != "" {
		if err := validateJWTProviderJWKSURL(provider.JWKSURL, path, allowInsecure, insecureFlagPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validateJWTProviderJWKSURL(raw, path string, allowInsecure bool, insecureFlagPath string) error {
	if len(raw) > maxJWKSURLLength {
		return fmt.Errorf("%s.jwksURL: must be <= %d characters (got %d)", path, maxJWKSURLLength, len(raw))
	}
	return validateOIDCURL(raw, path+".jwksURL", allowInsecure, insecureFlagPath)
}

func validateAPIFrontend(kn *kubernautv1alpha2.Kubernaut) []error {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	var errs []error

	af := kn.Spec.APIFrontend
	if af.AgentCardURL != "" {
		u, err := url.Parse(af.AgentCardURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.agentCardURL: must be a valid URL with scheme and host"))
		}
	}

	// IA-2: When jwtProviders is configured, multi-provider JWT auth
	// satisfies the authentication requirement — top-level issuerURL is
	// not needed. Otherwise an explicit issuerURL is required; the operator
	// must never invent or select an identity provider on the user's behalf.
	hasMultiProvider := len(af.Auth.JWTProviders) > 0
	if !hasMultiProvider {
		if af.Auth.IssuerURL == "" {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.auth.issuerURL: required when jwtProviders is empty"))
		} else if err := validateOIDCURL(
			af.Auth.IssuerURL,
			"spec.apiFrontend.auth.issuerURL",
			af.Auth.AllowInsecureIssuers,
			"spec.apiFrontend.auth.allowInsecureIssuers",
		); err != nil {
			errs = append(errs, err)
		}
	}

	// Deprecated field, but still supported for backward compatibility until
	// removed from the API -- must keep validating it while it's honored.
	if ref := af.RBACRolesConfigMapRef; ref != nil && ref.ConfigMapName == "" { //nolint:staticcheck
		errs = append(errs, fmt.Errorf("spec.apiFrontend.rbacRolesConfigMapRef.configMapName: must not be empty when rbacRolesConfigMapRef is set"))
	}

	errs = append(errs, validateToolRoleBindings(kn)...)

	if hasMultiProvider {
		errs = append(errs, validateJWTProviderList(
			af.Auth.JWTProviders,
			"spec.apiFrontend.auth",
			af.Auth.AllowInsecureIssuers,
			"spec.apiFrontend.auth.allowInsecureIssuers",
		)...)
	}

	return errs
}

func validateConsoleProviderLogoutURL(kn *kubernautv1alpha2.Kubernaut) error {
	const path = "spec.console.auth.providerLogoutURL"
	raw := kn.Spec.Console.Auth.ProviderLogoutURL
	if raw == "" {
		return nil
	}
	logoutURL, err := parseConsoleProviderLogoutURL(raw, path)
	if err != nil {
		return err
	}
	if !kn.Spec.ConsoleEnabled() {
		return nil
	}
	return validateConsoleLogoutAuthority(kn, logoutURL, path)
}

func parseConsoleProviderLogoutURL(raw, path string) (*url.URL, error) {
	if strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("%s must not contain leading or trailing whitespace", path)
	}

	logoutURL, err := url.Parse(raw)
	if err != nil || logoutURL.Scheme == "" || logoutURL.Host == "" || logoutURL.Opaque != "" {
		return nil, fmt.Errorf("%s must be an absolute HTTPS URL", path)
	}
	if err := validateConsoleLogoutURLShape(logoutURL, path); err != nil {
		return nil, err
	}
	if err := validateConsoleLogoutPlaceholders(raw, path); err != nil {
		return nil, err
	}

	query, err := url.ParseQuery(logoutURL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%s contains an invalid query", path)
	}
	if !strings.Contains(raw, idTokenPlaceholder) && queryContainsPlaceholder(query) {
		return nil, fmt.Errorf("%s must use the literal %s placeholder", path, idTokenPlaceholder)
	}
	if key := unsupportedConsoleLogoutQueryParameter(query); key != "" {
		return nil, fmt.Errorf("%s contains unsupported query parameter %q", path, key)
	}
	return logoutURL, nil
}

func validateConsoleLogoutURLShape(logoutURL *url.URL, path string) error {
	if !strings.EqualFold(logoutURL.Scheme, httpsURLScheme) {
		return fmt.Errorf("%s must use https", path)
	}
	if logoutURL.Hostname() == "" {
		return fmt.Errorf("%s must include a host", path)
	}
	if logoutURL.User != nil {
		return fmt.Errorf("%s must not include user information", path)
	}
	if logoutURL.Fragment != "" {
		return fmt.Errorf("%s must not include a fragment", path)
	}
	return nil
}

func validateConsoleLogoutPlaceholders(raw, path string) error {
	if strings.Count(raw, idTokenPlaceholder) > 1 {
		return fmt.Errorf("%s may contain at most one literal %s placeholder", path, idTokenPlaceholder)
	}
	if strings.ContainsAny(strings.ReplaceAll(raw, idTokenPlaceholder, ""), "{}") {
		return fmt.Errorf("%s contains an unsupported placeholder", path)
	}
	return nil
}

func queryContainsPlaceholder(query url.Values) bool {
	for _, values := range query {
		for _, value := range values {
			if strings.Contains(value, idTokenPlaceholder) {
				return true
			}
		}
	}
	return false
}

func unsupportedConsoleLogoutQueryParameter(query url.Values) string {
	allowed := map[string]struct{}{
		"id_token_hint": {},
		"logout_hint":   {},
		"client_id":     {},
		"ui_locales":    {},
	}
	for key := range query {
		if _, ok := allowed[strings.ToLower(key)]; !ok {
			return key
		}
	}
	return ""
}

func validateConsoleLogoutAuthority(kn *kubernautv1alpha2.Kubernaut, logoutURL *url.URL, path string) error {
	issuer := kn.Spec.ConsoleIssuerURL()
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme == "" || issuerURL.Host == "" || issuerURL.User != nil {
		return fmt.Errorf("%s cannot be validated without a valid effective OIDC issuer", path)
	}
	if !strings.EqualFold(issuerURL.Scheme, httpsURLScheme) {
		return fmt.Errorf("%s requires an HTTPS effective OIDC issuer", path)
	}
	if canonicalHTTPSAuthority(logoutURL) != canonicalHTTPSAuthority(issuerURL) {
		return fmt.Errorf("%s authority must match the effective OIDC issuer", path)
	}
	return nil
}

func canonicalHTTPSAuthority(value *url.URL) string {
	port := value.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(value.Hostname()), port)
}

// validateOIDCURL validates the operator-facing OIDC URL contract. OIDC
// discovery and JWKS clients require an absolute URL with a host; HTTP is only
// accepted when the explicit development/test escape hatch is enabled.
func validateOIDCURL(raw, path string, allowInsecure bool, insecureFlagPath string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s: must be an absolute URL with scheme and host", path)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == httpsURLScheme || (scheme == httpURLScheme && allowInsecure) {
		return nil
	}
	return fmt.Errorf("%s: scheme must be %s (got %q); set %s=true to permit HTTP for dev/test", path, httpsURLScheme, u.Scheme, insecureFlagPath)
}

// validToolPersonas is the set of known persona names for tool role bindings.
var validToolPersonas = map[string]bool{
	"sre": true, "ai-orchestrator": true, "cicd": true,
	"observability": true, "l3-audit": true, "remediation-approver": true,
}

func validateToolRoleBindings(kn *kubernautv1alpha2.Kubernaut) []error {
	rbac := kn.Spec.APIFrontend.RBAC
	if rbac == nil {
		return nil
	}

	var errs []error

	if rbac.SARCacheTTL != "" {
		if _, err := time.ParseDuration(rbac.SARCacheTTL); err != nil {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.sarCacheTTL: invalid Go duration %q: %w", rbac.SARCacheTTL, err))
		}
	}

	seen := make(map[string]bool, len(rbac.RoleBindings))
	for i, rb := range rbac.RoleBindings {
		if err := validateToolRoleBinding(rb, i, seen); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// validateToolRoleBinding validates a single spec.apiFrontend.rbac.roleBindings
// entry: exactly one of role/clusterRoleName must be set, it must not
// duplicate an earlier entry (tracked via seen), and a persona-based role
// must be a known persona.
func validateToolRoleBinding(rb kubernautv1alpha2.ToolRoleBinding, index int, seen map[string]bool) error {
	if rb.Role != "" && rb.ClusterRoleName != "" {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: role and clusterRoleName are mutually exclusive", index)
	}
	if rb.Role == "" && rb.ClusterRoleName == "" {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: one of role or clusterRoleName must be set", index)
	}

	if rb.Role == "" {
		if seen[rb.ClusterRoleName] {
			return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate clusterRoleName %q", rb.ClusterRoleName)
		}
		seen[rb.ClusterRoleName] = true
		return nil
	}

	if seen[rb.Role] {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate role %q", rb.Role)
	}
	seen[rb.Role] = true
	if !validToolPersonas[rb.Role] {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: unknown persona %q", rb.Role)
	}
	return nil
}

const (
	alignmentCheckTimeoutMin = time.Second
	alignmentCheckTimeoutMax = 60 * time.Second
)

func validateAlignmentCheck(kn *kubernautv1alpha2.Kubernaut) []error {
	ac := &kn.Spec.KubernautAgent.AlignmentCheck
	if !ac.Enabled {
		return nil
	}

	var errs []error
	const base = "spec.kubernautAgent.alignmentCheck"

	if ac.Timeout != "" {
		d, err := time.ParseDuration(ac.Timeout)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.timeout: invalid Go duration %q: %w", base, ac.Timeout, err))
		} else if d < alignmentCheckTimeoutMin || d > alignmentCheckTimeoutMax {
			errs = append(errs, fmt.Errorf("%s.timeout: must be between %s and %s, got %s", base, alignmentCheckTimeoutMin, alignmentCheckTimeoutMax, d))
		}
	}

	if ac.MaxStepTokens < 0 {
		errs = append(errs, fmt.Errorf("%s.maxStepTokens: must be positive, got %d", base, ac.MaxStepTokens))
	}

	return errs
}

func validateDryRun(kn *kubernautv1alpha2.Kubernaut) []error {
	ro := &kn.Spec.RemediationOrchestrator
	if !ro.DryRun {
		return nil
	}

	if ro.DryRunHoldPeriod == "" {
		return nil
	}

	if _, err := time.ParseDuration(ro.DryRunHoldPeriod); err != nil {
		return []error{fmt.Errorf("spec.remediationOrchestrator.dryRunHoldPeriod: invalid Go duration %q: %w", ro.DryRunHoldPeriod, err)}
	}
	return nil
}

func validateInteractive(kn *kubernautv1alpha2.Kubernaut) []error {
	interactive := kn.Spec.KubernautAgent.Interactive
	if interactive == nil || !interactive.InteractiveEnabled() {
		return nil
	}

	var errs []error
	const base = "spec.kubernautAgent.interactive"

	if interactive.SessionTTL != "" {
		if _, err := time.ParseDuration(interactive.SessionTTL); err != nil {
			errs = append(errs, fmt.Errorf("%s.sessionTTL: invalid Go duration %q: %w", base, interactive.SessionTTL, err))
		}
	}

	if interactive.InactivityTimeout != "" {
		if _, err := time.ParseDuration(interactive.InactivityTimeout); err != nil {
			errs = append(errs, fmt.Errorf("%s.inactivityTimeout: invalid Go duration %q: %w", base, interactive.InactivityTimeout, err))
		}
	}

	return errs
}

// validFleetBackends are the only backends recognized by upstream
// pkg/fleet.FleetConfig (jordigilh/kubernaut). "valkey" was explicitly
// removed upstream and is rejected here for the same reason.
// validMCPGatewayTypes are the only MCP Gateway implementations recognized
// by upstream pkg/fleet/registry (jordigilh/kubernaut).
var validMCPGatewayTypes = map[string]bool{
	"eaigw":                true,
	mcpGatewayTypeKuadrant: true,
}

// fleetBackendACM is spec.fleet.scopeCheck.backend's value for the BYO ACM Search
// integration. backend=fleetmetadatacache already has a named constant
// (ComponentFleetMetadataCache in common.go); this gives "acm" the same
// treatment instead of repeating the bare literal.
const fleetBackendACM = "acm"

var validFleetBackends = map[string]bool{
	ComponentFleetMetadataCache: true,
	fleetBackendACM:             true,
}

// validateFleetConfig validates spec.fleet. When Enabled is false or
// omitted, the other fields are inert and left unvalidated so users can
// pre-stage configuration ahead of enabling it.
func validateFleetConfig(knV2 *kubernautv1alpha2.Kubernaut) []error {
	fleet := &knV2.Spec.Fleet
	if fleet.Enabled == nil || !*fleet.Enabled {
		return nil
	}

	var errs []error
	const base = "spec.fleet"

	if fleet.ScopeCheck.Backend == "" {
		errs = append(errs, fmt.Errorf("%s.scopeCheck.backend: must not be empty when fleet.enabled is true — must be one of: fleetmetadatacache, acm", base))
	} else if !validFleetBackends[fleet.ScopeCheck.Backend] {
		errs = append(errs, fmt.Errorf("%s.scopeCheck.backend: invalid backend %q — must be one of: fleetmetadatacache, acm", base, fleet.ScopeCheck.Backend))
	}

	errs = append(errs, validateFleetScopeCheck(&fleet.ScopeCheck, base)...)

	// FedRAMP IA-5 (authenticator management): upstream pkg/fleet has no
	// unauthenticated mode for the ACM Search GraphQL API. Without a token,
	// Gateway/RemediationOrchestrator crash-loop at startup instead of
	// failing fast here at admission.
	// #222: Gateway and RemediationOrchestrator both unconditionally call
	// upstream Fleet.ValidateFullFederation() at startup, which requires
	// mcpGateway.endpoint and mcpGateway.type whenever fleet is enabled. Omitting
	// either crash-loops both components instead of failing fast here.
	// kubernaut-operator#455: leaving this empty let FleetMetadataCache's
	// cluster registry silently default to watching its own install
	// namespace instead of the MCP Gateway's namespace, instead of the
	// documented cluster-wide fallback (root cause of
	// jordigilh/kubernaut#2298's "0 clusters" symptom). Also least-privilege
	// (CM-6): a cluster can have multiple MCP Gateways installed, so an
	// explicit namespace scopes every fleet-aware component's CRD watch to
	// exactly one of them instead of watching cluster-wide.
	errs = append(errs, validateFleetMCPGateway(&fleet.MCPGateway, base)...)
	errs = append(errs, validateFleetTrust("spec.fleet.mcpGateway.tls", fleet.MCPGateway.TLS)...)

	errs = append(errs, validateFleetOAuth2(knV2, fleet)...)
	errs = append(errs, validateFleetTrust("spec.fleet.oauth2.tls", fleet.OAuth2.TLS)...)
	errs = append(errs, validateFleetMCPClient(fleet.MCPClient)...)

	return errs
}

func validateFleetScopeCheck(scope *kubernautv1alpha2.FleetScopeCheckSpec, base string) []error {
	var errs []error
	if scope.Backend == fleetBackendACM {
		if scope.ACM == nil {
			return []error{fmt.Errorf("%s.scopeCheck.acm: must be set when backend is %q", base, fleetBackendACM)}
		}
		if scope.ACM.Endpoint == "" {
			errs = append(errs, fmt.Errorf("%s.scopeCheck.acm.endpoint: must be set when backend is %q", base, fleetBackendACM))
		} else if err := validateFleetHTTPSURL("spec.fleet.scopeCheck.acm.endpoint", scope.ACM.Endpoint); err != nil {
			errs = append(errs, err)
		}
		if scope.ACM.TokenSecretRef == nil || scope.ACM.TokenSecretRef.Name == "" {
			errs = append(errs, fmt.Errorf("%s.scopeCheck.acm.tokenSecretRef: must be set when backend is %q", base, fleetBackendACM))
		}
		return append(errs, validateFleetTrust("spec.fleet.scopeCheck.acm.tls", scope.ACM.TLS)...)
	}
	if scope.ACM != nil {
		errs = append(errs, fmt.Errorf("%s.scopeCheck.acm: must be omitted when backend is %q", base, ComponentFleetMetadataCache))
	}
	return errs
}

func validateFleetMCPGateway(gateway *kubernautv1alpha2.FleetMCPGatewaySpec, base string) []error {
	var errs []error
	if gateway.Endpoint == "" {
		errs = append(errs, fmt.Errorf("%s.mcpGateway.endpoint: must be set when fleet.enabled is true — Gateway and RemediationOrchestrator require it for remote-cluster reads (upstream Fleet.ValidateFullFederation)", base))
	} else if err := validateFleetHTTPSURL("spec.fleet.mcpGateway.endpoint", gateway.Endpoint); err != nil {
		errs = append(errs, err)
	}
	if gateway.Type == "" {
		errs = append(errs, fmt.Errorf("%s.mcpGateway.type: must be set when fleet.enabled is true — must be one of: eaigw, kuadrant", base))
	} else if !validMCPGatewayTypes[gateway.Type] {
		errs = append(errs, fmt.Errorf("%s.mcpGateway.type: invalid value %q — must be one of: eaigw, kuadrant", base, gateway.Type))
	}
	if gateway.Namespace == "" {
		errs = append(errs, fmt.Errorf("%s.mcpGateway.namespace: must be set when fleet.enabled is true — scopes every fleet-aware component's MCP Gateway CRD watch to a single namespace (kubernaut-operator#455)", base))
	}
	return errs
}

func validateFleetMCPClient(client *kubernautv1alpha2.FleetMCPClientSpec) []error {
	if client == nil {
		return nil
	}
	var errs []error
	validate := func(path, value string) {
		if value == "" {
			return
		}
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be a positive Go duration", path))
		}
	}
	if client.Retry != nil {
		validate("spec.fleet.mcpClient.retry.initialInterval", client.Retry.InitialInterval)
		validate("spec.fleet.mcpClient.retry.maxInterval", client.Retry.MaxInterval)
		validate("spec.fleet.mcpClient.retry.maxElapsedTime", client.Retry.MaxElapsedTime)
	}
	if client.Timeouts != nil {
		validate("spec.fleet.mcpClient.timeouts.tokenRefresh", client.Timeouts.TokenRefresh)
		validate("spec.fleet.mcpClient.timeouts.connect", client.Timeouts.Connect)
		validate("spec.fleet.mcpClient.timeouts.discoverProbe", client.Timeouts.DiscoverProbe)
	}
	return errs
}

// validateFleetOAuth2 validates the OAuth2 configuration required by active
// Fleet. There is no Fleet-specific OAuth2 enable flag; Fleet activation is
// the sole gate and the token endpoint/effective credentials are mandatory.
func validateFleetOAuth2(knV2 *kubernautv1alpha2.Kubernaut, fleet *kubernautv1alpha2.FleetSpec) []error {
	const base = "spec.fleet"

	var errs []error
	if fleet.OAuth2.TokenURL == "" {
		errs = append(errs, fmt.Errorf("%s.oauth2.tokenURL: must be set when fleet.enabled is true", base))
	} else if err := validateFleetHTTPSURL("spec.fleet.oauth2.tokenURL", fleet.OAuth2.TokenURL); err != nil {
		errs = append(errs, err)
	}

	// A federated IdP (e.g. Keycloak) issues distinct per-service OAuth2
	// client registrations against one shared token endpoint (confirmed
	// against upstream's own Helm chart: kubernaut.fleet.oauth2 helper
	// resolves each service's own credentialsSecretRef, falling back to
	// the fleet-wide default). Each of the six fleet-aware components
	// needs its own *effective* value (own override, or the shared
	// fallback) — a shared credentialsSecretRef covers whichever
	// component doesn't override it, but a component that overrides it
	// no longer benefits from the shared value covering it too.
	//
	// #224: generalized from a 2-way Gateway/RemediationOrchestrator
	// switch to a loop over all five fleet-aware components (SP/AF/EM
	// gained their own Fleet override fields) -- see Finding 7.
	//
	// #204: extended to a sixth component -- KA authenticates its own
	// list_clusters/list_tools_for_cluster GatewayDiscoverer calls to
	// the MCP Gateway and needs the same effective-value guarantee.
	//
	// F1 (v1alpha2): each component's bespoke FleetOAuth2CredentialsSecretRef
	// field collapsed into a shared *FleetOverrideSpec.
	components := []struct {
		specPath string
		override *kubernautv1alpha2.FleetOverrideSpec
	}{
		{"spec.gateway.fleet.oauth2CredentialsSecretRef", knV2.Spec.Gateway.Fleet},
		{"spec.remediationOrchestrator.fleet.oauth2CredentialsSecretRef", knV2.Spec.RemediationOrchestrator.Fleet},
		{"spec.signalProcessing.fleet.oauth2CredentialsSecretRef", knV2.Spec.SignalProcessing.Fleet},
		{"spec.apiFrontend.fleet.oauth2CredentialsSecretRef", knV2.Spec.APIFrontend.Fleet},
		{"spec.effectivenessMonitor.fleet.oauth2CredentialsSecretRef", knV2.Spec.EffectivenessMonitor.Fleet},
		{"spec.kubernautAgent.fleet.oauth2CredentialsSecretRef", knV2.Spec.KubernautAgent.Fleet},
	}
	var missing []string
	for _, c := range components {
		if effectiveFleetOAuth2SecretRef(c.override, fleet.OAuth2.CredentialsSecretRef) == "" {
			missing = append(missing, c.specPath)
		}
	}
	switch len(missing) {
	case 0:
		// all six have an effective value.
	case len(components):
		errs = append(errs, fmt.Errorf("%s.oauth2.credentialsSecretRef: must be set when fleet.enabled is true", base))
	default:
		errs = append(errs, fmt.Errorf("%s.oauth2.credentialsSecretRef: must be set, or %s must be set, when fleet.enabled is true (the other fleet-aware components already override their own)", base, strings.Join(missing, " or ")))
	}

	// #235/DD-235: WorkflowExecution is deliberately NOT one of the six
	// components above -- it is the only fleet-aware component that calls
	// MCP write tools (resources_create_or_update/resources_delete), so its
	// credential must never fall back to the shared read-only
	// spec.fleet.oauth2.credentialsSecretRef (least-privilege). Checked
	// unconditionally here, independent of whether the shared field or any
	// of the six read-only components' overrides are set.
	if knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef == "" {
		errs = append(errs, fmt.Errorf("spec.workflowExecution.fleet.oauth2CredentialsSecretRef: must be independently set when fleet.enabled is true -- WorkflowExecution's write-scoped MCP credential never falls back to the shared %s.oauth2.credentialsSecretRef (least-privilege, DD-235)", base))
	}

	return errs
}

func validateFleetHTTPSURL(path, value string) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", path)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != httpsURLScheme || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s must be an absolute https URL without user information", path)
	}
	return nil
}

func validateFleetTrust(path string, trust *kubernautv1alpha2.FleetTrustSpec) []error {
	if trust == nil {
		return nil
	}

	source := trust.Source
	if source == "" {
		source = kubernautv1alpha2.FleetTrustSourceInterService
	}
	hasFile := trust.CAFile != ""
	hasSecret := trust.CACertSecretRef != nil
	var errs []error

	switch source {
	case kubernautv1alpha2.FleetTrustSourceInterService, kubernautv1alpha2.FleetTrustSourceSystem:
		if hasFile || hasSecret {
			errs = append(errs, fmt.Errorf("%s: source %q must not set caFile or caCertSecretRef", path, source))
		}
	case kubernautv1alpha2.FleetTrustSourceFile:
		if !hasFile {
			errs = append(errs, fmt.Errorf("%s.caFile: must be set when source is file", path))
		} else if !filepath.IsAbs(trust.CAFile) {
			errs = append(errs, fmt.Errorf("%s.caFile: must be an absolute path", path))
		}
		if hasSecret {
			errs = append(errs, fmt.Errorf("%s: source file must not set caCertSecretRef", path))
		}
	case kubernautv1alpha2.FleetTrustSourceSecret:
		if !hasSecret || trust.CACertSecretRef.Name == "" {
			errs = append(errs, fmt.Errorf("%s.caCertSecretRef.name: must be set when source is secret", path))
		}
		if hasFile {
			errs = append(errs, fmt.Errorf("%s: source secret must not set caFile", path))
		}
	default:
		errs = append(errs, fmt.Errorf("%s.source: invalid value %q — must be one of: interService, system, file, secret", path, source))
	}

	return errs
}

// validateFleetMetadataCache validates spec.fleetMetadataCache. FMC has no
// separate enable toggle (kubernaut-operator#450): it is active whenever
// spec.fleet.enabled is true and spec.fleet.scopeCheck.backend is
// "fleetmetadatacache" (KubernautSpec.FleetMetadataCacheEnabled()), which
// means validateFleetConfig/validateFleetOAuth2 already enforce FMC's
// mcpGateway.endpoint/mcpGateway.type/oauth2.tokenURL
// requirements unconditionally whenever FMC is active -- duplicating those
// checks here would just double-report the same error. The one FMC-specific
// requirement that isn't already covered is its own OAuth2 credential
// override: FleetMetadataCacheSpec.Fleet isn't one of
// validateFleetOAuth2's six components (Gateway/RemediationOrchestrator/
// SignalProcessing/APIFrontend/EffectivenessMonitor/KubernautAgent), so it
// needs its own effective-value check here.
func validateFleetMetadataCache(knV2 *kubernautv1alpha2.Kubernaut) []error {
	if !knV2.Spec.FleetMetadataCacheEnabled() {
		return nil
	}

	fleet := &knV2.Spec.Fleet
	if effectiveFleetOAuth2SecretRef(knV2.Spec.FleetMetadataCache.Fleet, fleet.OAuth2.CredentialsSecretRef) == "" {
		return []error{fmt.Errorf("spec.fleetMetadataCache.fleet.oauth2CredentialsSecretRef: must be set, or spec.fleet.oauth2.credentialsSecretRef must be set, when fleetMetadataCache is active (fleet.enabled=true, backend=fleetmetadatacache)")}
	}

	return nil
}
