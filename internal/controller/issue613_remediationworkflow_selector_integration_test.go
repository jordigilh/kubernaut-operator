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
	corev1 "k8s.io/api/core/v1"
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

const (
	remediationWorkflowCRDName       = "remediationworkflows.kubernaut.ai"
	issue613SelectorOtherNamespace   = "issue-613-selector-other"
	issue613AIAnalysisOmittedName    = "issue-613-aia-omitted"
	issue613AIAnalysisEmptyName      = "issue-613-aia-empty"
	issue613ApprovalRequestOmitted   = "issue-613-rar-omitted"
	issue613ApprovalRequestEmpty     = "issue-613-rar-empty"
	issue613WorkflowExecutionOmitted = "issue-613-we-omitted"
	issue613WorkflowExecutionEmpty   = "issue-613-we-empty"
)

var remediationWorkflowGVR = schema.GroupVersionResource{
	Group:    "kubernaut.ai",
	Version:  "v1alpha1",
	Resource: "remediationworkflows",
}

var (
	aiAnalysisGVR = schema.GroupVersionResource{
		Group:    "kubernaut.ai",
		Version:  "v1alpha1",
		Resource: "aianalyses",
	}
	remediationApprovalRequestGVR = schema.GroupVersionResource{
		Group:    "kubernaut.ai",
		Version:  "v1alpha1",
		Resource: "remediationapprovalrequests",
	}
	workflowExecutionGVR = schema.GroupVersionResource{
		Group:    "kubernaut.ai",
		Version:  "v1alpha1",
		Resource: "workflowexecutions",
	}
)

var (
	issue613EmptyParameters         = map[string]interface{}{}
	issue613ParameterPath           = []string{"spec", "parameters"}
	issue613AIAnalysisParameterPath = []string{"status", "rcaResult", "selectedWorkflow", "parameters"}
)

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
			resources.AnnotationSpecHash:     "pre-2601-spec-hash",
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
		ensureIssue613Namespace(ctx, issue613SelectorOtherNamespace)

		createIssue613Workflow(ctx, testNamespace, "issue-613-workflow-match", "workflow-id-match")
		createIssue613Workflow(ctx, testNamespace, "issue-613-workflow-other", "workflow-id-other")
		createIssue613Workflow(ctx, issue613SelectorOtherNamespace, "issue-613-workflow-cross-namespace", "workflow-id-match")

		workflows := issue613RemediationWorkflowResource(testNamespace)
		matched, err := workflows.List(ctx, metav1.ListOptions{FieldSelector: "status.workflowId=workflow-id-match"})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched.Items).To(HaveLen(1))
		Expect(matched.Items[0].GetName()).To(Equal("issue-613-workflow-match"))
	})

	It("IT-SCHEMA-613-004 preserves omitted and explicit-empty parameter maps through API-server persistence", func() {
		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())
		waitForIssue613SelectorCRD(ctx)

		assertIssue613AIAnalysisParameterRoundTrip(ctx)
		assertIssue613SpecParameterRoundTrip(
			ctx,
			issue613Resource(remediationApprovalRequestGVR, testNamespace),
			issue613RemediationApprovalRequest(issue613ApprovalRequestOmitted, nil),
			issue613RemediationApprovalRequest(issue613ApprovalRequestEmpty, issue613EmptyParameters),
		)
		assertIssue613SpecParameterRoundTrip(
			ctx,
			issue613Resource(workflowExecutionGVR, testNamespace),
			issue613WorkflowExecution(issue613WorkflowExecutionOmitted, nil),
			issue613WorkflowExecution(issue613WorkflowExecutionEmpty, issue613EmptyParameters),
		)
	})
})

// deleteIssue613RemediationWorkflowCRD removes the shared CRD before each
// scenario so fresh-install and upgrade behavior are independently observable.
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

// waitForIssue613SelectorCRD waits for the API server to establish the
// upstream CRD and publish its .status.workflowId field selector.
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

// ensureIssue613Namespace creates the second namespace used to prove that a
// namespaced field-selector list does not cross namespace boundaries.
func ensureIssue613Namespace(ctx context.Context, name string) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Get(ctx, clientObjectKey(name), namespace)
	if apierrors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
}

// issue613RemediationWorkflowResource returns a dynamic client scoped to the
// namespace under test, making the request boundary explicit.
func issue613RemediationWorkflowResource(namespace string) dynamic.ResourceInterface {
	return issue613Resource(remediationWorkflowGVR, namespace)
}

