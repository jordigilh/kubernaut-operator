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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	// DataStorageAuditHashKeySecretName is the stable default Secret name.
	DataStorageAuditHashKeySecretName = "datastorage-audit-hmac-key"
	// DataStorageAuditHashKeyStateConfigMapName records non-secret key identity.
	DataStorageAuditHashKeyStateConfigMapName = "datastorage-audit-hmac-key-state"
	// DataStorageAuditHashKeySecretDataKey is the projected Secret data key.
	DataStorageAuditHashKeySecretDataKey = "audit-hmac-key.yaml" //nolint:gosec // this is a non-secret Kubernetes data-key name
	// DataStorageAuditHashKeySecretsFile is the upstream-mounted audit key path.
	DataStorageAuditHashKeySecretsFile = "/etc/datastorage/secrets/audit-hmac-key.yaml"
	// DataStorageAuditHashKeyDefault is the upstream inner YAML key.
	DataStorageAuditHashKeyDefault = "hmacKey"
	// DataStorageAuditHashKeySourceGenerated identifies an operator-provisioned key.
	DataStorageAuditHashKeySourceGenerated = "operator-generated"
	// DataStorageAuditHashKeySourceExisting identifies an administrator-provided key.
	DataStorageAuditHashKeySourceExisting = "administrator-managed"
	// DataStorageAuditHashKeyStatePhaseKey is the state marker phase field.
	DataStorageAuditHashKeyStatePhaseKey = "phase"
	// DataStorageAuditHashKeyStateProvisioning identifies an incomplete first write.
	DataStorageAuditHashKeyStateProvisioning = "provisioning"
	// DataStorageAuditHashKeyStateReady identifies a committed key identity.
	DataStorageAuditHashKeyStateReady = "ready"
	// DataStorageAuditHashKeyFinalizer prevents accidental key deletion. The
	// finalizer is removed only by an explicit administrative cleanup procedure
	// after audit data is retired.
	DataStorageAuditHashKeyFinalizer = "kubernaut.ai/audit-hmac-key-protection"

	// DataStorageAuditHashKeyManagedAnnotation identifies the operator-generated key.
	DataStorageAuditHashKeyManagedAnnotation = "kubernaut.ai/audit-hmac-key"
	// DataStorageAuditHashKeySourceAnnotation records the non-secret source.
	DataStorageAuditHashKeySourceAnnotation = "kubernaut.ai/audit-hmac-key-source"
)

var auditHashKeyRandomRead = rand.Read

// DataStorageAuditHashKeyReference is the effective non-secret key source.
type DataStorageAuditHashKeyReference struct {
	SecretName string
	SecretKey  string
	Generated  bool
}

// DataStorageAuditHashKeyReferenceFor resolves omitted CR fields to the
// operator-managed default while preserving an administrator Secret override.
func DataStorageAuditHashKeyReferenceFor(kn *kubernautv1alpha2.Kubernaut) DataStorageAuditHashKeyReference {
	ref := DataStorageAuditHashKeyReference{
		SecretName: DataStorageAuditHashKeySecretName,
		SecretKey:  DataStorageAuditHashKeyDefault,
		Generated:  true,
	}
	if spec := kn.Spec.DataStorage.AuditHashKey; spec != nil {
		if spec.ExistingSecret != "" {
			ref.SecretName = spec.ExistingSecret
			ref.Generated = false
		}
		if spec.SecretKey != "" {
			ref.SecretKey = spec.SecretKey
		}
	}
	return ref
}

// ValidateDataStorageAuditHashKeyReference validates all key-source identity
// fields before any state marker or Secret mutation is attempted.
func ValidateDataStorageAuditHashKeyReference(ref DataStorageAuditHashKeyReference) error {
	if err := validateDataStorageAuditHashKeyName(ref.SecretKey); err != nil {
		return err
	}
	if errs := validation.IsDNS1123Subdomain(ref.SecretName); len(errs) > 0 {
		return fmt.Errorf("audit HMAC secret name %q is invalid: %s", ref.SecretName, errs[0])
	}
	return nil
}

