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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const PostgreSQLDataStorageSecretKey = "db-secrets.yaml" //nolint:gosec // Secret data-key name, not credential data

type dataStorageCredentialYAML struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// ValidatePostgreSQLSecret validates the single administrator-owned Secret
// consumed by PostgreSQL, migration, and DataStorage. Secret values are never
// included in returned errors.
func ValidatePostgreSQLSecret(secret *corev1.Secret) error {
	if secret == nil {
		return fmt.Errorf("postgresql secret is nil")
	}

	user, err := requiredSecretValue(secret, "POSTGRES_USER")
	if err != nil {
		return err
	}
	password, err := requiredSecretValue(secret, "POSTGRES_PASSWORD")
	if err != nil {
		return err
	}
	if _, err := requiredSecretValue(secret, "POSTGRES_DB"); err != nil {
		return err
	}

	runtime, err := parseDataStorageCredential(secret)
	if err != nil {
		return err
	}
	if runtime.Username != user {
		return fmt.Errorf("postgresql secret %q has inconsistent database username", secret.Name)
	}
	if runtime.Password != password {
		return fmt.Errorf("postgresql secret %q has inconsistent database password", secret.Name)
	}
	return nil
}

// ValidateValkeySecret validates the administrator-owned Valkey credential
// document without adopting or rewriting the referenced Secret.
func ValidateValkeySecret(secret *corev1.Secret) error {
	if secret == nil {
		return fmt.Errorf("valkey secret is nil")
	}
	raw, ok := secret.Data["valkey-secrets.yaml"]
	if !ok || len(raw) == 0 {
		return fmt.Errorf("valkey secret %q is missing required key %q", secret.Name, "valkey-secrets.yaml")
	}
	var document struct {
		Password string `json:"password" yaml:"password"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("valkey secret %q contains invalid credential YAML", secret.Name)
	}
	if strings.TrimSpace(document.Password) == "" {
		return fmt.Errorf("valkey secret %q is missing a non-empty password", secret.Name)
	}
	return nil
}

// parseDataStorageCredential decodes and validates the DataStorage credential
// document from the administrator-owned PostgreSQL Secret.
func parseDataStorageCredential(secret *corev1.Secret) (dataStorageCredentialYAML, error) {
	raw, ok := secret.Data[PostgreSQLDataStorageSecretKey]
	if !ok || len(raw) == 0 {
		return dataStorageCredentialYAML{}, fmt.Errorf("postgresql secret %q is missing required key %q", secret.Name, PostgreSQLDataStorageSecretKey)
	}
	var document dataStorageCredentialYAML
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return dataStorageCredentialYAML{}, fmt.Errorf("postgresql secret %q contains invalid credential YAML", secret.Name)
	}
	if strings.TrimSpace(document.Username) == "" {
		return dataStorageCredentialYAML{}, fmt.Errorf("postgresql secret %q is missing a non-empty username", secret.Name)
	}
	if strings.TrimSpace(document.Password) == "" {
		return dataStorageCredentialYAML{}, fmt.Errorf("postgresql secret %q is missing a non-empty password", secret.Name)
	}
	return document, nil
}

// requiredSecretValue returns a non-empty PostgreSQL Secret value without
// including the value in an error message.
func requiredSecretValue(secret *corev1.Secret, key string) (string, error) {
	raw, ok := secret.Data[key]
	if !ok || strings.TrimSpace(string(raw)) == "" {
		return "", fmt.Errorf("postgresql secret %q is missing required key %q", secret.Name, key)
	}
	return string(raw), nil
}