// issue613Resource returns a dynamic namespaced resource client for an
// upstream v1alpha1 operand CRD.
func issue613Resource(gvr schema.GroupVersionResource, namespace string) dynamic.ResourceInterface {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	return dynamicClient.Resource(gvr).Namespace(namespace)
}

// createIssue613Workflow persists a valid workflow with the supplied namespace
// and status identifier, allowing the selector test to prove request scoping.
func createIssue613Workflow(
	ctx context.Context,
	namespace string,
	name string,
	workflowID string,
) {
	workflows := issue613RemediationWorkflowResource(namespace)
	workflow := &rwv1alpha1.RemediationWorkflow{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubernaut.ai/v1alpha1",
			Kind:       "RemediationWorkflow",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

// assertIssue613AIAnalysisParameterRoundTrip verifies that AIAnalysis status
// keeps nil and an explicitly empty parameters map distinguishable.
func assertIssue613AIAnalysisParameterRoundTrip(ctx context.Context) {
	resource := issue613Resource(aiAnalysisGVR, testNamespace)
	omitted := createIssue613ParameterObject(ctx, resource, issue613AIAnalysis(issue613AIAnalysisOmittedName))
	explicit := createIssue613ParameterObject(ctx, resource, issue613AIAnalysis(issue613AIAnalysisEmptyName))

	updateIssue613AIAnalysisStatus(ctx, resource, omitted, nil)
	updateIssue613AIAnalysisStatus(ctx, resource, explicit, issue613EmptyParameters)

	assertIssue613ParameterState(ctx, resource, omitted.GetName(), false, issue613AIAnalysisParameterPath...)
	assertIssue613ParameterState(ctx, resource, explicit.GetName(), true, issue613AIAnalysisParameterPath...)
}

// assertIssue613SpecParameterRoundTrip verifies that a spec-level parameters
// map survives API-server storage in both omitted and explicitly empty forms.
func assertIssue613SpecParameterRoundTrip(
	ctx context.Context,
	resource dynamic.ResourceInterface,
	omitted *unstructured.Unstructured,
	explicit *unstructured.Unstructured,
) {
	createIssue613ParameterObject(ctx, resource, omitted)
	createIssue613ParameterObject(ctx, resource, explicit)

	assertIssue613ParameterState(ctx, resource, omitted.GetName(), false, issue613ParameterPath...)
	assertIssue613ParameterState(ctx, resource, explicit.GetName(), true, issue613ParameterPath...)
}

// createIssue613ParameterObject persists one fixture and registers cleanup
// without hiding API-server admission or storage errors.
func createIssue613ParameterObject(
	ctx context.Context,
	resource dynamic.ResourceInterface,
	object *unstructured.Unstructured,
) *unstructured.Unstructured {
	created, err := resource.Create(ctx, object, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "creating %s", object.GetName())
	DeferCleanup(func() {
		Expect(resource.Delete(ctx, created.GetName(), metav1.DeleteOptions{})).To(Succeed())
	})
	return created
}

// assertIssue613ParameterState reads a persisted object and checks whether the
// requested field is absent or remains an empty JSON object.
func assertIssue613ParameterState(
	ctx context.Context,
	resource dynamic.ResourceInterface,
	name string,
	wantPresent bool,
	path ...string,
) {
	persisted, err := resource.Get(ctx, name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "reading %s", name)
	value, found, err := unstructured.NestedFieldNoCopy(persisted.Object, path...)
	Expect(err).NotTo(HaveOccurred(), "parameter field must be readable for %s", name)
	Expect(found).To(Equal(wantPresent), "parameter field presence for %s", name)
	if wantPresent {
		Expect(value).To(Equal(map[string]interface{}{}), "explicit empty parameters for %s", name)
	}
}

// updateIssue613AIAnalysisStatus writes the selected-workflow status fixture
// through the real status subresource before the persistence assertion.
func updateIssue613AIAnalysisStatus(
	ctx context.Context,
	resource dynamic.ResourceInterface,
	object *unstructured.Unstructured,
	parameters map[string]interface{},
) {
	status := object.DeepCopy()
	selectedWorkflow := map[string]interface{}{
		"workflowId":      "workflow-id-roundtrip",
		"workflowName":    "restart-pod",
		"actionType":      "RestartPod",
		"version":         "1.0.0",
		"executionBundle": "quay.io/kubernaut-ai/remediation-workflow:test",
		"confidence":      0.9,
		"rationale":       "issue-613 persistence fixture",
	}
	if parameters != nil {
		selectedWorkflow["parameters"] = parameters
	}
	status.Object["status"] = map[string]interface{}{
		"phase": "Completed",
		"rcaResult": map[string]interface{}{
			"selectedWorkflow": selectedWorkflow,
		},
	}
	_, err := resource.UpdateStatus(ctx, status, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "updating AIAnalysis status for %s", object.GetName())
}

// issue613AIAnalysis returns the minimal valid spec needed to exercise the
// selected-workflow status parameters field on a real API server.
func issue613AIAnalysis(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubernaut.ai/v1alpha1",
		"kind":       "AIAnalysis",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": testNamespace,
		},
		"spec": map[string]interface{}{
			"remediationRequestRef": map[string]interface{}{
				"name":      "issue-613-remediation-request",
				"namespace": testNamespace,
			},
			"remediationId": name,
			"analysisRequest": map[string]interface{}{
				"analysisTypes": []interface{}{"Investigation"},
				"signalContext": map[string]interface{}{
					"fingerprint":      name,
					"severity":         "critical",
					"signalName":       "Issue613TestSignal",
					"signalMode":       "reactive",
					"environment":      "production",
					"businessPriority": "P1",
					"targetResource": map[string]interface{}{
						"kind":      "Pod",
						"name":      "issue-613-test-pod",
						"namespace": testNamespace,
					},
					"enrichmentResults": map[string]interface{}{},
				},
			},
		},
	}}
}

