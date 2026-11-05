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

	rwv1alpha1 "github.com/jordigilh/kubernaut/api/remediationworkflow/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const remediationWorkflowCRDName = "remediationworkflows.kubernaut.ai"

var remediationWorkflowGVR = schema.GroupVersionResource{
	Group:    "kubernaut.ai",
	Version:  "v1alpha1",
	Resource: "remediationworkflows",
}

var _ = Describe("Issue #613 RemediationWorkflow selectable field", func() {
	ctx := context.Background()

	BeforeEach(func() {
		// Each scenario starts from a genuinely absent operand CRD. This keeps
		// the fresh-install and upgrade assertions independent of Ginkgo's
		// randomized spec order.
		deleteIssue613RemediationWorkflowCRD(ctx)
	})

	It("IT-SCHEMA-613-001 installs the selector-enabled CRD from upstream assets", func() {
		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())

		crd := waitForIssue613SelectorCRD(ctx)
		Expect(crd.Spec.Versions).To(HaveLen(1))
		Expect(crd.Spec.Versions[0].SelectableFields).To(ConsistOf(
			apiextensionsv1.SelectableField{JSONPath: ".status.workflowId"},
		))
	})

	It("IT-SCHEMA-613-002 upgrades a pre-#2601 CRD to the selector-enabled schema", func() {
		legacy := embeddedOperandCRD("kubernaut.ai_remediationworkflows.yaml")
		legacy.Spec.Versions[0].SelectableFields = nil
		legacy.Labels = map[string]string{
			"app.kubernetes.io/managed-by": "kubernaut-operator",
		}
		legacy.Annotations = map[string]string{
			"issue-613-preserved-annotation": "true",
		}
		Expect(k8sClient.Create(ctx, legacy)).To(Succeed())

		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())

		crd := waitForIssue613SelectorCRD(ctx)
		Expect(crd.Spec.Versions[0].SelectableFields).To(ConsistOf(
			apiextensionsv1.SelectableField{JSONPath: ".status.workflowId"},
		))
		Expect(crd.Annotations).To(HaveKeyWithValue("issue-613-preserved-annotation", "true"))
	})

	It("IT-SCHEMA-613-003 lists only the namespace-scoped workflow matching status.workflowId", func() {
		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())
		waitForIssue613SelectorCRD(ctx)

		workflows := issue613RemediationWorkflowResource()
		createIssue613Workflow(ctx, workflows, "issue-613-workflow-match", "workflow-id-match")
		createIssue613Workflow(ctx, workflows, "issue-613-workflow-other", "workflow-id-other")

		matched, err := workflows.List(ctx, metav1.ListOptions{FieldSelector: "status.workflowId=workflow-id-match"})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched.Items).To(HaveLen(1))
		Expect(matched.Items[0].GetName()).To(Equal("issue-613-workflow-match"))
	})
})

func deleteIssue613RemediationWorkflowCRD(ctx context.Context) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := k8sClient.Get(ctx, clientObjectKey(remediationWorkflowCRDName), crd)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, crd)).To(Succeed())

	Eventually(func() bool {
		current := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, clientObjectKey(remediationWorkflowCRDName), current)
		return apierrors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue(), "RemediationWorkflow CRD should be deleted before the scenario")
}

func waitForIssue613SelectorCRD(ctx context.Context) *apiextensionsv1.CustomResourceDefinition {
	var current apiextensionsv1.CustomResourceDefinition
	Eventually(func() error {
		if err := k8sClient.Get(ctx, clientObjectKey(remediationWorkflowCRDName), &current); err != nil {
			return err
		}
		if len(current.Spec.Versions) != 1 {
			return fmt.Errorf("expected one RemediationWorkflow version, got %d", len(current.Spec.Versions))
		}
		if len(current.Spec.Versions[0].SelectableFields) != 1 ||
			current.Spec.Versions[0].SelectableFields[0].JSONPath != ".status.workflowId" {
			return fmt.Errorf("RemediationWorkflow CRD selector is not established")
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return nil
			}
		}
		return fmt.Errorf("RemediationWorkflow CRD is not established")
	}, timeout, interval).Should(Succeed())
	return current.DeepCopy()
}

func issue613RemediationWorkflowResource() dynamic.ResourceInterface {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	return dynamicClient.Resource(remediationWorkflowGVR).Namespace(testNamespace)
}

func createIssue613Workflow(
	ctx context.Context,
	workflows dynamic.ResourceInterface,
	name string,
	workflowID string,
) {
	workflow := &rwv1alpha1.RemediationWorkflow{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubernaut.ai/v1alpha1",
			Kind:       "RemediationWorkflow",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: rwv1alpha1.RemediationWorkflowSpec{
			Version:    "1.0.0",
			ActionType: "RestartPod",
			Description: rwv1alpha1.RemediationWorkflowDescription{
				What:      "Restart a pod",
				WhenToUse: "A pod is unhealthy",
			},
			Labels: rwv1alpha1.RemediationWorkflowLabels{
				Severity:    []string{"critical"},
				Environment: []string{"production"},
				Component:   []string{"Pod"},
				Priority:    "P1",
			},
			Execution: rwv1alpha1.RemediationWorkflowExecution{
				Engine: "job",
				Bundle: "quay.io/kubernaut-ai/remediation-workflow:test",
			},
			Parameters: []rwv1alpha1.RemediationWorkflowParameter{{
				Name:        "target",
				Type:        "string",
				Required:    true,
				Description: "Pod name",
			}},
		},
	}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workflow)
	Expect(err).NotTo(HaveOccurred())
	created, err := workflows.Create(ctx, &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	status := created.DeepCopy()
	status.Object["status"] = map[string]interface{}{
		"workflowId":    workflowID,
		"catalogStatus": "Active",
		"registeredBy":  "issue-613-integration-test",
	}
	_, err = workflows.UpdateStatus(ctx, status, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

// clientObjectKey avoids importing controller-runtime's client package only
// for the two cluster-scoped CRD lookups in this integration contract.
func clientObjectKey(name string) types.NamespacedName {
	return types.NamespacedName{Name: name}
}
