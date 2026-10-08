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

package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const openshiftConfigManagedNamespace = "openshift-config-managed"

var errTelemetryAmbientTrustPending = errors.New("ambient telemetry trust is pending")

type telemetryLane struct {
	component string
	fieldPath string
	spec      kubernautv1alpha2.TelemetrySpec
}

// telemetryMaterialRevisionSet keeps rollout revisions typed to the three
// telemetry-producing workloads. Kubernetes metadata remains map-shaped, but
// this internal value has a fixed schema and should not accept arbitrary
// component names.
type telemetryMaterialRevisionSet struct {
	Gateway        string
	DataStorage    string
	KubernautAgent string
}

func (s *telemetryMaterialRevisionSet) set(component, revision string) {
	switch component {
	case resources.ComponentGateway:
		s.Gateway = revision
	case resources.ComponentDataStorage:
		s.DataStorage = revision
	case resources.ComponentKubernautAgent:
		s.KubernautAgent = revision
	}
}

func (s telemetryMaterialRevisionSet) get(component string) (string, bool) {
	switch component {
	case resources.ComponentGateway:
		return s.Gateway, s.Gateway != ""
	case resources.ComponentDataStorage:
		return s.DataStorage, s.DataStorage != ""
	case resources.ComponentKubernautAgent:
		return s.KubernautAgent, s.KubernautAgent != ""
	default:
		return "", false
	}
}

func telemetryLanes(kn *kubernautv1alpha2.Kubernaut) []telemetryLane {
	lanes := make([]telemetryLane, 0, 3)
	if kn.Spec.GatewayEnabled() {
		lanes = append(lanes, telemetryLane{
			component: resources.ComponentGateway,
			fieldPath: "spec.gateway.config.telemetry",
			spec:      kn.Spec.Gateway.Config.Telemetry,
		})
	}
	lanes = append(lanes,
		telemetryLane{
			component: resources.ComponentDataStorage,
			fieldPath: "spec.dataStorage.telemetry",
			spec:      kn.Spec.DataStorage.Telemetry,
		},
		telemetryLane{
			component: resources.ComponentKubernautAgent,
			fieldPath: "spec.kubernautAgent.telemetry",
			spec:      kn.Spec.KubernautAgent.Telemetry,
		},
	)
	return lanes
}

// ensureTelemetryAmbientTrustSource makes the trust source observable before
// telemetry validation. OpenShift service-CA injection is asynchronous, so
// creating its two ConfigMaps here lets the controller wait on their contents
// instead of deploying a producer with an empty trust bundle. Explicit TLS
// modes are prepared through the same runtime source path used by migration.
func (r *KubernautReconciler) ensureTelemetryAmbientTrustSource(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) error {
	if !hasNetworkTelemetry(kn) {
		return nil
	}
	material, caBundle, err := r.ensureRuntimeTLS(ctx, kn)
	if err != nil {
		return fmt.Errorf("ensuring runtime trust source: %w", err)
	}
	if material.Source == resources.TLSMaterialSourceOpenShiftServiceCA {
		if err := r.ensureNamespaced(ctx, kn, resources.InterServiceCAConfigMap(kn)); err != nil {
			return fmt.Errorf("ensuring inter-service CA ConfigMap: %w", err)
		}
		if err := r.ensureNamespaced(ctx, kn, resources.TrustBundleConfigMap(kn)); err != nil {
			return fmt.Errorf("ensuring ambient trust bundle ConfigMap: %w", err)
		}
		return nil
	}
	if err := r.ensureGenericTLSConfigMaps(ctx, kn, caBundle); err != nil {
		return fmt.Errorf("ensuring explicit ambient trust bundle: %w", err)
	}
	return nil
}

// validateTelemetrySecrets verifies administrator-owned OTLP Secret material
// before the deployment phase. It reads only the selected keys and never
// copies Secret data into status, logs, annotations, or ConfigMaps.
func (r *KubernautReconciler) validateTelemetrySecrets(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) error {
	for _, lane := range telemetryLanes(kn) {
		if !resources.TelemetryNetworkEnabled(lane.spec) {
			continue
		}
		for _, reference := range resources.TelemetrySecretReferences(lane.spec) {
			secret := &corev1.Secret{}
			key := client.ObjectKey{Namespace: kn.Namespace, Name: reference.Name}
			if err := r.Get(ctx, key, secret); err != nil {
				return fmt.Errorf("%s secret %q could not be read: %w", lane.fieldPath, reference.Name, err)
			}
			if len(reference.Keys) == 1 {
				if err := validateTelemetryCASecret(secret, reference.Keys[0]); err != nil {
					return fmt.Errorf("%s.tls.caCertSecretRef: %w", lane.fieldPath, err)
				}
				continue
			}
			if err := validateTelemetryClientSecret(secret); err != nil {
				return fmt.Errorf("%s.tls.tlsClientSecretRef: %w", lane.fieldPath, err)
			}
		}
	}
	return nil
}

