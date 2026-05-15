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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ServiceAccount", func() {
	Context("per component", func() {
		It("names, namespaces, and labels each service account", func() {
			kn := testKubernaut()
			for _, component := range AllComponents() {
				sa := ServiceAccount(kn, component)
				wantName := ServiceAccountName(component)
				Expect(sa.Name).To(Equal(wantName), "ServiceAccount(%q).Name = %q, want %q", component, sa.Name, wantName)
				Expect(sa.Namespace).To(Equal(kn.Namespace), "ServiceAccount(%q).Namespace = %q, want %q", component, sa.Namespace, kn.Namespace)
				Expect(sa.Labels["app"]).To(Equal(component), "ServiceAccount(%q).Labels[app] = %q, want %q", component, sa.Labels["app"], component)
			}
		})
	})

	Context("WorkflowRunnerServiceAccount", func() {
		It("uses default workflow namespace", func() {
			kn := testKubernaut()
			sa := WorkflowRunnerServiceAccount(kn)

			Expect(sa.Name).To(Equal("kubernaut-workflow-runner"))
			Expect(sa.Namespace).To(Equal("kubernaut-workflows"))
		})

		It("uses custom workflow namespace from spec", func() {
			kn := testKubernaut()
			kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf-ns"
			sa := WorkflowRunnerServiceAccount(kn)

			Expect(sa.Name).To(Equal("kubernaut-workflow-runner"))
			Expect(sa.Namespace).To(Equal("custom-wf-ns"))
		})
	})
})