// NewDataStorageAuditHashKeySecret creates the initial generated key. Callers
// must not update or recreate this Secret after the state marker exists.
func NewDataStorageAuditHashKeySecret(kn *kubernautv1alpha2.Kubernaut, ref DataStorageAuditHashKeyReference) (*corev1.Secret, error) {
	if !ref.Generated {
		return nil, fmt.Errorf("audit HMAC secret %q is administrator-managed", ref.SecretName)
	}
	if err := ValidateDataStorageAuditHashKeyReference(ref); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := auditHashKeyRandomRead(key); err != nil {
		return nil, fmt.Errorf("generating audit HMAC key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	contentBytes := []byte(fmt.Sprintf("%s: %s\n", ref.SecretKey, encoded))
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       ref.SecretName,
			Namespace:  kn.Namespace,
			Labels:     ComponentLabels(kn, ComponentDataStorage),
			Finalizers: []string{DataStorageAuditHashKeyFinalizer},
			Annotations: map[string]string{
				DataStorageAuditHashKeyManagedAnnotation: "true",
				DataStorageAuditHashKeySourceAnnotation:  DataStorageAuditHashKeySourceGenerated,
			},
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			DataStorageAuditHashKeySecretDataKey: contentBytes,
		},
	}, nil
}

// DataStorageAuditHashKeyStateConfigMap records only key identity and source.
// It deliberately never contains key material.
func DataStorageAuditHashKeyStateConfigMap(kn *kubernautv1alpha2.Kubernaut, ref DataStorageAuditHashKeyReference) *corev1.ConfigMap {
	source := DataStorageAuditHashKeySourceExisting
	if ref.Generated {
		source = DataStorageAuditHashKeySourceGenerated
	}
	return &corev1.ConfigMap{
		ObjectMeta: func() metav1.ObjectMeta {
			metadata := ObjectMeta(kn, DataStorageAuditHashKeyStateConfigMapName, ComponentDataStorage)
			metadata.Finalizers = []string{DataStorageAuditHashKeyFinalizer}
			return metadata
		}(),
		Data: map[string]string{
			"secretName":                         ref.SecretName,
			"secretKey":                          ref.SecretKey,
			"source":                             source,
			DataStorageAuditHashKeyStatePhaseKey: DataStorageAuditHashKeyStateReady,
		},
	}
}

// ValidateDataStorageAuditHashKeySecret validates the mounted YAML contract
// without returning or logging the key value.
func ValidateDataStorageAuditHashKeySecret(secret *corev1.Secret, keyName string) error {
	_, err := dataStorageAuditHashKeyValue(secret, keyName)
	return err
}

// DataStorageAuditHashKeyFingerprint returns a non-secret identity digest for
// the effective key. It is used only to detect replacement of an initialized
// Secret; the key value itself is never persisted in the state marker.
func DataStorageAuditHashKeyFingerprint(secret *corev1.Secret, keyName string) (string, error) {
	value, err := dataStorageAuditHashKeyValue(secret, keyName)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:]), nil
}

func dataStorageAuditHashKeyValue(secret *corev1.Secret, keyName string) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("audit HMAC secret is nil")
	}
	if err := validateDataStorageAuditHashKeyName(keyName); err != nil {
		return "", err
	}
	data, ok := secret.Data[DataStorageAuditHashKeySecretDataKey]
	if !ok || len(data) == 0 {
		return "", fmt.Errorf("secret %q is missing required key %q", secret.Name, DataStorageAuditHashKeySecretDataKey)
	}
	values := map[string]string{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("secret %q key %q contains invalid YAML", secret.Name, DataStorageAuditHashKeySecretDataKey)
	}
	value := values[keyName]
	if strings.TrimSpace(value) != value {
		return "", fmt.Errorf("secret %q key %q contains leading or trailing whitespace", secret.Name, keyName)
	}
	if value == "" {
		return "", fmt.Errorf("secret %q key %q is empty or missing", secret.Name, keyName)
	}
	if len([]byte(value)) < 32 {
		return "", fmt.Errorf("secret %q key %q must contain at least 32 bytes", secret.Name, keyName)
	}
	return value, nil
}

func validateDataStorageAuditHashKeyName(keyName string) error {
	if keyName == "" {
		return fmt.Errorf("audit HMAC key name is empty")
	}
	for _, character := range keyName {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("audit HMAC key name %q contains unsupported characters", keyName)
	}
	return nil
}
