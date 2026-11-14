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
	"io/fs"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

var _ = Describe("EnsureCRDs embedded assets", func() {
	It("parses each embedded CRD YAML", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())

		var errs []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
			if err != nil {
				errs = append(errs, fmt.Sprintf("reading %s: %v", entry.Name(), err))
				continue
			}

			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := sigsyaml.Unmarshal(data, crd); err != nil {
				errs = append(errs, fmt.Sprintf("CRD %s fails to unmarshal: %v", entry.Name(), err))
				continue
			}

			if crd.Name == "" {
				errs = append(errs, fmt.Sprintf("CRD %s has no metadata.name", entry.Name()))
			}
			if crd.Spec.Group == "" {
				errs = append(errs, fmt.Sprintf("CRD %s has no spec.group", entry.Name()))
			}
		}
		Expect(errs).To(BeEmpty())
	})

	It("preserves fields through yamlToUnstructured JSON round-trip", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
			Expect(err).NotTo(HaveOccurred(), "reading %s", entry.Name())
			raw := string(data)

			obj, err := yamlToUnstructured(data)
			Expect(err).NotTo(HaveOccurred(), "yamlToUnstructured(%s)", entry.Name())

			roundTripped, err := obj.MarshalJSON()
			Expect(err).NotTo(HaveOccurred(), "re-marshalling %s", entry.Name())

			if strings.Contains(raw, "serviceAccountName") {
				Expect(string(roundTripped)).To(ContainSubstring("serviceAccountName"), "%s: raw YAML has serviceAccountName but yamlToUnstructured round-trip lost it", entry.Name())
			}

			Expect(obj.GetName()).NotTo(BeEmpty(), "%s has no metadata.name after yamlToUnstructured", entry.Name())
		}
	})

	It("embeds at least 9 CRD files", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())
		count := 0
		for _, e := range entries {
			if !e.IsDir() {
				count++
			}
		}
		Expect(count).To(BeNumerically(">=", 9))
	})

	It("contains the merged schema foundation contract", func() {
		aianalysis := embeddedCRDSchema("kubernaut.ai_aianalyses.yaml")
		agentSessionRef := schemaProperty(&aianalysis, "status", "investigationSession", "agentSessionRef")
		Expect(agentSessionRef.Properties).To(HaveKey("namespace"))
		Expect(agentSessionRef.Properties).To(HaveKey("uid"))
		Expect(agentSessionRef.Properties).To(HaveKey("name"))
		Expect(schemaEnumValues(schemaProperty(&aianalysis, "status", "phase"))).To(ContainElement("Cancelled"))

		Expect(schemaEnumValues(schemaProperty(embeddedCRDSchemaPointer("kubernaut.ai_effectivenessassessments.yaml"), "status", "phase"))).
			To(ContainElement("Cancelled"))
		Expect(schemaEnumValues(schemaProperty(embeddedCRDSchemaPointer("kubernaut.ai_remediationapprovalrequests.yaml"), "status", "decision"))).
			To(ContainElement("Cancelled"))

		failurePhase := schemaProperty(embeddedCRDSchemaPointer("kubernaut.ai_remediationrequests.yaml"), "status", "completionStatus", "failurePhase")
		Expect(failurePhase.AllOf).NotTo(BeEmpty())
		for _, branch := range failurePhase.AllOf {
			if len(branch.Enum) == 0 {
				continue
			}
			Expect(schemaEnumValues(branch)).To(ContainElement("EffectivenessAssessment"))
		}

		Expect(schemaEnumValues(schemaProperty(embeddedCRDSchemaPointer("kubernaut.ai_signalprocessings.yaml"), "status", "phase"))).
			To(ContainElement("Cancelled"))
		Expect(schemaEnumValues(schemaProperty(embeddedCRDSchemaPointer("kubernaut.ai_workflowexecutions.yaml"), "status", "phase"))).
			To(ContainElements("Cancelling", "Cancelled"))
	})

	It("contains the #2601 trusted-dispatch and approval asset contract", func() {
		// Keep this contract tied to the immutable upstream dependency in go.mod.
		// A local CRD patch would make this test pass while leaving the
		// dependency's other schemas stale.
		workflowCRD := embeddedCRD("kubernaut.ai_remediationworkflows.yaml")
		Expect(workflowCRD.Spec.Versions).To(HaveLen(1))
		Expect(workflowCRD.Spec.Versions[0].SelectableFields).To(ConsistOf(
			apiextensionsv1.SelectableField{JSONPath: ".status.workflowId"},
		))

		aiAnalysis := embeddedCRDSchema("kubernaut.ai_aianalyses.yaml")
		selectedWorkflow := schemaProperty(&aiAnalysis, "status", "rcaResult", "selectedWorkflow")
		Expect(selectedWorkflow.Properties).NotTo(HaveKey("parameterSchema"))
		selectedParameters := schemaProperty(&selectedWorkflow, "parameters")
		Expect(selectedParameters.Description).
			To(ContainSubstring("explicitly empty map is preserved"))
		Expect(selectedParameters.AdditionalProperties).NotTo(BeNil())
		Expect(selectedParameters.AdditionalProperties.Schema).NotTo(BeNil())
		Expect(selectedParameters.AdditionalProperties.Schema.Properties).To(HaveKey("kind"))
		Expect(selectedParameters.AdditionalProperties.Schema.Properties).To(HaveKey("stringValue"))
		validationRules := make([]string, 0, len(selectedWorkflow.XValidations))
		for _, validation := range selectedWorkflow.XValidations {
			validationRules = append(validationRules, validation.Rule)
		}
		Expect(validationRules).NotTo(ContainElement(ContainSubstring("parameterSchema")))

		approvalRequest := embeddedCRDSchema("kubernaut.ai_remediationapprovalrequests.yaml")
		approvalSpec := schemaProperty(&approvalRequest, "spec")
		Expect(approvalSpec.Properties).NotTo(HaveKey("parameterSchema"))
		Expect(approvalSpec.Properties).To(HaveKey("parameters"))
		Expect(approvalSpec.Properties).To(HaveKey("targetContext"))
		Expect(approvalSpec.Properties).To(HaveKey("workflow"))
		Expect(schemaProperty(&approvalRequest, "spec", "parameters").Description).
			To(ContainSubstring("distinct from missing data"))
		approvalParameters := schemaProperty(&approvalRequest, "spec", "parameters")
		Expect(approvalParameters.AdditionalProperties).NotTo(BeNil())
		Expect(approvalParameters.AdditionalProperties.Schema).NotTo(BeNil())
		Expect(approvalParameters.AdditionalProperties.Schema.Properties).To(HaveKey("kind"))
		Expect(schemaProperty(&approvalRequest, "spec", "targetContext").Properties).
			To(HaveKey("signalTarget"))
		workflowSnapshot := schemaProperty(&approvalRequest, "spec", "workflow")
		Expect(workflowSnapshot.Properties).To(HaveKey("actionType"))
		Expect(workflowSnapshot.Properties).To(HaveKey("workflowId"))
		Expect(workflowSnapshot.Properties).To(HaveKey("workflowName"))
		Expect(workflowSnapshot.Properties).To(HaveKey("executionBundle"))
		Expect(schemaProperty(&approvalRequest, "status", "workflowOverride", "parameters").Description).
			To(ContainSubstring("partial changes"))
		Expect(schemaProperty(&approvalRequest, "status", "workflowOverride", "workflowName").Description).
			To(ContainSubstring("legacy field"))

		workflowExecution := embeddedCRDSchema("kubernaut.ai_workflowexecutions.yaml")
		workflowReference := schemaProperty(&workflowExecution, "spec", "workflowRef")
		Expect(workflowReference.Properties).NotTo(HaveKey("declaredParameterNames"))
		workflowParameters := schemaProperty(&workflowExecution, "spec", "parameters")
		Expect(workflowParameters.AdditionalProperties).NotTo(BeNil())
		Expect(workflowParameters.AdditionalProperties.Schema).NotTo(BeNil())
		Expect(workflowParameters.AdditionalProperties.Schema.Properties).To(HaveKey("kind"))
	})
})

