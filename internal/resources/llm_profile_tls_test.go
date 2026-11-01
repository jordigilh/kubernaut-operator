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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"sigs.k8s.io/yaml"
)

const testLLMTLSCAFile = "/etc/kubernaut-agent/llm-tls-client/ca.crt"

var _ = Describe("LLM profile TLS material (#532)", func() {
	It("UT-532-VAL-001 [SC-8, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] rejects an operator-owned CA reference without a rendered CA path", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles[testPrimaryProfile]
		profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "primary-ca"}
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile

		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("tlsCaFile")))
	})

	It("UT-532-VAL-002 [SC-12, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] rejects a CA Secret key containing a path separator", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles[testPrimaryProfile]
		profile.TLSCaFile = testLLMTLSCAFile
		profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "primary-ca", Key: "../ca.crt"}
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile

		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("tlsCaSecretRef.key")))
	})

	It("UT-532-VAL-003 [SC-12, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] rejects malformed CA paths and Secret names", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles[testPrimaryProfile]
		profile.TLSCaFile = "relative/ca.crt"
		profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "bad_name"}
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		errs := errorStrings(ValidateKubernaut(kn))
		Expect(errs).To(ContainElement(ContainSubstring("tlsCaFile must be an absolute path")))
		Expect(errs).To(ContainElement(ContainSubstring("tlsCaSecretRef.name must be a valid Kubernetes object name")))

		profile.TLSCaSecretRef.Name = ""
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("tlsCaSecretRef.name must not be empty")))

		profile.TLSCaSecretRef.Name = "primary-ca"
		profile.TLSCaFile = "/etc/llm/.."
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("must name a file below the deterministic TLS mount")))
	})

	It("UT-532-VAL-004 [SC-8, SI-10; SOC2 CC8.1; ASVS v5.0.0-V12.3.1] rejects CA and client paths that collide", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles[testPrimaryProfile]
		profile.TLSCaFile = "/etc/ca/ca.pem"
		profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "primary-ca"}
		profile.TLSCertFile = "/etc/client/ca.pem"
		profile.TLSKeyFile = "/etc/client/client.key"
		profile.TLSClientSecretRef = "primary-client"
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("must use different filenames for projected TLS material")))

		profile.TLSCaFile = profile.TLSCertFile
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("tlsCaFile and spec.llmProfiles[\"primary\"].tlsCertFile must be different paths")))

		profile.TLSCaFile = profile.TLSKeyFile
		kn.Spec.LLMProfiles[testPrimaryProfile] = profile
		Expect(errorStrings(ValidateKubernaut(kn))).To(ContainElement(ContainSubstring("tlsCaFile and spec.llmProfiles[\"primary\"].tlsKeyFile must be different paths")))
	})

	It("UT-532-CM-001 [SC-8, SC-12, CM-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V12.3.1, v5.0.0-V13.3.1] remaps a workflow CA path to its deterministic material mount", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["workflow-tls"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              LLMProviderAnthropic,
			Model:                 "claude-sonnet-4-6",
			CredentialsSecretName: "workflow-credentials",
			TLSCaFile:             "/custom/workflow-ca.pem",
			TLSCaSecretRef:        &kubernautv1alpha2.CACertSecretRef{Name: "workflow-ca"},
			TLSCertFile:           "/custom/workflow.crt",
			TLSKeyFile:            "/custom/workflow.key",
			TLSClientSecretRef:    "workflow-client",
		}
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = "workflow-tls"

		cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			PhaseModels map[string]llmPhaseOverrideYAML `yaml:"phaseModels"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())

		phase := root.PhaseModels["workflow_discovery"]
		Expect(phase.TLSCaFile).To(Equal("/etc/kubernaut-agent/workflow-tls/workflow-ca.pem"))
		Expect(phase.TLSCertFile).To(Equal("/etc/kubernaut-agent/workflow-tls/workflow.crt"))
		Expect(phase.TLSKeyFile).To(Equal("/etc/kubernaut-agent/workflow-tls/workflow.key"))
	})

	It("UT-532-CM-002 [SC-8, SC-12, CM-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V12.3.1, v5.0.0-V13.3.1] remaps an alignment CA path to its deterministic material mount", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "alignment-tls",
		}
		kn.Spec.LLMProfiles["alignment-tls"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              LLMProviderAnthropic,
			Model:                 "claude-sonnet-4-6",
			CredentialsSecretName: "alignment-credentials",
			TLSCaFile:             "/custom/alignment-ca.pem",
			TLSCaSecretRef:        &kubernautv1alpha2.CACertSecretRef{Name: "alignment-ca"},
			TLSCertFile:           "/custom/alignment.crt",
			TLSKeyFile:            "/custom/alignment.key",
			TLSClientSecretRef:    "alignment-client",
		}

		cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		var root kubernautAgentConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.AI.AlignmentCheck).NotTo(BeNil())
		Expect(root.AI.AlignmentCheck.LLM).NotTo(BeNil())
		Expect(root.AI.AlignmentCheck.LLM.TLSCaFile).To(Equal("/etc/kubernaut-agent/alignment-tls/alignment-ca.pem"))
		Expect(root.AI.AlignmentCheck.LLM.TLSCertFile).To(Equal("/etc/kubernaut-agent/alignment-tls/alignment.crt"))
		Expect(root.AI.AlignmentCheck.LLM.TLSKeyFile).To(Equal("/etc/kubernaut-agent/alignment-tls/alignment.key"))
	})

	It("UT-532-DEP-001 [SC-8, SC-12, AC-6, IA-5; SOC2 CC6.1, CC6.6; ASVS v5.0.0-V8.2.1, v5.0.0-V12.3.1] mounts an investigation CA-only Secret read-only", func() {
		kn := testKubernaut()
		mutateLLMProfile(kn, func(profile *kubernautv1alpha2.LLMProfileSpec) {
			profile.TLSCaFile = testLLMTLSCAFile
			profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "primary-ca"}
		})

		dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		volume := findLLMProfileTLSVolume(dep, "llm-tls-client")
		Expect(volume).NotTo(BeNil())
		Expect(volume.Projected).NotTo(BeNil())
		foundCA := false
		for _, source := range volume.Projected.Sources {
			if source.Secret == nil || source.Secret.Name != "primary-ca" {
				continue
			}
			foundCA = true
			Expect(source.Secret.Items).To(ConsistOf(corev1.KeyToPath{Key: "ca.crt", Path: "ca.crt"}))
		}
		Expect(foundCA).To(BeTrue(), "expected primary-ca in the projected investigation TLS volume")
		expectHasVolumeMount(dep, "llm-tls-client", "/etc/kubernaut-agent/llm-tls-client")
		for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == "llm-tls-client" {
				Expect(mount.ReadOnly).To(BeTrue())
			}
		}
	})

	It("UT-532-DEP-002 [SC-8, SC-12, AC-6, IA-5; SOC2 CC6.1, CC6.6; ASVS v5.0.0-V8.2.1, v5.0.0-V13.3.1] projects only each distinct workflow and alignment TLS source", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["workflow-tls"] = llmTLSProfileFor532("workflow", "workflow-ca", "workflow-client")
		kn.Spec.LLMProfiles["alignment-tls"] = llmTLSProfileFor532("alignment", "alignment-ca", "alignment-client")
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = "workflow-tls"
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{Enabled: true, LLMProfileRef: "alignment-tls"}

		dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		expectProjectedSecret(dep, workflowTLSClientVolumeName, "workflow-ca", "workflow-client")
		expectProjectedSecret(dep, alignmentTLSClientVolumeName, "alignment-ca", "alignment-client")
		for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == workflowTLSClientVolumeName || mount.Name == alignmentTLSClientVolumeName {
				Expect(mount.ReadOnly).To(BeTrue())
			}
		}
	})

	It("UT-532-DEP-003 [AC-6, SC-12; SOC2 CC6.6; ASVS v5.0.0-V8.2.1] reuses intentionally shared investigation TLS material", func() {
		kn := testKubernaut()
		mutateLLMProfile(kn, func(profile *kubernautv1alpha2.LLMProfileSpec) {
			profile.TLSCaFile = testLLMTLSCAFile
			profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "shared-ca"}
			profile.TLSCertFile = "/etc/kubernaut-agent/llm-tls-client/tls.crt"
			profile.TLSKeyFile = "/etc/kubernaut-agent/llm-tls-client/tls.key"
			profile.TLSClientSecretRef = "shared-client"
		})
		shared := kn.Spec.LLMProfiles[testPrimaryProfile]
		kn.Spec.LLMProfiles["workflow-shared"] = shared
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = "workflow-shared"

		dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		expectNoVolume(dep, workflowTLSClientVolumeName)
		expectHasVolume(dep, "llm-tls-client")

		cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			PhaseModels map[string]llmPhaseOverrideYAML `yaml:"phaseModels"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
		Expect(root.PhaseModels["workflow_discovery"].TLSCaFile).To(Equal(testLLMTLSCAFile))
	})

	It("UT-532-AF-CM-001 [SC-8, SC-12, AC-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V12.3.1, v5.0.0-V13.3.1] renders API Frontend profile TLS paths at mounted locations", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(profile *kubernautv1alpha2.LLMProfileSpec) {
			profile.TLSCaFile = "/custom/af-ca.pem"
			profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "af-ca"}
			profile.TLSCertFile = "/custom/af.crt"
			profile.TLSKeyFile = "/custom/af.key"
			profile.TLSClientSecretRef = "af-client"
		})

		cm, err := APIFrontendConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.TLSCaFile).To(Equal("/etc/apifrontend/llm-tls-client/af-ca.pem"))
		Expect(root.Agent.LLM.TLSCertFile).To(Equal("/etc/apifrontend/llm-tls-client/af.crt"))
		Expect(root.Agent.LLM.TLSKeyFile).To(Equal("/etc/apifrontend/llm-tls-client/af.key"))
	})

	It("UT-532-AF-DEP-001 [SC-8, SC-12, AC-6, IA-5; SOC2 CC6.1, CC6.6; ASVS v5.0.0-V8.2.1, v5.0.0-V13.3.1] mounts API Frontend profile TLS material read-only", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(profile *kubernautv1alpha2.LLMProfileSpec) {
			profile.TLSCaFile = "/custom/af-ca.pem"
			profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "af-ca"}
			profile.TLSCertFile = "/custom/af.crt"
			profile.TLSKeyFile = "/custom/af.key"
			profile.TLSClientSecretRef = "af-client"
		})

		dep, err := APIFrontendDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		expectProjectedSecret(dep, "llm-tls-client", "af-ca", "af-client")
	})

	It("UT-532-AF-CM-002 [SC-8, SC-12, AC-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V12.3.1, v5.0.0-V13.3.1] isolates severity-triage profile TLS paths", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-tls"] = llmTLSProfileFor532("triage", "triage-ca", "triage-client")
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-tls"}

		cm, err := APIFrontendConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.TLSCaFile).To(Equal("/etc/apifrontend/severity-triage-tls/triage-ca.pem"))

		dep, err := APIFrontendDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		expectProjectedSecret(dep, "severity-triage-llm-tls-client", "triage-ca", "triage-client")
	})
})