func validateTelemetryCASecret(secret *corev1.Secret, key string) error {
	data, ok := secret.Data[key]
	if !ok || len(data) == 0 {
		return fmt.Errorf("secret %q is missing key %q", secret.Name, key)
	}

	certificates, err := parseTelemetryCertificates(data)
	if err != nil {
		return fmt.Errorf("secret %q key %q does not contain valid ca pem: %w", secret.Name, key, err)
	}
	now := time.Now()
	for _, certificate := range certificates {
		if !certificate.IsCA {
			return fmt.Errorf("secret %q key %q contains a non-ca certificate", secret.Name, key)
		}
		if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
			return fmt.Errorf("secret %q key %q contains an expired or not-yet-valid ca certificate", secret.Name, key)
		}
	}
	return nil
}

func parseTelemetryCertificates(data []byte) ([]*x509.Certificate, error) {
	remaining := bytes.TrimSpace(data)
	certificates := make([]*x509.Certificate, 0, 1)
	for len(remaining) > 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN ")) {
			return nil, errors.New("contains non-pem data")
		}
		block, rest := pem.Decode(remaining)
		if block == nil {
			return nil, errors.New("contains malformed pem")
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("contains unsupported pem block %q", block.Type)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("contains an invalid certificate: %w", err)
		}
		certificates = append(certificates, certificate)
		remaining = bytes.TrimSpace(rest)
	}
	if len(certificates) == 0 {
		return nil, errors.New("does not contain a certificate")
	}
	return certificates, nil
}

func validateTelemetryClientSecret(secret *corev1.Secret) error {
	certificatePEM, certificateOK := secret.Data[corev1.TLSCertKey]
	keyPEM, keyOK := secret.Data[corev1.TLSPrivateKeyKey]
	if !certificateOK || len(certificatePEM) == 0 {
		return fmt.Errorf("secret %q is missing key %q", secret.Name, corev1.TLSCertKey)
	}
	if !keyOK || len(keyPEM) == 0 {
		return fmt.Errorf("secret %q is missing key %q", secret.Name, corev1.TLSPrivateKeyKey)
	}
	pair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return fmt.Errorf("secret %q contains an invalid tls key pair: %w", secret.Name, err)
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("secret %q does not contain a client certificate", secret.Name)
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("secret %q contains an invalid client certificate: %w", secret.Name, err)
	}
	now := time.Now()
	if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return fmt.Errorf("secret %q contains an expired or not-yet-valid client certificate", secret.Name)
	}
	if certificate.IsCA {
		return fmt.Errorf("secret %q contains a CA certificate instead of a client certificate", secret.Name)
	}
	if len(certificate.ExtKeyUsage) > 0 && !telemetryClientAuthUsage(certificate.ExtKeyUsage) {
		return fmt.Errorf("secret %q client certificate does not allow client authentication", secret.Name)
	}
	return nil
}