// embeddedCRDSchema loads the OpenAPI schema from an embedded CRD asset.
func embeddedCRDSchema(filename string) apiextensionsv1.JSONSchemaProps {
	return *embeddedCRDSchemaPointer(filename)
}

// embeddedCRDSchemaPointer loads the OpenAPI schema while preserving its nested property tree.
func embeddedCRDSchemaPointer(filename string) *apiextensionsv1.JSONSchemaProps {
	crd := embeddedCRD(filename)
	Expect(crd.Spec.Versions).NotTo(BeEmpty(), "%s has no versions", filename)
	Expect(crd.Spec.Versions[0].Schema).NotTo(BeNil(), "%s has no OpenAPI schema", filename)
	return crd.Spec.Versions[0].Schema.OpenAPIV3Schema
}

// embeddedCRD loads one operand CRD from the upstream shared asset filesystem.
func embeddedCRD(filename string) *apiextensionsv1.CustomResourceDefinition {
	data, err := fs.ReadFile(assets.CRDsFS, "crds/"+filename)
	Expect(err).NotTo(HaveOccurred(), "reading %s", filename)

	crd := &apiextensionsv1.CustomResourceDefinition{}
	Expect(sigsyaml.Unmarshal(data, crd)).NotTo(HaveOccurred(), "unmarshalling %s", filename)
	return crd
}

// schemaProperty returns the nested schema property at path.
func schemaProperty(schema *apiextensionsv1.JSONSchemaProps, path ...string) apiextensionsv1.JSONSchemaProps {
	current := schema
	for _, name := range path {
		next, ok := current.Properties[name]
		Expect(ok).To(BeTrue(), "missing schema property %q in %q", name, strings.Join(path, "."))
		current = &next
	}
	return *current
}

// schemaEnumValues decodes the string values in an OpenAPI enum.
func schemaEnumValues(schema apiextensionsv1.JSONSchemaProps) []string {
	values := make([]string, 0, len(schema.Enum))
	for _, enum := range schema.Enum {
		var value string
		Expect(sigsyaml.Unmarshal(enum.Raw, &value)).NotTo(HaveOccurred())
		values = append(values, value)
	}
	return values
}
