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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jordigilh/kubernaut-operator/test/utils"
)

const (
	// operatorNS is the namespace where the operator controller-manager runs.
	operatorNS = "kubernaut-operator-system"

	// crNS is the namespace where the Kubernaut CR and its workload resources live.
	crNS = "kubernaut-system"

	// workflowNS is the namespace the operator creates for workflow execution.
	workflowNS = "kubernaut-workflows"

	serviceAccountName     = "kubernaut-operator-controller-manager"
	metricsServiceName     = "kubernaut-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "kubernaut-operator-metrics-binding"
)

var _ = Describe("Kubernaut Operator E2E (OCP)", Ordered, func() {
	var controllerPodName string

	BeforeAll(func() {
		By("creating the CR namespace")
		cmd := exec.Command("kubectl", "create", "ns", crNS)
		_, _ = utils.Run(cmd) // ignore if already exists

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics",
			"-n", operatorNS, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)

		By("deleting Kubernaut CR if it still exists")
		cmd = exec.Command("kubectl", "delete", "kubernaut", "kubernaut",
			"-n", crNS, "--ignore-not-found=true", "--timeout=60s")
		_, _ = utils.Run(cmd)

		By("cleaning up BYO secrets")
		for _, s := range []string{"postgresql-secret", "valkey-secret", "llm-credentials"} {
			cmd = exec.Command("kubectl", "delete", "secret", s,
				"-n", crNS, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		}

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing CR namespace")
		cmd = exec.Command("kubectl", "delete", "ns", crNS, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			collectDiagnostic("kubectl", "logs", controllerPodName, "-n", operatorNS)
			collectDiagnostic("kubectl", "get", "events", "-n", operatorNS, "--sort-by=.lastTimestamp")
			collectDiagnostic("kubectl", "get", "events", "-n", crNS, "--sort-by=.lastTimestamp")
			collectDiagnostic("kubectl", "describe", "pod", controllerPodName, "-n", operatorNS)
		}
	})

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	// ── 1. Operator health ──────────────────────────────────────────────
	Context("Operator Pod", func() {
		It("should be running", func() {
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", operatorNS,
				)
				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				cmd = exec.Command("kubectl", "get", "pods", controllerPodName,
					"-o", "jsonpath={.status.phase}", "-n", operatorNS)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should serve the metrics endpoint", func() {
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=kubernaut-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", operatorNS, serviceAccountName))
			_, _ = utils.Run(cmd) // ignore if exists

			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", operatorNS)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", operatorNS)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"))
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", operatorNS,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {"drop": ["ALL"]},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {"type": "RuntimeDefault"}
							}
						}],
						"serviceAccount": "%s"
					}
				}`, token, metricsServiceName, operatorNS, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}", "-n", operatorNS)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"))
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring("controller_runtime_reconcile_total"))
		})
	})

	// ── 2. CR lifecycle: create → Running ───────────────────────────────
	Context("Kubernaut CR Lifecycle", func() {
		BeforeAll(func() {
			By("creating BYO prerequisite secrets")
			createBYOSecrets()

			By("applying the Kubernaut CR from config/samples")
			cmd := exec.Command("kubectl", "apply", "-f", "config/samples/v1alpha1_kubernaut.yaml")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Kubernaut CR")
		})

		It("should add the finalizer", func() {
			verifyFinalizer := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "kubernaut", "kubernaut",
					"-n", crNS, "-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("kubernaut.ai/cleanup"))
			}
			Eventually(verifyFinalizer).Should(Succeed())
		})

		It("should reach Running phase (all services deployed)", func() {
			verifyRunning := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "kubernaut", "kubernaut",
					"-n", crNS, "-o",
					`jsonpath={.status.conditions[?(@.type=="ServicesDeployed")].status}`)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"),
					"ServicesDeployed condition should be True")
			}
			Eventually(verifyRunning, 10*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should create all component Deployments and reach readiness", func() {
			deployments := []string{
				"gateway-deployment", "data-storage-deployment", "aianalysis-deployment",
				"signalprocessing-deployment", "remediationorchestrator-deployment",
				"workflowexecution-deployment", "effectivenessmonitor-deployment",
				"notification-deployment", "holmesgpt-api-deployment", "authwebhook-deployment",
			}
			for _, dep := range deployments {
				cmd := exec.Command("kubectl", "get", "deployment", dep,
					"-n", crNS, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Deployment %q should exist", dep)
				Expect(output).To(Equal(dep))

				readyCmd := exec.Command("kubectl", "get", "deployment", dep,
					"-n", crNS, "-o", "jsonpath={.status.readyReplicas}")
				readyOutput, err := utils.Run(readyCmd)
				Expect(err).NotTo(HaveOccurred(), "Deployment %q readyReplicas should be readable", dep)
				Expect(readyOutput).NotTo(Equal("0"),
					"Deployment %q should have at least one ready replica", dep)
				Expect(readyOutput).NotTo(BeEmpty(),
					"Deployment %q should have at least one ready replica", dep)
			}
		})

		It("should create Services for reachable components", func() {
			verifyService := func(name string) {
				cmd := exec.Command("kubectl", "get", "service", name,
					"-n", crNS, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Service %q should exist", name)
				Expect(output).To(Equal(name))
			}
			for _, svc := range []string{
				"gateway-service", "data-storage-service", "aianalysis-service",
				"signalprocessing-service", "remediationorchestrator-service",
				"workflowexecution-service", "effectivenessmonitor-service",
				"notification-service", "holmesgpt-api-service", "authwebhook-service",
			} {
				verifyService(svc)
			}
		})

		It("should create the OCP Route for Gateway", func() {
			cmd := exec.Command("kubectl", "get", "route", "gateway-route",
				"-n", crNS, "-o", "jsonpath={.spec.to.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Gateway Route should exist")
			Expect(output).To(Equal("gateway-service"))
		})

		It("should create ClusterRoles with kubernaut labels", func() {
			cmd := exec.Command("kubectl", "get", "clusterroles",
				"-l", "app.kubernetes.io/managed-by=kubernaut-operator",
				"-o", "go-template={{range .items}}{{.metadata.name}}{{\"\\n\"}}{{end}}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			lines := utils.GetNonEmptyLines(output)
			Expect(len(lines)).To(BeNumerically(">=", 13),
				"expected at least 13 ClusterRoles (base set)")
		})

		It("should create ClusterRoleBindings with kubernaut labels", func() {
			cmd := exec.Command("kubectl", "get", "clusterrolebindings",
				"-l", "app.kubernetes.io/managed-by=kubernaut-operator",
				"-o", "go-template={{range .items}}{{.metadata.name}}{{\"\\n\"}}{{end}}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			lines := utils.GetNonEmptyLines(output)
			Expect(len(lines)).To(BeNumerically(">=", 10),
				"expected at least 10 ClusterRoleBindings")
		})

		It("should create ServiceAccounts for all components", func() {
			serviceAccounts := []string{
				"gateway", "data-storage-sa", "aianalysis-controller",
				"signalprocessing-controller", "remediationorchestrator-controller",
				"workflowexecution-controller", "effectivenessmonitor-controller",
				"notification-controller", "holmesgpt-api-sa", "authwebhook",
			}
			for _, sa := range serviceAccounts {
				cmd := exec.Command("kubectl", "get", "serviceaccount", sa,
					"-n", crNS, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "SA %q should exist", sa)
				Expect(output).To(Equal(sa))
			}
		})

		It("should create the workflow namespace", func() {
			cmd := exec.Command("kubectl", "get", "namespace", workflowNS,
				"-o", "jsonpath={.metadata.labels.app\\.kubernetes\\.io/managed-by}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Workflow namespace should exist")
			Expect(output).To(Equal("kubernaut-operator"))
		})

		It("should create the workflow runner ServiceAccount in the workflow namespace", func() {
			cmd := exec.Command("kubectl", "get", "serviceaccount", "kubernaut-workflow-runner",
				"-n", workflowNS, "-o", "jsonpath={.metadata.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("kubernaut-workflow-runner"))
		})

		It("should create OCP service-CA annotated ConfigMaps when monitoring is enabled", func() {
			for _, cm := range []string{
				"effectivenessmonitor-service-ca",
				"holmesgpt-api-service-ca",
			} {
				verifyCM := func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "configmap", cm,
						"-n", crNS,
						"-o", "jsonpath={.metadata.annotations.service\\.beta\\.openshift\\.io/inject-cabundle}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(), "ConfigMap %q should exist", cm)
					g.Expect(output).To(Equal("true"))
				}
				Eventually(verifyCM).Should(Succeed())
			}
		})

		It("should create default policy ConfigMaps", func() {
			for _, pair := range []struct {
				name string
				key  string
			}{
				{"aianalysis-policies", "approval.rego"},
				{"signalprocessing-policy", "policy.rego"},
			} {
				cmd := exec.Command("kubectl", "get", "configmap", pair.name,
					"-n", crNS,
					"-o", fmt.Sprintf("jsonpath={.data.%s}", pair.key))
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "ConfigMap %q should exist", pair.name)
				Expect(output).NotTo(BeEmpty(),
					"ConfigMap %q should have key %q with content", pair.name, pair.key)
			}
		})

		It("should create the HolmesGPT client RoleBinding", func() {
			cmd := exec.Command("kubectl", "get", "rolebinding",
				"holmesgpt-api-client-aianalysis",
				"-n", crNS, "-o", "jsonpath={.roleRef.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "HolmesGPT client RoleBinding should exist")
			Expect(output).To(Equal(crNS + "-holmesgpt-api-client"))
		})
	})

	// ── 3. Deletion and cleanup ─────────────────────────────────────────
	Context("Kubernaut CR Deletion", func() {
		BeforeAll(func() {
			By("deleting the Kubernaut CR")
			cmd := exec.Command("kubectl", "delete", "kubernaut", "kubernaut",
				"-n", crNS, "--timeout=120s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete Kubernaut CR")
		})

		It("should clean up all ClusterRoles managed by the operator", func() {
			verifyCleanedUp("clusterroles")
		})

		It("should clean up all ClusterRoleBindings managed by the operator", func() {
			verifyCleanedUp("clusterrolebindings")
		})

		It("should clean up the workflow namespace", func() {
			verifyCleaned := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "namespace", workflowNS,
					"--ignore-not-found=true", "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(),
					"workflow namespace should be deleted")
			}
			Eventually(verifyCleaned, 2*time.Minute).Should(Succeed())
		})

		It("should clean up webhook configurations", func() {
			for _, wh := range []struct {
				kind string
				name string
			}{
				{"mutatingwebhookconfigurations", crNS + "-authwebhook-mutating"},
				{"validatingwebhookconfigurations", crNS + "-authwebhook-validating"},
			} {
				cmd := exec.Command("kubectl", "get", wh.kind, wh.name,
					"--ignore-not-found=true", "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
				Expect(output).To(BeEmpty(),
					"%s/%s should be deleted", wh.kind, wh.name)
			}
		})

		It("should remove the finalizer from the CR", func() {
			cmd := exec.Command("kubectl", "get", "kubernaut", "kubernaut",
				"-n", crNS, "--ignore-not-found=true", "-o", "jsonpath={.metadata.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(BeEmpty(), "Kubernaut CR should be fully deleted")
		})
	})
})

// createBYOSecrets creates the prerequisite Secrets the operator expects:
// postgresql, valkey, and LLM credentials.
func createBYOSecrets() {
	secrets := []struct {
		name string
		data map[string]string
	}{
		{
			name: "postgresql-secret",
			data: map[string]string{
				"POSTGRES_USER":     "kubernaut",
				"POSTGRES_PASSWORD": "test-pg-password",
				"POSTGRES_DB":       "kubernaut",
			},
		},
		{
			name: "valkey-secret",
			data: map[string]string{
				"valkey-secrets.yaml": "requirepass test-valkey-password",
			},
		},
		{
			name: "llm-credentials",
			data: map[string]string{
				"api-key": "sk-test-not-real-key-for-e2e",
			},
		},
	}

	for _, s := range secrets {
		args := []string{"create", "secret", "generic", s.name, "-n", crNS}
		for k, v := range s.data {
			args = append(args, fmt.Sprintf("--from-literal=%s=%s", k, v))
		}
		cmd := exec.Command("kubectl", args...)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create secret %q", s.name)
	}
}

// serviceAccountToken returns a token for the operator service account.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			operatorNS, serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves logs from the curl-metrics pod.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNS)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}


// kubectlGetLabeled runs a label-selector query and returns the output.
func kubectlGetLabeled(kind, label, gotemplate string) (string, error) {
	args := []string{"get", kind, "-l", label}
	if gotemplate != "" {
		args = append(args, "-o", fmt.Sprintf("go-template=%s", gotemplate))
	}
	return utils.Run(exec.Command("kubectl", args...))
}

// verifyCleanedUp polls until all operator-managed resources of the given kind
// are deleted.
func verifyCleanedUp(kind string) {
	Eventually(func(g Gomega) {
		output, err := kubectlGetLabeled(kind,
			"app.kubernetes.io/managed-by=kubernaut-operator",
			`{{range .items}}{{.metadata.name}}{{"\\n"}}{{end}}`)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(utils.GetNonEmptyLines(output)).To(BeEmpty(),
			"all operator-managed %s should be deleted", kind)
	}, 2*time.Minute).Should(Succeed())
}

// collectDiagnostic runs a command and writes its output to GinkgoWriter.
func collectDiagnostic(name string, args ...string) {
	cmd := exec.Command(name, args...)
	output, err := utils.Run(cmd)
	if err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== %s %v ===\n%s\n", name, args, output)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== %s %v FAILED: %v ===\n", name, args, err)
	}
}