// issue613RemediationApprovalRequest returns a minimal valid RAR with an
// optional spec.parameters field in the requested nil-or-empty form.
func issue613RemediationApprovalRequest(name string, parameters map[string]interface{}) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"remediationRequestRef": map[string]interface{}{
			"name":      "issue-613-remediation-request",
			"namespace": testNamespace,
		},
		"aiAnalysisRef": map[string]interface{}{
			"name": "issue-613-aia-source",
		},
		"confidence":      0.72,
		"confidenceLevel": "medium",
		"reason":          "Issue #613 persistence fixture",
		"recommendedWorkflow": map[string]interface{}{
			"workflowId":      "workflow-id-roundtrip",
			"version":         "1.0.0",
			"executionBundle": "quay.io/kubernaut-ai/remediation-workflow:test",
			"rationale":       "Issue #613 persistence fixture",
		},
		"investigationSummary": "Issue #613 persistence fixture",
		"recommendedActions": []interface{}{
			map[string]interface{}{
				"action":    "RestartPod",
				"rationale": "Issue #613 persistence fixture",
			},
		},
		"whyApprovalRequired": "Issue #613 persistence fixture",
		"requiredBy":          "2099-01-01T00:00:00Z",
	}
	if parameters != nil {
		spec["parameters"] = parameters
	}
	return issue613NamespacedObject("RemediationApprovalRequest", name, spec)
}

// issue613WorkflowExecution returns a minimal valid WE with an optional
// spec.parameters field in the requested nil-or-empty form.
func issue613WorkflowExecution(name string, parameters map[string]interface{}) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"remediationRequestRef": map[string]interface{}{
			"name":      "issue-613-remediation-request",
			"namespace": testNamespace,
		},
		"workflowRef": map[string]interface{}{
			"workflowId":      "workflow-id-roundtrip",
			"workflowName":    "restart-pod",
			"actionType":      "RestartPod",
			"version":         "1.0.0",
			"executionBundle": "quay.io/kubernaut-ai/remediation-workflow:test",
		},
		"targetResource": "default/Pod/issue-613-test-pod",
	}
	if parameters != nil {
		spec["parameters"] = parameters
	}
	return issue613NamespacedObject("WorkflowExecution", name, spec)
}

// issue613NamespacedObject builds an unstructured namespaced fixture for one
// of the upstream v1alpha1 operand CRDs.
func issue613NamespacedObject(kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubernaut.ai/v1alpha1",
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": testNamespace,
		},
		"spec": spec,
	}}
}

// clientObjectKey avoids importing controller-runtime's client package only
// for the two cluster-scoped CRD lookups in this integration contract.
func clientObjectKey(name string) types.NamespacedName {
	return types.NamespacedName{Name: name}
}
