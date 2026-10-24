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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	defaultMustGatherImage = "quay.io/kubernaut-ai/must-gather:latest"
)

var _ = Describe("Kubernaut Operator E2E (OCP)", Ordered, func() {
	var controllerPodName string

	BeforeAll(func() {
		By("creating the CR namespace")
		cmd := exec.Command("kubectl", "create", "ns", crNS)
		_, _ = utils.Run(cmd) // ignore if already exists

		By("installing the disposable PostgreSQL and Valkey bootstrap")
		installDevelopmentBootstrap()

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
		if preserveE2ENamespaces() {
			_, _ = fmt.Fprintf(GinkgoWriter, "preserving E2E namespaces %q and %q for failure investigation\n", operatorNS, crNS)
			return
		}

		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics",
			"-n", operatorNS, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)

		By("deleting Kubernaut CR if it still exists")
		cmd = exec.Command("kubectl", "delete", "kubernaut", "kubernaut",
			"-n", crNS, "--ignore-not-found=true", "--timeout=60s")
		_, _ = utils.Run(cmd)

		By("uninstalling the disposable PostgreSQL and Valkey bootstrap")
		cmd = exec.Command("helm", "uninstall", "kubernaut-dev-bootstrap",
			"-n", crNS, "--wait")
		_, _ = utils.Run(cmd)

		By("removing the test-owned audit key protection finalizer")
		cmd = exec.Command("kubectl", "patch", "secret", "datastorage-audit-hmac-key",
			"-n", crNS, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "patch", "configmap", "datastorage-audit-hmac-key-state",
			"-n", crNS, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		_, _ = utils.Run(cmd)

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
			collectMustGather()
			collectDiagnostic("logs", controllerPodName, "-n", operatorNS)
			collectDiagnostic("get", "events", "-n", operatorNS, "--sort-by=.lastTimestamp")
			collectDiagnostic("get", "events", "-n", crNS, "--sort-by=.lastTimestamp")
			collectDiagnostic("describe", "pod", controllerPodName, "-n", operatorNS)
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
			By("applying the Kubernaut CR from config/samples")
			cmd := exec.Command("kubectl", "apply", "-f", "config/samples/v1alpha2_kubernaut.yaml")
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
				"gateway", "data-storage", "aianalysis-controller",
				"signalprocessing-controller", "remediationorchestrator-controller",
				"workflowexecution-controller", "effectivenessmonitor-controller",
				"notification-controller", "kubernaut-agent", "authwebhook",
			}
			for _, dep := range deployments {
				cmd := exec.Command("kubectl", "get", "deployment", dep,
					"-n", crNS, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Deployment %q should exist", dep)
				Expect(output).To(Equal(dep))

				Eventually(func(g Gomega) {
					readyCmd := exec.Command("kubectl", "get", "deployment", dep,
						"-n", crNS, "-o", "jsonpath={.status.readyReplicas}")
					readyOutput, getErr := utils.Run(readyCmd)
					g.Expect(getErr).NotTo(HaveOccurred(),
						"Deployment %q readyReplicas should be readable", dep)
					g.Expect(readyOutput).NotTo(BeEmpty(),
						"Deployment %q should have at least one ready replica", dep)
					g.Expect(readyOutput).NotTo(Equal("0"),
						"Deployment %q should have at least one ready replica", dep)
				}, 10*time.Minute, 5*time.Second).Should(Succeed())
			}
		})

		It("E2E-TLS-2545-OP-001 [AC-6, CM-7, SC-8; SOC2 CC7.2; "+
			"ASVS v5.0.0-V12.3.1, v5.0.0-V15.2.5] uses upstream ambient CA ownership "+
			"without legacy static env or ambient-only volumes", func() {
			for _, dep := range []string{
				"gateway", "data-storage", "aianalysis-controller",
				"signalprocessing-controller", "remediationorchestrator-controller",
				"workflowexecution-controller", "effectivenessmonitor-controller",
				"notification-controller", "kubernaut-agent", "authwebhook",
			} {
				cmd := exec.Command("kubectl", "get", "deployment", dep, "-n", crNS, "-o", "yaml")
				manifest, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "Deployment %q should be readable", dep)
				Expect(manifest).NotTo(ContainSubstring("name: SSL_CERT_FILE"), dep)
				Expect(manifest).NotTo(ContainSubstring("name: SSL_CERT_DIR"), dep)
				Expect(manifest).NotTo(ContainSubstring("name: TLS_CA_FILE"), dep)
				Expect(manifest).NotTo(ContainSubstring("name: ambient-tmp"), dep)
				Expect(manifest).NotTo(ContainSubstring("name: combined-ca"), dep)
			}
		})

		It("E2E-NOT-SECRET-001 [IA-5, SC-28, CM-6; SOC2 CC6.1, CC6.6, CC8.1; "+
			"ASVS v5.0.0-V8.2.1, v5.0.0-V16.5.2] projects the provider-neutral "+
			"Notification credential and preserves its routing reference", func() {
			By("checking the namespace-local Secret projection")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "notification-controller",
					"-n", crNS,
					"-o", "yaml")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("name: slack-webhook"))
				g.Expect(output).To(ContainSubstring("name: notification-teams"))
				g.Expect(output).To(ContainSubstring("name: notification-pagerduty"))
				g.Expect(output).To(ContainSubstring("mountPath: /etc/notification/credentials"))
				g.Expect(output).To(ContainSubstring("readOnly: true"))
			}, 5*time.Minute, 2*time.Second).Should(Succeed())

			By("checking the explicit routing credential reference")
			cmd := exec.Command("kubectl", "get", "configmap", "notification-routing",
				"-n", crNS, "-o", "jsonpath={.data.routing\\.yaml}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("credentialRef: slack-ops"))
			Expect(output).To(ContainSubstring("credentialRef: teams-primary"))
			Expect(output).To(ContainSubstring("credentialRef: pagerduty-critical"))
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
				"signalprocessing-controller-metrics", "remediationorchestrator-controller",
				"workflowexecution-controller-metrics", "effectivenessmonitor-metrics",
				"notification-metrics", "kubernaut-agent", "authwebhook-service",
			} {
				verifyService(svc)
			}
		})

		It("E2E-EXP-565-001 [SC-7, SC-8, SI-4; SOC2 CC6.1, CC7.2; "+
			"ASVS v5.0.0-V12.2.1, v5.0.0-V16.5.2] keeps Gateway internal unless "+
			"an administrator creates external exposure", func() {
			serviceCmd := exec.Command("kubectl", "get", "service", "gateway-service", "-n", crNS, "-o", "jsonpath={.spec.type}")
			serviceType, err := utils.Run(serviceCmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(serviceType).To(Equal("ClusterIP"))

			routeCmd := exec.Command("kubectl", "get", "route", "gateway-route", "-n", crNS,
				"--ignore-not-found=true", "-o", "jsonpath={.metadata.name}")
			routeName, err := utils.Run(routeCmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(routeName).To(BeEmpty(), "the operator must not create a Gateway Route")
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
				"notification-controller", "kubernaut-agent-sa", "authwebhook",
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
				"kubernaut-agent-service-ca",
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

		It(
			"E2E-MON-563-001 [SC-8, SC-13, SI-4; SOC2 CC7.2; "+
				"ASVS V12.3.1, V12.3.2, V13.3.4] verifies monitoring HTTPS with the injected service CA",
			func() {
				const (
					probeName                    = "effectivenessmonitor-monitoring-tls-probe"
					probeServiceAccountName      = "monitoring-tls-probe"
					probeMonitoringBindingName   = "monitoring-tls-probe-monitoring-view"
					probeAlertmanagerBindingName = "monitoring-tls-probe-alertmanager-view"
					probeImage                   = "docker.io/curlimages/curl@sha256:" +
						"ce8e655b562cf387752de7d2076ec3f81a4383c908f386b6724ec3571ce08b4a"
				)
				Eventually(func(g Gomega) {
					cmd := exec.Command(
						"kubectl", "get", "kubernaut", "kubernaut", "-n", crNS,
						"-o", `jsonpath={.status.conditions[?(@.type=="ServicesDeployed")].status}`,
					)
					status, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status).To(Equal("True"))
				}, 10*time.Minute, 5*time.Second).Should(Succeed())
				Eventually(func(g Gomega) {
					cmd := exec.Command(
						"kubectl", "get", "serviceaccount", "effectivenessmonitor-controller",
						"-n", crNS, "-o", "jsonpath={.metadata.name}",
					)
					name, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(name).To(Equal("effectivenessmonitor-controller"))
				}, 5*time.Minute, 2*time.Second).Should(Succeed())
				Eventually(func(g Gomega) {
					cmd := exec.Command(
						"kubectl", "get", "configmap", "effectivenessmonitor-service-ca",
						"-n", crNS, "-o", "jsonpath={.metadata.name}",
					)
					name, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(name).To(Equal("effectivenessmonitor-service-ca"))
				}, 5*time.Minute, 2*time.Second).Should(Succeed())
				cmd := exec.Command("kubectl", "create", "serviceaccount", probeServiceAccountName, "-n", crNS)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					for _, resource := range []string{
						"clusterrolebinding/" + probeMonitoringBindingName,
						"clusterrolebinding/" + probeAlertmanagerBindingName,
					} {
						cmd := exec.Command("kubectl", "delete", resource, "--ignore-not-found=true")
						_, cleanupErr := utils.Run(cmd)
						Expect(cleanupErr).NotTo(HaveOccurred(), "failed to delete %s", resource)
					}
					cmd := exec.Command(
						"kubectl", "delete", "serviceaccount", probeServiceAccountName,
						"-n", crNS, "--ignore-not-found=true",
					)
					_, cleanupErr := utils.Run(cmd)
					Expect(cleanupErr).NotTo(HaveOccurred(), "failed to delete probe ServiceAccount")
				})
				for _, args := range [][]string{
					{
						"create", "clusterrolebinding", probeMonitoringBindingName,
						"--clusterrole=cluster-monitoring-view",
						"--serviceaccount=" + crNS + ":" + probeServiceAccountName,
					},
					{
						"create", "clusterrolebinding", probeAlertmanagerBindingName,
						"--clusterrole=" + crNS + "-alertmanager-view",
						"--serviceaccount=" + crNS + ":" + probeServiceAccountName,
					},
				} {
					cmd := exec.Command("kubectl", args...)
					_, err = utils.Run(cmd)
					Expect(err).NotTo(HaveOccurred())
				}
				probeScript := `set -eu; token=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token); ` +
					`curl -fsS --connect-timeout 10 --cacert /etc/monitoring-ca/service-ca.crt ` +
					`-H \"Authorization: Bearer ${token}\" ` +
					`https://thanos-querier.openshift-monitoring.svc:9091/api/v1/status/buildinfo ` +
					`>/dev/null; curl -fsS --connect-timeout 10 --cacert /etc/monitoring-ca/service-ca.crt ` +
					`-H \"Authorization: Bearer ${token}\" ` +
					`https://alertmanager-main.openshift-monitoring.svc:9094/api/v2/status >/dev/null`
				probeOverrides := `{
				"spec": {
					"serviceAccountName": "` + probeServiceAccountName + `",
					"restartPolicy": "Never",
					"containers": [{
						"name": "probe",
						"image": "` + probeImage + `",
						"command": ["/bin/sh", "-c"],
						"args": ["` + probeScript + `"],
						"volumeMounts": [{
							"name": "monitoring-ca",
							"mountPath": "/etc/monitoring-ca",
							"readOnly": true
						}],
						"securityContext": {
							"allowPrivilegeEscalation": false,
							"capabilities": {"drop": ["ALL"]},
							"runAsNonRoot": true,
							"runAsUser": 1000,
							"seccompProfile": {"type": "RuntimeDefault"}
						}
					}],
					"volumes": [{
						"name": "monitoring-ca",
						"configMap": {
							"name": "effectivenessmonitor-service-ca",
							"items": [{
								"key": "service-ca.crt",
								"path": "service-ca.crt"
							}]
						}
					}]
				}
			}`
				var probePayload struct {
					Spec struct {
						Containers []struct {
							Args []string `json:"args"`
						} `json:"containers"`
					} `json:"spec"`
				}
				Expect(json.Unmarshal([]byte(probeOverrides), &probePayload)).To(Succeed())
				Expect(probePayload.Spec.Containers).To(HaveLen(1))
				Expect(probePayload.Spec.Containers[0].Args).To(HaveLen(1))
				Expect(probePayload.Spec.Containers[0].Args[0]).To(
					ContainSubstring(`-H "Authorization: Bearer ${token}"`),
				)
				cmd = exec.Command("kubectl", "run", probeName, "--restart=Never", "--namespace", crNS,
					"--image="+probeImage, "--overrides", probeOverrides)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					cmd := exec.Command(
						"kubectl", "delete", "pod", probeName, "-n", crNS,
						"--ignore-not-found=true",
					)
					_, cleanupErr := utils.Run(cmd)
					Expect(cleanupErr).NotTo(HaveOccurred(), "failed to delete monitoring TLS probe pod")
				})

				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pod", probeName, "-n", crNS, "-o", "jsonpath={.status.phase}")
					phase, getErr := utils.Run(cmd)
					g.Expect(getErr).NotTo(HaveOccurred())
					if phase == "Failed" {
						logs, logErr := utils.Run(exec.Command("kubectl", "logs", probeName, "-n", crNS))
						g.Expect(logErr).NotTo(HaveOccurred(), logs)
					}
					g.Expect(phase).To(Equal("Succeeded"))
				}, 5*time.Minute, 2*time.Second).Should(Succeed())
			},
		)

		It("should preserve bootstrap-provided policy ConfigMaps without operator ownership", func() {
			for _, name := range []string{
				"aianalysis-policy",
				"signalprocessing-policy",
			} {
				cmd := exec.Command("kubectl", "get", "configmap", name,
					"-n", crNS, "-o", "jsonpath={.metadata.labels.app\\.kubernetes\\.io/managed-by}")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "ConfigMap %q should be provided by the bootstrap chart", name)
				Expect(output).To(Equal("Helm"), "ConfigMap %q should remain Helm-owned", name)

				cmd = exec.Command("kubectl", "get", "configmap", name,
					"-n", crNS, "-o", "jsonpath={.metadata.ownerReferences}")
				output, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(), "ConfigMap %q ownership should be readable", name)
				Expect(output).To(BeEmpty(), "ConfigMap %q should not be adopted by the operator", name)
			}
		})

		It("should create the Kubernaut Agent client RoleBinding", func() {
			cmd := exec.Command("kubectl", "get", "rolebinding",
				"kubernaut-agent-client-aianalysis",
				"-n", crNS, "-o", "jsonpath={.roleRef.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Kubernaut Agent client RoleBinding should exist")
			Expect(output).To(Equal(crNS + "-kubernaut-agent-client"))
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

		It("should retain the workflow namespace and clean operator-owned contents", func() {
			cmd := exec.Command("kubectl", "get", "namespace", workflowNS,
				"-o", "jsonpath={.metadata.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "workflow namespace should be retained")
			Expect(output).To(Equal(workflowNS))

			cmd = exec.Command("kubectl", "get", "namespace", workflowNS,
				"-o", "jsonpath={.metadata.labels.app\\.kubernetes\\.io/managed-by}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "workflow namespace ownership should be readable")
			Expect(output).To(Equal("kubernaut-operator"),
				"retained workflow namespace should keep operator provenance")

			for _, kind := range []string{"role", "rolebinding", "serviceaccount"} {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", kind,
						"-n", workflowNS,
						"-l", "app.kubernetes.io/managed-by=kubernaut-operator",
						"-o", `go-template={{range .items}}{{.metadata.name}}{{"\n"}}{{end}}`)
					output, getErr := utils.Run(cmd)
					g.Expect(getErr).NotTo(HaveOccurred(),
						"operator-owned workflow %s should be readable", kind)
					g.Expect(utils.GetNonEmptyLines(output)).To(BeEmpty(),
						"operator-owned workflow %s should be deleted", kind)
				}, 2*time.Minute).Should(Succeed())
			}
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

func installDevelopmentBootstrap() {
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "helm", "upgrade", "--install", "kubernaut-dev-bootstrap",
		"charts/kubernaut-dev-bootstrap",
		"--namespace", crNS,
		"--create-namespace",
		"--wait", "--timeout", "10m")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install the development bootstrap chart")
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
		cmd := exec.CommandContext(context.Background(), "kubectl", "create", "--raw", fmt.Sprintf(
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
	cmd := exec.CommandContext(context.Background(), "kubectl", "logs", "curl-metrics", "-n", operatorNS)
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
	return utils.Run(exec.CommandContext(context.Background(), "kubectl", args...))
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
func collectDiagnostic(args ...string) {
	cmd := exec.CommandContext(context.Background(), "kubectl", args...)
	output, err := utils.Run(cmd)
	if err == nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== kubectl %v ===\n%s\n", args, output)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "=== kubectl %v FAILED: %v ===\n", args, err)
	}
}

var mustGatherOnce sync.Once

func collectMustGather() {
	mustGatherOnce.Do(func() {
		destination := os.Getenv("KUBERNAUT_E2E_MUST_GATHER_DIR")
		if destination == "" {
			destination = filepath.Join("test-artifacts", "must-gather-"+time.Now().UTC().Format("20060102T150405Z"))
		}
		// E2E artifact path is explicitly supplied by the test runner.
		//nolint:gosec
		if err := os.MkdirAll(destination, 0o750); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "=== must-gather FAILED creating %s: %v ===\n", destination, err)
			return
		}
		image := os.Getenv("KUBERNAUT_MUST_GATHER_IMAGE")
		if image == "" {
			image = defaultMustGatherImage
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		// E2E image/path are explicit test-runner inputs.
		//nolint:gosec
		cmd := exec.CommandContext(ctx, "oc", "adm", "must-gather",
			"--image="+image,
			"--dest-dir="+destination,
			"--since=24h",
			"--",
			"/usr/bin/gather",
			"--namespace="+crNS,
			"--operator-namespace="+operatorNS,
			"--workflow-namespace="+workflowNS,
		)
		if _, err := utils.Run(cmd); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				err = fmt.Errorf("must-gather timed out after 10m: %w", ctx.Err())
			}
			_, _ = fmt.Fprintf(GinkgoWriter, "=== must-gather FAILED: %v ===\n", err)
			return
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "=== must-gather saved to %s ===\n", destination)
	})
}

func preserveE2ENamespaces() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_PRESERVE_NAMESPACES"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
