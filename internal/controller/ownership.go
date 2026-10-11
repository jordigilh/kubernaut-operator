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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// checkResourceOwnership is the common write/cleanup gate from
// docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md. Provider policies retain their
// separate, stricter gate. Only public object/owner metadata is logged.
func (r *KubernautReconciler) checkResourceOwnership(ctx context.Context, kn *kubernautv1alpha2.Kubernaut, obj client.Object, controllerOwned bool) error {
	if err := resources.ResourceOwnershipError(kn, obj, controllerOwned); err != nil {
		conflict := r.reportOwnershipConflict(ctx, kn, obj, err)
		if kn.DeletionTimestamp.IsZero() && kn.ResourceVersion != "" {
			if statusErr := r.patchStatus(ctx, kn, func() {
				r.setPhase(kn, kubernautv1alpha2.PhaseError)
				meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
					Type: kubernautv1alpha2.ConditionServicesDeployed, Status: metav1.ConditionFalse,
					Reason: "OwnershipConflict", Message: conflict.Error(), ObservedGeneration: kn.Generation,
				})
			}); statusErr != nil {
				r.resourceLogger(ctx, kn, obj).Error(statusErr, "recording ownership conflict status failed")
			}
		}
		return conflict
	}
	return nil
}

func (r *KubernautReconciler) reportOwnershipConflict(ctx context.Context, kn *kubernautv1alpha2.Kubernaut, obj client.Object, err error) error {
	conflict := fmt.Errorf("ownership check for %s %s/%s: %w; resolve the name conflict or explicitly transfer ownership after review", r.resourceKind(ctx, obj), obj.GetNamespace(), obj.GetName(), err)
	r.resourceLogger(ctx, kn, obj).Error(conflict, "resource ownership conflict")
	if r.Recorder != nil {
		r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "OwnershipConflict", "Reconcile", "%v; existing owners=%v labels=%v", conflict, obj.GetOwnerReferences(), obj.GetLabels())
	}
	return conflict
}

func ownershipMetadataMatches(desired, existing client.Object) bool {
	for _, key := range []string{resources.AnnotationOwnerNamespace, resources.AnnotationOwnerUID} {
		if existing.GetAnnotations()[key] != desired.GetAnnotations()[key] {
			return false
		}
	}
	for _, key := range []string{"app.kubernetes.io/managed-by", "app.kubernetes.io/part-of", "app.kubernetes.io/instance"} {
		if existing.GetLabels()[key] != desired.GetLabels()[key] {
			return false
		}
	}
	return equality.Semantic.DeepEqual(desired.GetOwnerReferences(), existing.GetOwnerReferences())
}

// administratorWorkflowNamespaceReady is the read-only exception, not ownership
// authorization. Restricted PSA cannot override operator provenance or an owner.
func administratorWorkflowNamespaceReady(namespace *corev1.Namespace) bool {
	if !namespace.DeletionTimestamp.IsZero() || len(namespace.OwnerReferences) != 0 ||
		namespace.Labels["app.kubernetes.io/managed-by"] == "kubernaut-operator" ||
		namespace.Annotations[resources.AnnotationOwnerNamespace] != "" ||
		namespace.Annotations[resources.AnnotationOwnerUID] != "" || namespace.Labels == nil {
		return false
	}
	return !resources.EnsureRestrictedPSALabels(namespace.DeepCopy().Labels)
}

func (r *KubernautReconciler) resourceKind(ctx context.Context, obj client.Object) string {
	if kind := obj.GetObjectKind().GroupVersionKind().Kind; kind != "" {
		return kind
	}
	if r.Scheme != nil {
		gvks, _, err := r.Scheme.ObjectKinds(obj)
		if err != nil {
			logf.FromContext(ctx).Error(err, "resolving resource kind", "name", obj.GetName())
		} else if len(gvks) > 0 {
			return gvks[0].Kind
		}
	}
	return fmt.Sprintf("%T", obj)
}

func (r *KubernautReconciler) resourceLogger(ctx context.Context, kn *kubernautv1alpha2.Kubernaut, obj client.Object) logr.Logger {
	return logf.FromContext(ctx).WithValues(
		"kind", r.resourceKind(ctx, obj), "namespace", obj.GetNamespace(), "name", obj.GetName(),
		"ownerReferences", obj.GetOwnerReferences(), "ownershipLabels", obj.GetLabels(),
		"ownerNamespace", obj.GetAnnotations()[resources.AnnotationOwnerNamespace],
		"ownerUID", obj.GetAnnotations()[resources.AnnotationOwnerUID],
		"generation", kn.Generation, "resourceVersion", kn.ResourceVersion,
		"objectResourceVersion", obj.GetResourceVersion())
}

func (r *KubernautReconciler) ownershipReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// deleteObservedResource closes the read-authorize-delete race. Both identity
// and version are required: the same UID may have changed ownership meanwhile.
func (r *KubernautReconciler) deleteObservedResource(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	uid, version := obj.GetUID(), obj.GetResourceVersion()
	opts = append(opts, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	return r.Delete(ctx, obj, opts...)
}

// deleteObservedProviderPolicy is called only after the five-marker selector.
// The independent owner/scope/lifecycle veto still applies before deletion.
func (r *KubernautReconciler) deleteObservedProviderPolicy(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := logf.FromContext(ctx).WithValues("kind", obj.GetKind(), "namespace", obj.GetNamespace(),
		"name", obj.GetName(), "ownerReferences", obj.GetOwnerReferences(), "ownershipLabels", obj.GetLabels(),
		"objectResourceVersion", obj.GetResourceVersion())
	if err := providerPolicyOwnershipError(obj, obj); err != nil {
		logger.Error(err, "native policy preserved outside ownership contract")
		return nil //nolint:nilerr // diagnosed preservation is successful cleanup, not permission to delete
	}
	if err := client.IgnoreNotFound(r.deleteObservedResource(ctx, obj)); err != nil {
		logger.Error(err, "native policy deletion failed")
		return err
	}
	logger.Info("native policy deleted")
	return nil
}