func telemetryClientAuthUsage(usages []x509.ExtKeyUsage) bool {
	for _, usage := range usages {
		if usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

// telemetryMaterialRevisions resolves one non-sensitive revision per active
// network telemetry producer. Ambient trust is shared, while referenced
// Secret resource versions are lane-specific so unrelated producers do not
// roll when only one telemetry Secret changes.
func (r *KubernautReconciler) telemetryMaterialRevisions(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (telemetryMaterialRevisionSet, error) {
	lanes := telemetryLanes(kn)
	if !hasNetworkTelemetry(kn) {
		return telemetryMaterialRevisionSet{}, nil
	}
	if err := r.validateTelemetryAmbientTrust(ctx, kn); err != nil {
		return telemetryMaterialRevisionSet{}, err
	}
	ambientRevision, err := r.telemetryAmbientTrustRevision(ctx, kn)
	if err != nil {
		return telemetryMaterialRevisionSet{}, err
	}
	revisions := telemetryMaterialRevisionSet{}
	for _, lane := range lanes {
		if !resources.TelemetryNetworkEnabled(lane.spec) {
			continue
		}
		inputs := []string{"ambient=" + ambientRevision}
		for _, reference := range resources.TelemetrySecretReferences(lane.spec) {
			secret := &corev1.Secret{}
			key := client.ObjectKey{Namespace: kn.Namespace, Name: reference.Name}
			if err := r.Get(ctx, key, secret); err != nil {
				return telemetryMaterialRevisionSet{}, fmt.Errorf("reading telemetry Secret %q for revision: %w", reference.Name, err)
			}
			inputs = append(inputs, fmt.Sprintf("secret=%s/%s@%s", key.Namespace, key.Name, secret.ResourceVersion))
		}
		digest := sha256.Sum256([]byte(strings.Join(inputs, "\x00")))
		revisions.set(lane.component, fmt.Sprintf("%x", digest))
	}
	return revisions, nil
}

// validateTelemetryAmbientTrust checks the effective inter-service trust
// ConfigMap when it contains material. OpenShift service-CA injection is
// asynchronous, so an absent or empty bundle is treated as pending rather
// than as a plaintext fallback; a present malformed PEM bundle is rejected.
func (r *KubernautReconciler) validateTelemetryAmbientTrust(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) error {
	if !hasNetworkTelemetry(kn) {
		return nil
	}
	material, err := resources.ResolveTLSMaterial(kn)
	if err != nil {
		return fmt.Errorf("resolving ambient trust source: %w", err)
	}
	// Manual mode may mount an administrator-owned CA Secret directly. The
	// regular runtime TLS validation already validates that Secret; there is no
	// ConfigMap-backed ambient source to inspect in this branch.
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual && material.InternalCASecretName != "" {
		return nil
	}

	configMapName := resources.TrustBundleConfigMapName
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual {
		configMapName = resources.InterServiceCAConfigMapName
	}
	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: kn.Namespace, Name: configMapName}
	if err := r.Get(ctx, key, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: ConfigMap %q does not exist", errTelemetryAmbientTrustPending, configMapName)
		}
		return fmt.Errorf("reading ambient trust ConfigMap %q: %w", configMapName, err)
	}
	caPEM := strings.TrimSpace(configMap.Data[resources.InterServiceTLSCAKeyFor(kn)])
	if caPEM == "" {
		return fmt.Errorf("%w: ConfigMap %q key %q is empty", errTelemetryAmbientTrustPending, configMapName, resources.InterServiceTLSCAKeyFor(kn))
	}
	certificates, err := parseTelemetryCertificates([]byte(caPEM))
	if err != nil {
		return fmt.Errorf("ambient trust ConfigMap %q key %q does not contain valid ca pem: %w", configMapName, resources.InterServiceTLSCAKeyFor(kn), err)
	}
	now := time.Now()
	for _, certificate := range certificates {
		if !certificate.IsCA {
			return fmt.Errorf("ambient trust ConfigMap %q key %q contains a non-ca certificate", configMapName, resources.InterServiceTLSCAKeyFor(kn))
		}
		if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
			return fmt.Errorf("ambient trust ConfigMap %q key %q contains an expired or not-yet-valid ca certificate", configMapName, resources.InterServiceTLSCAKeyFor(kn))
		}
	}
	return nil
}

func (r *KubernautReconciler) telemetryAmbientTrustRevision(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (string, error) {
	material, err := resources.ResolveTLSMaterial(kn)
	if err != nil {
		return "", fmt.Errorf("resolving runtime TLS material: %w", err)
	}
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual && material.InternalCASecretName != "" {
		secret := &corev1.Secret{}
		key := client.ObjectKey{Namespace: kn.Namespace, Name: material.InternalCASecretName}
		if err := r.Get(ctx, key, secret); err != nil {
			return "", fmt.Errorf("reading manual ambient trust Secret %q: %w", key.Name, err)
		}
		return fmt.Sprintf("secret=%s/%s@%s", key.Namespace, key.Name, secret.ResourceVersion), nil
	}

	configMapName := resources.TrustBundleConfigMapName
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual {
		configMapName = resources.InterServiceCAConfigMapName
	}
	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: kn.Namespace, Name: configMapName}
	if err := r.Get(ctx, key, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return configMapName + "@missing", nil
		}
		return "", fmt.Errorf("reading ambient telemetry trust ConfigMap %q: %w", configMapName, err)
	}
	return fmt.Sprintf("%s@%s", configMapName, configMap.ResourceVersion), nil
}

func stampTelemetryMaterialRevision(dep *appsv1.Deployment, revisions telemetryMaterialRevisionSet) {
	component := dep.Spec.Template.Labels["app"]
	revision, ok := revisions.get(component)
	if !ok || revision == "" {
		return
	}
	annotations := dep.Spec.Template.Annotations
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	annotations[resources.AnnotationTelemetryMaterialRevision] = revision
	dep.Spec.Template.Annotations = annotations
}

