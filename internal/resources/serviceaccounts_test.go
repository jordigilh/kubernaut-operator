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
	"testing"
)

func TestServiceAccount_PerComponent(t *testing.T) {
	kn := testKubernaut()
	for _, component := range AllComponents() {
		sa := ServiceAccount(kn, component)
		wantName := ServiceAccountName(component)
		if sa.Name != wantName {
			t.Errorf("ServiceAccount(%q).Name = %q, want %q", component, sa.Name, wantName)
		}
		if sa.Namespace != kn.Namespace {
			t.Errorf("ServiceAccount(%q).Namespace = %q, want %q", component, sa.Namespace, kn.Namespace)
		}
		if got := sa.Labels["app"]; got != component {
			t.Errorf("ServiceAccount(%q).Labels[app] = %q, want %q", component, got, component)
		}
	}
}

func TestWorkflowRunnerServiceAccount_DefaultNamespace(t *testing.T) {
	kn := testKubernaut()
	sa := WorkflowRunnerServiceAccount(kn)

	if sa.Name != "kubernaut-workflow-runner" {
		t.Errorf("Name = %q, want %q", sa.Name, "kubernaut-workflow-runner")
	}
	if sa.Namespace != "kubernaut-workflows" {
		t.Errorf("Namespace = %q, want %q", sa.Namespace, "kubernaut-workflows")
	}
}

func TestWorkflowRunnerServiceAccount_CustomNamespace(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf-ns"
	sa := WorkflowRunnerServiceAccount(kn)

	if sa.Name != "kubernaut-workflow-runner" {
		t.Errorf("Name = %q, want %q", sa.Name, "kubernaut-workflow-runner")
	}
	if sa.Namespace != "custom-wf-ns" {
		t.Errorf("Namespace = %q, want %q", sa.Namespace, "custom-wf-ns")
	}
}
