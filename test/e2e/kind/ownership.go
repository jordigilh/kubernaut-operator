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

package kind

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ownershipWitness struct {
	resource, name, namespace string
	object                    *unstructured.Unstructured
}

const workflowNamespaceName = "kubernaut-workflows"

func ownershipObject(ctx context.Context, resource, name, namespace string) (*unstructured.Unstructured, error) {
	args := []string{"get", resource, name, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	output, err := kubectl(ctx, args...)
	if err != nil {
		return nil, err
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON([]byte(output)); err != nil {
		return nil, fmt.Errorf("decoding %s %s: %w", resource, name, err)
	}
	return object, nil
}

// These are deliberately administrator-owned fixtures, not desired objects
// fabricated by resource builders. The installed operator is the only actor
// allowed to reconcile the Kubernaut CR; the test observes public APIs.
func createOwnershipConflicts(ctx context.Context) ([]ownershipWitness, error) {
	secret := &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "datastorage-db-secret", Namespace: kubernautNamespace},
		StringData: map[string]string{"administrator": "disposable-ownership-sentinel"},
	}
	ingress := &networkingv1.Ingress{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-ingress", Namespace: kubernautNamespace},
		Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: "administrator-service", Port: networkingv1.ServiceBackendPort{Number: 443},
			},
		}},
	}
	role := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole"},
		ObjectMeta: metav1.ObjectMeta{Name: "ownership-514-administrator", Labels: map[string]string{
			"kubernaut.ai/core-cluster-rbac": "true",
			"app.kubernetes.io/instance":     "kubernaut",
			"app.kubernetes.io/managed-by":   "administrator",
		}},
		Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}}},
	}
	if err := applyYAML(ctx, secret, ingress, role); err != nil {
		return nil, err
	}
	witnesses := []ownershipWitness{
		{resource: "secret", name: secret.Name, namespace: secret.Namespace},
		{resource: "ingress", name: ingress.Name, namespace: ingress.Namespace},
		{resource: "clusterrole", name: role.Name},
	}
	return captureOwnershipWitnesses(ctx, witnesses)
}

func captureOwnershipWitnesses(ctx context.Context, witnesses []ownershipWitness) ([]ownershipWitness, error) {
	for i := range witnesses {
		witness := &witnesses[i]
		object, err := ownershipObject(ctx, witness.resource, witness.name, witness.namespace)
		if err != nil {
			return nil, err
		}
		witness.object = object
	}
	return witnesses, nil
}

func captureProvisioningWitnesses(ctx context.Context) ([]ownershipWitness, error) {
	return captureOwnershipWitnesses(ctx, []ownershipWitness{
		{resource: "deployment", name: "postgresql", namespace: kubernautNamespace},
		{resource: "deployment", name: "valkey", namespace: kubernautNamespace},
		{resource: "service", name: "postgresql", namespace: kubernautNamespace},
		{resource: "service", name: "valkey", namespace: kubernautNamespace},
		{resource: "secret", name: "postgresql-secret", namespace: kubernautNamespace},
		{resource: "secret", name: "valkey-secret", namespace: kubernautNamespace},
		{resource: "secret", name: "llm-credentials", namespace: kubernautNamespace},
		{resource: "configmap", name: "aianalysis-policy", namespace: kubernautNamespace},
		{resource: "configmap", name: "signalprocessing-policy", namespace: kubernautNamespace},
	})
}

func createWorkflowOwnershipContent(ctx context.Context) ([]ownershipWitness, error) {
	content := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "provisioning-workflow-artifact", Namespace: workflowNamespaceName},
		Data:       map[string]string{"workflow": "retain after operator uninstall"},
	}
	if err := applyYAML(ctx, content); err != nil {
		return nil, err
	}
	return captureOwnershipWitnesses(ctx, []ownershipWitness{
		{resource: "configmap", name: content.Name, namespace: content.Namespace},
	})
}

func assertProvisioningWitnesses(ctx context.Context, witnesses []ownershipWitness) error {
	for _, witness := range witnesses {
		live, err := ownershipObject(ctx, witness.resource, witness.name, witness.namespace)
		if err != nil {
			return err
		}
		before := witness.object.DeepCopy()
		// Deployment/Service status belongs to Kubernetes, not the operator.
		// Preserve identity, spec, labels and owners while ignoring status churn.
		if witness.resource == "deployment" || witness.resource == "service" {
			delete(before.Object, "status")
			delete(live.Object, "status")
			before.SetResourceVersion("")
			live.SetResourceVersion("")
			before.SetManagedFields(nil)
			live.SetManagedFields(nil)
		}
		if !reflect.DeepEqual(live.Object, before.Object) {
			return fmt.Errorf("provisioning-owned %s %s changed or was replaced", witness.resource, witness.name)
		}
	}
	return nil
}

func assertRetainedNamespaces(ctx context.Context, witnesses []ownershipWitness) error {
	for _, witness := range witnesses {
		live, err := ownershipObject(ctx, "namespace", witness.name, "")
		if err != nil {
			return err
		}
		if live.GetUID() != witness.object.GetUID() || live.GetDeletionTimestamp() != nil {
			return fmt.Errorf("namespace %s was replaced or is terminating", witness.name)
		}
	}
	return nil
}

func assertOwnershipWitnesses(ctx context.Context, witnesses []ownershipWitness) error {
	for _, witness := range witnesses {
		live, err := ownershipObject(ctx, witness.resource, witness.name, witness.namespace)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(live.Object, witness.object.Object) {
			return fmt.Errorf("administrator %s %s changed outside the ownership contract", witness.resource, witness.name)
		}
	}
	return nil
}

func managedOwnershipRole(ctx context.Context) (*unstructured.Unstructured, error) {
	selector := "app.kubernetes.io/managed-by=kubernaut-operator,app.kubernetes.io/instance=kubernaut," +
		"kubernaut.ai/core-cluster-rbac=true"
	output, err := kubectl(ctx, "get", "clusterroles", "-l", selector, "-o", "json")
	if err != nil {
		return nil, err
	}
	list := &unstructured.UnstructuredList{}
	if err := list.UnmarshalJSON([]byte(output)); err != nil {
		return nil, fmt.Errorf("decoding managed ClusterRoles: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no operator-managed ClusterRole exists")
	}
	return list.Items[0].DeepCopy(), nil
}

func ownershipGenerationReady(ctx context.Context) error {
	kn, err := ownershipObject(ctx, "kubernaut", "kubernaut", kubernautNamespace)
	if err != nil {
		return err
	}
	conditions, _, err := unstructured.NestedSlice(kn.Object, "status", "conditions")
	if err != nil {
		return err
	}
	for _, item := range conditions {
		condition, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid Kubernaut status condition")
		}
		if condition["type"] == "ServicesDeployed" && condition["status"] == conditionTrue &&
			condition["observedGeneration"] == kn.GetGeneration() {
			return nil
		}
	}
	return fmt.Errorf("services have not reconciled generation %d", kn.GetGeneration())
}

func deleteOwnershipConflicts(ctx context.Context) error {
	for _, args := range [][]string{
		{"delete", "ingress", "gateway-ingress", "-n", kubernautNamespace, "--ignore-not-found"},
		{"delete", "clusterrole", "ownership-514-administrator", "--ignore-not-found"},
	} {
		if _, err := kubectl(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}