func llmTLSProfileFor532(prefix, caSecret, clientSecret string) kubernautv1alpha2.LLMProfileSpec {
	return kubernautv1alpha2.LLMProfileSpec{
		Provider:              LLMProviderAnthropic,
		Model:                 "claude-sonnet-4-6",
		CredentialsSecretName: prefix + "-credentials",
		TLSCaFile:             "/custom/" + prefix + "-ca.pem",
		TLSCaSecretRef:        &kubernautv1alpha2.CACertSecretRef{Name: caSecret},
		TLSCertFile:           "/custom/" + prefix + ".crt",
		TLSKeyFile:            "/custom/" + prefix + ".key",
		TLSClientSecretRef:    clientSecret,
	}
}

func findLLMProfileTLSVolume(dep *appsv1.Deployment, name string) *corev1.Volume {
	for i := range dep.Spec.Template.Spec.Volumes {
		if dep.Spec.Template.Spec.Volumes[i].Name == name {
			return &dep.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

func expectProjectedSecret(dep *appsv1.Deployment, volumeName, caSecret, clientSecret string) {
	volume := findLLMProfileTLSVolume(dep, volumeName)
	Expect(volume).NotTo(BeNil())
	Expect(volume.Projected).NotTo(BeNil())
	caFound := false
	clientFound := false
	for _, source := range volume.Projected.Sources {
		if source.Secret == nil {
			continue
		}
		switch source.Secret.Name {
		case caSecret:
			caFound = true
			Expect(source.Secret.Items).To(ContainElement(HaveField("Key", "ca.crt")))
		case clientSecret:
			clientFound = true
			Expect(source.Secret.Items).To(HaveLen(2))
			Expect(source.Secret.Items).To(ContainElement(HaveField("Key", corev1.TLSCertKey)))
			Expect(source.Secret.Items).To(ContainElement(HaveField("Key", corev1.TLSPrivateKeyKey)))
		}
	}
	Expect(caFound).To(BeTrue(), "expected CA Secret %q in projected volume %q", caSecret, volumeName)
	Expect(clientFound).To(BeTrue(), "expected client Secret %q in projected volume %q", clientSecret, volumeName)
}