// telemetrySecretToKubernaut maps changes to administrator-owned telemetry
// Secrets to the singleton CRs that reference them.
func (r *KubernautReconciler) telemetrySecretToKubernaut(ctx context.Context, object client.Object) []reconcile.Request {
	if object == nil {
		return nil
	}
	list := &kubernautv1alpha2.KubernautList{}
	if err := r.List(ctx, list, client.InNamespace(object.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "failed to list Kubernaut resources for telemetry Secret event", "secret", object.GetName())
		return nil
	}

	requests := make([]reconcile.Request, 0, len(list.Items))
	for index := range list.Items {
		kn := &list.Items[index]
		if telemetrySecretReferenced(kn, object.GetName()) || ambientTrustSecretReferenced(kn, object.GetName()) {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}})
		}
	}
	return requests
}

func telemetrySecretReferenced(kn *kubernautv1alpha2.Kubernaut, secretName string) bool {
	for _, lane := range telemetryLanes(kn) {
		for _, reference := range resources.TelemetrySecretReferences(lane.spec) {
			if reference.Name == secretName {
				return true
			}
		}
	}
	return false
}

func ambientTrustSecretReferenced(kn *kubernautv1alpha2.Kubernaut, secretName string) bool {
	if !hasNetworkTelemetry(kn) {
		return false
	}
	material, err := resources.ResolveTLSMaterial(kn)
	if err != nil || material.Source == resources.TLSMaterialSourceOpenShiftServiceCA {
		return false
	}
	return material.InternalCASecretName != "" && material.InternalCASecretName == secretName
}

func hasNetworkTelemetry(kn *kubernautv1alpha2.Kubernaut) bool {
	for _, lane := range telemetryLanes(kn) {
		if resources.TelemetryNetworkEnabled(lane.spec) {
			return true
		}
	}
	return false
}

// telemetryAmbientConfigMapToKubernaut maps updates to ConfigMaps that feed
// the effective inter-service trust source. The explicit watch covers manual
// administrator-owned ConfigMaps; operator-owned variants are also observed
// through Owns(ConfigMap).
func (r *KubernautReconciler) telemetryAmbientConfigMapToKubernaut(ctx context.Context, object client.Object) []reconcile.Request {
	configMap, ok := object.(*corev1.ConfigMap)
	if !ok || !telemetryAmbientConfigMap(configMap) {
		return nil
	}
	list := &kubernautv1alpha2.KubernautList{}
	if err := r.List(ctx, list); err != nil {
		logf.FromContext(ctx).Error(err, "failed to list Kubernaut resources for ambient trust event", "configMap", configMap.Name)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for index := range list.Items {
		kn := &list.Items[index]
		if configMap.Namespace != kn.Namespace && configMap.Namespace != openshiftConfigManagedNamespace {
			continue
		}
		if !hasNetworkTelemetry(kn) || !ambientTrustConfigMapRelevant(kn, configMap) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}})
	}
	return requests
}

func (r *KubernautReconciler) setTelemetryAmbientTrustPending(ctx context.Context, kn *kubernautv1alpha2.Kubernaut, err error) (reconcile.Result, error) {
	message := err.Error()
	if statusErr := r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type:               kubernautv1alpha2.ConditionBYOValidated,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonTLSWaitingForServiceCA,
			Message:            message,
			ObservedGeneration: kn.Generation,
		})
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type:               kubernautv1alpha2.ConditionTLSReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonTLSWaitingForServiceCA,
			Message:            message,
			ObservedGeneration: kn.Generation,
		})
		r.setPhase(kn, kubernautv1alpha2.PhaseValidating)
	}); statusErr != nil {
		return reconcile.Result{}, statusErr
	}
	return reconcile.Result{RequeueAfter: requeueMigrationPoll}, nil
}

func ambientTrustConfigMapRelevant(kn *kubernautv1alpha2.Kubernaut, configMap *corev1.ConfigMap) bool {
	if configMap.Namespace == openshiftConfigManagedNamespace && configMap.Name == "default-ingress-cert" {
		return kn.Spec.TLS.Mode == ""
	}
	if configMap.Name == resources.TrustBundleConfigMapName {
		return kn.Spec.TLS.Mode != kubernautv1alpha2.TLSModeManual
	}
	if configMap.Name != resources.InterServiceCAConfigMapName {
		return false
	}
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual {
		material, err := resources.ResolveTLSMaterial(kn)
		return err == nil && material.InternalCASecretName == ""
	}
	return kn.Spec.TLS.Mode == ""
}

func telemetryAmbientConfigMap(configMap *corev1.ConfigMap) bool {
	if configMap.Namespace == openshiftConfigManagedNamespace && configMap.Name == "default-ingress-cert" {
		return true
	}
	return configMap.Name == resources.InterServiceCAConfigMapName || configMap.Name == resources.TrustBundleConfigMapName
}
