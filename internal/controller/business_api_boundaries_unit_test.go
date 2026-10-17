package controller

import (
	"context"
	"embed"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

var _ = Describe("independent Kubernetes API boundary decisions", Label("unit"), func() {
	It("UT-AUDIT-515-API-001: reuses prepared administrator namespaces read-only and converges only owned namespace security", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		namespace := resources.WorkflowNamespace(kn)
		namespace.Labels, namespace.Annotations = nil, nil
		Expect(r.Create(ctx, namespace)).To(Succeed())
		Expect(r.deployWorkflowNamespace(ctx, kn)).To(MatchError(ContainSubstring("ownership conflict")))
		namespace.Labels = make(map[string]string)
		resources.EnsureRestrictedPSALabels(namespace.Labels)
		Expect(r.Update(ctx, namespace)).To(Succeed())
		before := namespace.DeepCopy()
		Expect(r.deployWorkflowNamespace(ctx, kn)).To(Succeed())
		Expect(r.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)).To(Succeed())
		Expect(namespace).To(Equal(before))
		Expect(r.deleteWorkflowResources(ctx, kn)).To(BeEmpty())
		Expect(r.Get(ctx, client.ObjectKeyFromObject(namespace), namespace)).To(Succeed(), "external namespace must survive")
		Expect(namespace).To(Equal(before))
		Expect(r.Delete(ctx, namespace)).To(Succeed())
		namespace = resources.WorkflowNamespace(kn)
		resources.StampOwnership(kn, namespace)
		Expect(r.Create(ctx, namespace)).To(Succeed())
		for _, operation := range []string{"get", unitUpdateOperation, policyDeleteOperation} {
			api := r.Client.(client.WithWatch)
			faults := interceptor.Funcs{}
			switch operation {
			case "get":
				faults.Get = deniedUnitGet
			case unitUpdateOperation:
				namespace.Labels = resources.CommonLabels(kn)
				Expect(api.Update(ctx, namespace)).To(Succeed())
				faults.Update = deniedUnitUpdate
			case "delete":
				runner := resources.WorkflowRunnerServiceAccount(kn)
				resources.StampOwnership(kn, runner)
				Expect(api.Create(ctx, runner)).To(Succeed())
				faults.Delete = deniedUnitDelete
			}
			r.Client = interceptor.NewClient(api, faults)
			r.APIReader = r.Client
			if operation == "delete" {
				Expect(r.deleteWorkflowResources(ctx, kn)).NotTo(BeEmpty())
			} else {
				Expect(r.deployWorkflowNamespace(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
			r.Client = api
			r.APIReader = api
		}
	})

	It("UT-AUDIT-515-API-002: surfaces each RBAC discovery failure independently", func() {
		for _, target := range []string{"*v1.ClusterRoleList", "*v1.ClusterRoleBindingList", "*v1.RoleList", "*v1.RoleBindingList"} {
			kn := newMinimalCR()
			r := businessUnitReconciler(kn, interceptor.Funcs{List: func(ctx context.Context, api client.WithWatch,
				list client.ObjectList, options ...client.ListOption) error {
				if fmt.Sprintf("%T", list) == target {
					return fmt.Errorf("%s: %s", unitAPIDenial, target)
				}
				return api.List(ctx, list, options...)
			}})
			if target == "*v1.ClusterRoleList" || target == "*v1.ClusterRoleBindingList" {
				Expect(r.deployClusterScopedCoreRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			} else {
				Expect(r.deployCoreRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
			additionalErrors := r.pruneOrphanedAdditionalComponentRBAC(ctx, kn, nil)
			if target == "*v1.ClusterRoleBindingList" {
				Expect(additionalErrors).NotTo(BeEmpty())
			} else {
				Expect(additionalErrors).To(BeEmpty())
			}
		}
	})

	It("UT-AUDIT-515-API-003: propagates additional and persona binding write, prune, role-read and status errors", func() {
		for _, operation := range []string{unitCreateOperation, unitListOperation, "role-read", "status", unitPruneOperation, "legacy-delete"} {
			kn := newMinimalCR()
			kn.Spec.AdditionalClusterRoles = []string{"customer-role"}
			kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{RoleBindings: []kubernautv1alpha2.ToolRoleBinding{{
				Role: "sre", Groups: []string{"engineers"},
			}}}
			kn.Status.BoundToolRoleBindings = []string{"stale-binding"}
			faults := interceptor.Funcs{}
			switch operation {
			case unitCreateOperation:
				faults.Create = func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
					if _, binding := object.(*rbacv1.ClusterRoleBinding); binding {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.Create(ctx, object, options...)
				}
			case unitListOperation:
				faults.List = deniedUnitList
			case "role-read":
				faults.Get = deniedUnitGet
			case "status":
				faults.SubResourcePatch = deniedUnitStatus
			default:
				faults.Delete = func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.DeleteOption) error {
					if operation == unitPruneOperation || object.GetName() == "apifrontend-rbac-roles" {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.Delete(ctx, object, options...)
				}
			}
			r := businessUnitReconciler(kn, faults)
			if operation == unitPruneOperation || operation == "legacy-delete" {
				seedBusinessOwnedResources(r, kn,
					&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "stale-binding"}},
					&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "apifrontend-rbac-roles", Namespace: kn.Namespace}})
				Expect(r.deployToolRBAC(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
				continue
			}
			Expect(r.deployAdditionalComponentRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)), operation)
			if operation == unitCreateOperation {
				Expect(r.deployToolRBAC(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
				Expect(r.deployConsoleAccessRBAC(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			}
		}
	})

	It("UT-AUDIT-515-API-004: provisions scoped Fleet and Ansible RBAC and preserves wanted objects during namespace migration", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.WorkflowExecution.Ansible.Enabled = true
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.deployCoreRBAC(ctx, kn, kn)).To(Succeed())
		Expect(r.deployToggleRBAC(ctx, kn)).To(Succeed())
		roles, bindings := resources.MCPGatewayNamespaceRBAC(kn, kn)
		Expect(roles).NotTo(BeEmpty())
		Expect(bindings).NotTo(BeEmpty())
		Expect(r.deployMCPGatewayNamespaceRBAC(ctx, kn, kn)).To(Succeed())
		kn.Spec.Fleet.MCPGateway.Namespace = "new-mcp-namespace"
		Expect(r.deployMCPGatewayNamespaceRBAC(ctx, kn, kn)).To(Succeed())
		for _, role := range roles {
			Expect(apierrors.IsNotFound(r.Get(ctx, client.ObjectKeyFromObject(role), role))).To(BeTrue())
		}
		for _, objectType := range []string{"*v1.Role", "*v1.RoleBinding", "*v1.ClusterRole", "*v1.ClusterRoleBinding"} {
			r = businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
				object client.Object, options ...client.CreateOption) error {
				if fmt.Sprintf("%T", object) == objectType {
					return fmt.Errorf("%s", unitAPIDenial)
				}
				return api.Create(ctx, object, options...)
			}})
			if objectType == "*v1.Role" || objectType == "*v1.RoleBinding" {
				Expect(r.deployMCPGatewayNamespaceRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
			} else {
				Expect(r.deployToggleRBAC(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
				Expect(r.deployRBAC(ctx, kn, kn)).To(HaveOccurred())
			}
		}
	})

	It("UT-AUDIT-515-API-005: does not report success when stale RBAC deletions fail", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.deployCoreRBAC(ctx, kn, kn)).To(Succeed())
		kn.Spec.Fleet.Enabled = ptr.To(false)
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Delete: deniedUnitDelete})
		Expect(r.deployMCPGatewayNamespaceRBAC(ctx, kn, kn)).To(HaveOccurred())
		Expect(r.deployClusterScopedCoreRBAC(ctx, kn, kn)).To(HaveOccurred())
		Expect(r.pruneOrphanedAdditionalComponentRBAC(ctx, kn, nil)).To(BeEmpty())
		stale := resources.AdditionalComponentCRB(kn, resources.ComponentGateway, "gateway", "customer")
		Expect(r.Create(ctx, stale)).To(Succeed())
		Expect(r.pruneOrphanedAdditionalComponentRBAC(ctx, kn, nil)).NotTo(BeEmpty())
	})

	It("UT-AUDIT-515-API-006: handles raced resource creation and invalid ownership without losing API errors", func() {
		kn := newMinimalCR()
		for _, racePersists := range []bool{true, false} {
			object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "raced", Namespace: kn.Namespace}}
			r := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
				object client.Object, options ...client.CreateOption) error {
				if racePersists {
					Expect(api.Create(ctx, object, options...)).To(Succeed())
				}
				return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, object.GetName())
			}})
			err := r.ensureResource(ctx, kn, object)
			if racePersists {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring("after AlreadyExists")))
			}
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: deniedUnitGet})
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "blocked", Namespace: kn.Namespace}}
		Expect(r.ensureResource(ctx, kn, object)).To(MatchError(ContainSubstring(unitAPIDenial)))
		_, err := r.createIfNotFound(ctx, kn, object, object.DeepCopy())
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		for index, faults := range []interceptor.Funcs{{Create: deniedUnitCreate}, {Create: func(context.Context, client.WithWatch,
			client.Object, ...client.CreateOption) error {
			return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, "blocked")
		}}} {
			r = businessUnitReconciler(kn, faults)
			created, createErr := r.createIfNotFound(ctx, kn, object.DeepCopy(), object.DeepCopy())
			Expect(created).To(BeFalse())
			if index == 0 {
				Expect(createErr).To(MatchError(ContainSubstring(unitAPIDenial)))
			} else {
				Expect(createErr).To(MatchError(ContainSubstring("after AlreadyExists")))
			}
		}
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		r.Scheme = runtime.NewScheme()
		Expect(r.ensureNamespaced(ctx, kn, object)).To(HaveOccurred())
		_, err = r.createIfNotFound(ctx, kn, object, object.DeepCopy())
		Expect(err).To(HaveOccurred())
	})

	It("UT-AUDIT-515-API-007: rejects persisted noncanonical names and preserves activation checks on direct API clients", func() {
		kn := newMinimalCR()
		kn.Name = "unexpected"
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		Expect(kn.Finalizers).To(BeEmpty())
		r.APIReader = nil
		Expect(r.authorizeSingleton(ctx, kn)).To(Succeed())
	})

	It("UT-AUDIT-515-API-008: makes optional monitoring availability and pruning failures explicit", func() {
		kn := newMinimalCR()
		kn.UID = "monitoring-boundary"
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Delete(ctx, &configv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}})).To(Succeed())
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		Expect(r.monitoringCondition(ctx, kn).Reason).To(Equal(ReasonMonitoringUnavailable))
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(true)
		kn.Spec.Monitoring.Prometheus.URL = monitoringPrometheusServiceURL
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		for _, name := range []string{"servicemonitors.monitoring.coreos.com", "prometheusrules.monitoring.coreos.com"} {
			Expect(r.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
		}
		Expect(r.monitoringCondition(ctx, kn).Reason).To(Equal(ReasonMonitoringUnavailable))
		Expect(r.reconcilePrometheusMonitoring(ctx, kn, false, false, false)).To(Succeed())
		Expect(r.reconcilePrometheusMonitoring(ctx, kn, true, false, false)).To(Succeed())
		Expect(r.ensureAPIFrontendMonitoring(ctx, kn)).To(Succeed())
		legacy := &monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "authwebhook-monitor", Namespace: kn.Namespace}}
		Expect(resources.SetOwnerReference(kn, legacy, r.Scheme)).To(Succeed())
		Expect(r.Create(ctx, legacy)).To(Succeed())
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Delete: deniedUnitDelete})
		Expect(r.deployMonitoring(ctx, kn, true, false)).To(MatchError(ContainSubstring(unitAPIDenial)))
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: deniedUnitGet})
		Expect(r.pruneLegacyAuthWebhookServiceMonitor(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
	})

	It("UT-AUDIT-515-API-009: recovers stale service-CA failures only when the requested serving Secret is absent", func() {
		kn := newMinimalCR()
		service := resources.Services(kn, kn)[0]
		service.Annotations["service.beta.openshift.io/serving-cert-generation-error"] = "stale"
		service.Annotations["service.alpha.openshift.io/serving-cert-generation-error-num"] = "3"
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, service)).To(Succeed())
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: kn.Namespace, Name: service.Annotations[resources.OCPServingCertAnnotation]}}
		Expect(r.Create(ctx, secret)).To(Succeed())
		Expect(r.clearStaleServingCertErrors(ctx, kn, service)).To(Succeed())
		Expect(r.Delete(ctx, secret)).To(Succeed())
		Expect(r.clearStaleServingCertErrors(ctx, kn, service)).To(Succeed())
		live := &corev1.Service{}
		Expect(r.Get(ctx, client.ObjectKeyFromObject(service), live)).To(Succeed())
		Expect(live.Annotations).NotTo(HaveKey("service.beta.openshift.io/serving-cert-generation-error"))
		Expect(live.Annotations).NotTo(HaveKey("service.alpha.openshift.io/serving-cert-generation-error-num"))
		Expect(r.Delete(ctx, live)).To(Succeed())
		Expect(r.clearStaleServingCertErrors(ctx, kn, service)).To(Succeed())
		service.ResourceVersion = ""
		Expect(r.Create(ctx, service)).To(Succeed())
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context,
			api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, secret := object.(*corev1.Secret); secret {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.ensureServices(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		Expect(ownedServiceSpec(&corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9090}}}}).
			Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		stampConfigMapHash(&appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentGateway}},
		}}}, nil)
	})

	It("UT-AUDIT-515-API-010: treats OpenShift profile/domain reads and external event mapping as optional evidence", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		ingress := &configv1.Ingress{}
		Expect(r.Get(ctx, client.ObjectKey{Name: "cluster"}, ingress)).To(Succeed())
		ingress.Spec.Domain = "apps.example.com"
		Expect(r.Update(ctx, ingress)).To(Succeed())
		Expect(r.clusterIngressDomain(ctx)).To(Equal("apps.example.com"))
		apiServer := &configv1.APIServer{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}, Spec: configv1.APIServerSpec{
			TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
		}}
		Expect(r.Create(ctx, apiServer)).To(Succeed())
		Expect(r.resolveClusterTLSProfile(ctx)).NotTo(BeEmpty())
		Expect(r.Delete(ctx, ingress)).To(Succeed())
		Expect(r.clusterIngressDomain(ctx)).To(BeEmpty())
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: deniedUnitGet, List: deniedUnitList})
		Expect(r.clusterIngressDomain(ctx)).To(BeEmpty())
		Expect(r.resolveClusterTLSProfile(ctx)).To(BeEmpty())
		Expect(r.apiServerToKubernaut(ctx, apiServer)).To(BeEmpty())
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "prometheus"}})).To(BeEmpty())
		Expect(openShiftAPIServerWatchSupported(nil)).To(BeFalse())
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{configv1.GroupVersion})
		mapper.Add(configv1.GroupVersion.WithKind("APIServer"), meta.RESTScopeRoot)
		r.Client = businessMappedClient{Client: r.Client, mapper: mapper}
		Expect(r.openShiftPlatformDetected(ctx)).To(BeTrue())
		mapper = meta.NewDefaultRESTMapper([]schema.GroupVersion{configv1.GroupVersion})
		mapper.Add(configv1.GroupVersion.WithKind("Ingress"), meta.RESTScopeRoot)
		r.Client = businessMappedClient{Client: r.Client, mapper: mapper}
		Expect(r.openShiftPlatformDetected(ctx)).To(BeTrue())
		r.Client = businessMappedClient{Client: r.Client, mapper: nil}
		Expect(r.routeAPISupported()).To(BeFalse())
	})
})

var _ = Describe("independent activation and exposure failure decisions", Label("unit"), func() {
	It("UT-AUDIT-515-API-011: reports every migration prerequisite and Job API failure before progressing", func() {
		kn := newMinimalCR()
		for _, name := range []string{pgSecretName, "datastorage-db-secret", "kubernaut-migrations",
			resources.InterServiceCAConfigMapName, resources.TrustBundleConfigMapName} {
			r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch,
				key client.ObjectKey, object client.Object, options ...client.GetOption) error {
				if key.Name == name {
					return fmt.Errorf("%s", unitAPIDenial)
				}
				return api.Get(ctx, key, object, options...)
			}})
			Expect(r.ensureMigrationPrereqs(ctx, kn)).To(MatchError(ContainSubstring(unitAPIDenial)), name)
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		r.RestCfg = &rest.Config{Host: "://"}
		_, err := r.phaseMigrate(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionCRDsInstalled)).To(BeTrue())
		kn.Spec.TLS.Mode = "invalid"
		Expect(businessUnitReconciler(kn, interceptor.Funcs{}).ensureMigrationPrereqs(ctx, kn)).To(HaveOccurred())
		kn.Spec.TLS.Mode = ""
		pg := &corev1.Secret{}
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: pgSecretName}, pg)).To(Succeed())
		delete(pg.Data, "POSTGRES_USER")
		Expect(r.Update(ctx, pg)).To(Succeed())
		Expect(r.ensureMigrationPrereqs(ctx, kn)).To(HaveOccurred())
		Expect(r.validateSecret(ctx, kn.Namespace, pgSecretName, []string{"POSTGRES_USER"})).To(HaveOccurred())
		for _, faults := range []interceptor.Funcs{{Create: deniedUnitCreate}, {SubResourcePatch: deniedUnitStatus}} {
			r = businessUnitReconciler(kn, faults)
			_, err = r.ensureMigrationJob(ctx, kn)
			Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		}
		r = businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "stale", Namespace: kn.Namespace}}
		_, err = r.handleCompleteMigrationJob(ctx, kn, job, "new")
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		_, err = r.handleFailedMigrationJob(ctx, kn, job)
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
	})

	It("UT-AUDIT-515-API-012: propagates native policy render, ownership, prune and status failures", func() {
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		kn.Spec.Monitoring.Prometheus.Enabled, kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false), ptr.To(false)
		for _, faults := range []interceptor.Funcs{
			{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
				if _, native := object.(*unstructured.Unstructured); native {
					return fmt.Errorf("%s", unitAPIDenial)
				}
				return api.Create(ctx, object, options...)
			}},
			{List: func(ctx context.Context, api client.WithWatch, list client.ObjectList, options ...client.ListOption) error {
				if _, native := list.(*unstructured.UnstructuredList); native {
					return fmt.Errorf("%s", unitAPIDenial)
				}
				return api.List(ctx, list, options...)
			}},
		} {
			r := ciliumBusinessReconciler(kn, faults)
			Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		}
		r := ciliumBusinessReconciler(kn, interceptor.Funcs{})
		kn.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "metrics"}}
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(HaveOccurred())
		kn.Spec.NetworkPolicies.ScrapeSources = nil
		r.Client = interceptor.NewClient(r.Client.(businessMappedClient).Client.(client.WithWatch),
			interceptor.Funcs{SubResourcePatch: deniedUnitStatus})
		Expect(r.patchProviderPolicyStatus(ctx, kn, policy.DetectionResult{}, false, nil)).To(HaveOccurred())
		desired := nativePolicyOwnershipFixture()
		existing := desired.DeepCopy()
		existing.SetLabels(nil)
		Expect(providerPolicyOwnershipError(existing, desired)).To(HaveOccurred())
		r.Scheme = runtime.NewScheme()
		_, err := r.ensureRenderedProviderPolicies(ctx, kn, []policy.RenderedPolicy{{Object: desired, Namespaced: true}})
		Expect(err).To(HaveOccurred())
		for index, faults := range []interceptor.Funcs{{List: deniedUnitList}, {Delete: deniedUnitDelete},
			{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Kind: "AbsentPolicy"}}
			}}} {
			r = businessUnitReconciler(kn, interceptor.Funcs{})
			desired.SetNamespace(kn.Namespace)
			Expect(r.Create(ctx, desired.DeepCopy())).To(Succeed())
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), faults)
			errors := r.deleteProviderPolicies(ctx, kn.Namespace, kn.Name)
			if index == 2 {
				// NoKindMatch is an absent optional API, not a cleanup failure.
				Expect(errors).To(BeEmpty())
			} else {
				Expect(errors).NotTo(BeEmpty())
			}
		}
		kn.Namespace = ""
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(HaveOccurred())
	})

	It("UT-AUDIT-515-API-013: reconciles all OpenShift exposures and surfaces per-component write failures", func() {
		kn := newMinimalCR()
		kn.Spec.Console.Enabled = ptr.To(true)
		kn.Spec.APIFrontend.Route.Enabled = ptr.To(true)
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		r.Scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{}, &routev1.RouteList{})
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{routev1.GroupVersion})
		mapper.Add(routev1.GroupVersion.WithKind("Route"), meta.RESTScopeNamespace)
		base := r.Client
		r.Client = businessMappedClient{Client: base, mapper: mapper}
		hasRoute, err := r.reconcileRoutes(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasRoute).To(BeTrue())
		Expect(r.finalizeDeployStatus(ctx, kn, true)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionRouteReady)).To(BeTrue())
		for _, route := range []*routev1.Route{resources.GatewayRouteStub(kn), resources.APIFrontendRouteStub(kn), resources.ConsoleRouteStub(kn)} {
			r.Client = businessMappedClient{Client: interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
					if _, isRoute := object.(*routev1.Route); isRoute && key.Name == route.Name {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.Get(ctx, key, object, options...)
				},
			}), mapper: mapper}
			_, err = r.reconcileRoutes(ctx, kn)
			Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		}
		r.Client = businessMappedClient{Client: base, mapper: mapper}
		kn.Spec.Gateway.Enabled, kn.Spec.APIFrontend.Route.Enabled, kn.Spec.Console.Enabled = ptr.To(false), ptr.To(false), ptr.To(false)
		hasRoute, err = r.reconcileOpenShiftRoutes(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasRoute).To(BeFalse())
		seedBusinessOwnedResources(r, kn, resources.GatewayRouteStub(kn))
		r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Delete: deniedUnitDelete})
		_, err = r.reconcileOptionalRoute(ctx, kn, "Gateway", nil, resources.GatewayRouteStub(kn))
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
	})

	It("UT-AUDIT-515-API-014: validates portable exposure identity before creating Ingress and rejects invalid material", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHook,
			Hooks: &kubernautv1alpha2.TLSHooksConfig{TLSCerts: kubernautv1alpha2.TLSCertsConfig{ExtraSANs: []string{"gateway.example.com"}}}}
		kn.Spec.Gateway.Route.Enabled, kn.Spec.APIFrontend.Route.Enabled = ptr.To(false), ptr.To(false)
		kn.Spec.Gateway.Ingress = kubernautv1alpha2.IngressSpec{Enabled: ptr.To(true), IngressClassName: "nginx",
			Host: "gateway.example.com", TLSSecretName: resources.GatewayTLSSecretName}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		_, err := r.reconcileGenericIngresses(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("reading TLS Secret")))
		secrets, err := resources.DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		for _, secret := range secrets {
			Expect(r.Create(ctx, secret)).To(Succeed())
		}
		hasIngress, err := r.reconcileGenericIngresses(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasIngress).To(BeTrue())
		kn.Spec.Gateway.Ingress.Host = "uncovered.example.com"
		_, err = r.reconcileRoutes(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("validating TLS Secret")))
		kn.Spec.Gateway.Ingress.Host = "gateway.example.com"
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Update: deniedUnitUpdate})
		kn.Spec.Gateway.Ingress.Annotations = map[string]string{"updated": "true"}
		_, err = r.reconcileGenericIngresses(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		kn.Spec.Gateway.Ingress.Host = ""
		_, err = r.reconcileGenericIngresses(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("building generic exposure")))
	})
})

var _ = Describe("independent activation trust boundaries", Label("unit"), func() {
	It("UT-FLEET-521-TRUST-001: ignores the absent ACM trust lane for FMC", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(Succeed())
	})

	It("UT-FLEET-521-PREFLIGHT-SECRET-001: validates ACM token Secret existence and selected key before deployment", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.Fleet.ScopeCheck.Backend = fleetBackendACM
		kn.Spec.Fleet.ScopeCheck.ACM = &kubernautv1alpha2.FleetACMScopeCheckSpec{
			Endpoint:       "https://acm-search.example.com/graphql",
			TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
		}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateFleetTokenSecret(ctx, kn)).To(MatchError(ContainSubstring("not found")))
		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated).Reason).To(Equal(ReasonFleetTokenSecretInvalid))
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acm-search-token", Namespace: kn.Namespace}, Data: map[string][]byte{"wrong": []byte("token")}}
		Expect(r.Create(ctx, secret)).To(Succeed())
		Expect(r.validateFleetTokenSecret(ctx, kn)).To(MatchError(ContainSubstring("missing required key")))
		secret.Data["token"] = []byte("bearer")
		Expect(r.Update(ctx, secret)).To(Succeed())
		Expect(r.validateFleetTokenSecret(ctx, kn)).To(Succeed())
	})

	It("UT-AUDIT-515-API-015: refuses invalid Fleet trust in either connection lane and accepts only readable CA material", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.Fleet.ScopeCheck.Backend = fleetBackendACM
		kn.Spec.Fleet.ScopeCheck.ACM = &kubernautv1alpha2.FleetACMScopeCheckSpec{
			Endpoint:       "https://acm-search.example.com/graphql",
			TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
		}
		kn.Spec.Fleet.ScopeCheck.ACM.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "scope-ca"}}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("could not be read")))
		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated).Reason).
			To(Equal(ReasonFleetTrustSecretInvalid))
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "scope-ca", Namespace: kn.Namespace}}
		Expect(r.Create(ctx, secret)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("missing key")))
		secret.Data = map[string][]byte{"ca.crt": []byte("malformed")}
		Expect(r.Update(ctx, secret)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("valid ca pem")))
		secret.Data["ca.crt"] = telemetryCAPEM()
		Expect(r.Update(ctx, secret)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(Succeed())
		kn.Spec.Fleet.OAuth2.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: secret.Name, Key: "missing-oauth-key"}}
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("spec.fleet.oauth2.tls")))
		kn.Spec.Fleet.OAuth2.TLS.Source = kubernautv1alpha2.FleetTrustSourceSystem
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(Succeed())
	})

	It("UT-AUDIT-515-API-016: prevents validation and deployment from concealing telemetry, database or TLS read failures", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.Telemetry = telemetrySecretSpec()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: unitTelemetryEndpoint}
		reads := 0
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch,
			key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, isConfigMap := object.(*corev1.ConfigMap); isConfigMap && key.Name == resources.TrustBundleConfigMapName {
				reads++
				if reads == 2 {
					return fmt.Errorf("%s", unitAPIDenial)
				}
			}
			return api.Get(ctx, key, object, options...)
		}})
		result, err = r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated).Reason).
			To(Equal(ReasonTelemetryAmbientInvalid))
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError(ContainSubstring("resolving telemetry material revision")))
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{}
		kn.Spec.PostgreSQL.SecretName = unitMissingReference
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError(ContainSubstring("fetching pg secret for config")))
		kn.Spec.PostgreSQL.SecretName = pgSecretName
		for _, failStatus := range []bool{false, true} {
			invalidKn := newMinimalCR()
			invalidKn.Spec.TLS.Mode = unitInvalidTLSMode
			faults := interceptor.Funcs{}
			if failStatus {
				faults.SubResourcePatch = func(ctx context.Context, api client.Client, name string, object client.Object,
					patch client.Patch, options ...client.SubResourcePatchOption) error {
					if current, isCR := object.(*kubernautv1alpha2.Kubernaut); isCR &&
						meta.IsStatusConditionFalse(current.Status.Conditions, kubernautv1alpha2.ConditionTLSReady) {
						return fmt.Errorf("%s", unitAPIDenial)
					}
					return api.SubResource(name).Patch(ctx, object, patch, options...)
				}
			}
			r = businessUnitReconciler(invalidKn, faults)
			Expect(r.phaseDeploy(ctx, invalidKn, invalidKn)).To(MatchError(ContainSubstring("ensuring runtime TLS")))
		}
	})

	It("UT-AUDIT-515-API-017: propagates every inter-phase reload failure and does not activate without native isolation", func() {
		for _, failRead := range []int{1, 2, 3} {
			kn := newMinimalCR()
			job, err := resources.MigrationJob(kn)
			Expect(err).NotTo(HaveOccurred())
			kn.Status.LastMigrationHash = resources.SpecHash(job)
			reads := 0
			r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch,
				key client.ObjectKey, object client.Object, options ...client.GetOption) error {
				if _, isCR := object.(*kubernautv1alpha2.Kubernaut); isCR {
					reads++
					if reads == failRead {
						return fmt.Errorf("%s", unitAPIDenial)
					}
				}
				return api.Get(ctx, key, object, options...)
			}})
			_, err = r.reconcilePhases(ctx, kn, kn)
			Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		}
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		r := businessUnitReconcilerWithoutProvider(kn, interceptor.Funcs{})
		_, err := r.reconcilePhases(ctx, kn, kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		kn.Status.LastMigrationHash = resources.SpecHash(job)
		r = businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
			object client.Object, options ...client.CreateOption) error {
			if _, deployment := object.(*appsv1.Deployment); deployment {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Create(ctx, object, options...)
		}})
		_, err = r.reconcilePhases(ctx, kn, kn)
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
	})

	It("UT-AUDIT-515-API-018: wires optional Console/Fleet workloads, skips external routing and surfaces build and cleanup errors", func() {
		kn := newMinimalCR()
		kn.Spec.Console.Enabled = ptr.To(true)
		kn.Spec.Console.Auth.SecretName = "external-console-oidc"
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.Notification.Routing = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: "external-routing"}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		hashes, err := r.deployConfigMaps(ctx, kn, kn, "kubernaut", "kubernaut", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(hashes).To(HaveKey("fleetmetadatacache"))
		Expect(hashes).NotTo(HaveKey("notification-routing"))
		builders, err := r.enabledDeploymentBuilders(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.ensureDeployments(ctx, kn, kn, builders, hashes, telemetryMaterialRevisionSet{})).To(Succeed())
		Expect(r.ensureServices(ctx, kn, kn)).To(Succeed())
		failingConsoleService := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
			object client.Object, options ...client.CreateOption) error {
			if object.GetName() == string(resources.ComponentConsole) {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Create(ctx, object, options...)
		}})
		Expect(failingConsoleService.ensureServices(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)), resources.ComponentConsole)

		failingConsoleConfigMap := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
			object client.Object, options ...client.CreateOption) error {
			if object.GetName() == string(resources.ComponentConsole)+"-nginx" {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Create(ctx, object, options...)
		}})
		_, err = failingConsoleConfigMap.deployConfigMaps(ctx, kn, kn, "kubernaut", "kubernaut", "")
		Expect(err).To(HaveOccurred(), "Console nginx ConfigMap")
		Expect(err.Error()).To(ContainSubstring(unitAPIDenial), "Console nginx ConfigMap")

		kn.Spec.Gateway.Enabled = ptr.To(false)
		kn.Spec.Fleet.Enabled = ptr.To(false)
		Expect(r.cleanupDisabledGateway(ctx, kn)).To(Succeed())
		failing := businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		seedBusinessOwnedResources(failing, kn,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.DeploymentName(resources.ComponentGateway), Namespace: kn.Namespace}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: kn.Namespace}})
		_, err = failing.deployWorkloads(ctx, kn, kn, nil, telemetryMaterialRevisionSet{})
		Expect(err).To(HaveOccurred())
		kn.Spec.Gateway.Enabled = ptr.To(true)
		_, err = failing.enabledDeploymentBuilders(ctx, kn, kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.KubernautAgent.Audit.FlushIntervalSeconds = "NaN"
		_, err = r.deployConfigMaps(ctx, kn, kn, "kubernaut", "kubernaut", "")
		Expect(err).To(HaveOccurred())
		kn.Spec.KubernautAgent.Audit.FlushIntervalSeconds = "1"
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Temperature = "NaN"
		kn.Spec.LLMProfiles["primary"] = profile
		_, err = r.deployConfigMaps(ctx, kn, kn, "kubernaut", "kubernaut", "")
		Expect(err).To(HaveOccurred())
		profile.Temperature = "0.5"
		kn.Spec.LLMProfiles["primary"] = profile
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{LLMConfidence: "NaN"}
		_, err = r.deployConfigMaps(ctx, kn, kn, "kubernaut", "kubernaut", "")
		Expect(err).To(HaveOccurred())
		kn.Spec.APIFrontend.SeverityTriage = nil
		withoutControllerImage("RELATED_IMAGE_DATA_STORAGE")
		Expect(r.ensureDeployments(ctx, kn, kn, builders, nil, telemetryMaterialRevisionSet{})).To(HaveOccurred())
	})

	It("UT-AUDIT-515-API-019: rejects unreadable readiness, roles, cleanup resources and migration images", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: deniedUnitGet})
		_, err := r.phaseRunning(ctx, kn, kn)
		Expect(err).To(HaveOccurred())
		_, err = r.detectMissingClusterRoles(ctx, []string{"customer-role"})
		Expect(err).To(HaveOccurred())
		Expect(r.deleteWorkflowResources(ctx, kn)).NotTo(BeEmpty())
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		_, err = r.phaseRunning(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseDegraded))
		withoutControllerImage("RELATED_IMAGE_DB_MIGRATE")
		_, err = r.ensureMigrationJob(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("building migration job")))
	})

	It("UT-AUDIT-515-API-020: validates external CA, serving and signing failures without silently replacing administrator material", func() {
		kn := newMinimalCR()
		generator := kn.DeepCopy()
		generator.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		generated, err := resources.DevelopmentSelfSignedTLSSecrets(generator, nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName: generated[0].Name, ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
			}}
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha2.SigningCertSpec{SecretName: "datastorage-signing-cert"}
		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateExplicitTLS(ctx, kn, material)).To(HaveOccurred())
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(HaveOccurred())
		ca := generated[0].DeepCopy()
		ca.Data = nil
		Expect(r.Create(ctx, ca)).To(Succeed())
		Expect(r.validateExplicitTLS(ctx, kn, material)).To(HaveOccurred())
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(HaveOccurred())
		ca.Data = generated[0].Data
		Expect(r.Update(ctx, ca)).To(Succeed())
		Expect(r.validateExplicitTLS(ctx, kn, material)).To(HaveOccurred(), "serving Secrets are still absent")
		for _, secret := range generated[1:] {
			Expect(r.Create(ctx, secret)).To(Succeed())
		}
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		Expect(r.validateRuntimeTLSSecret(ctx, kn, material, ca, resources.TLSServiceAuthWebhook, resources.GatewayTLSSecretName)).To(HaveOccurred())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeAdministratorManaged
		Expect(r.validateRuntimeTLSSecret(ctx, kn, material, ca, resources.TLSServiceGateway, ca.Name)).To(HaveOccurred())
		Expect(r.validateExplicitTLS(ctx, kn, material)).To(Succeed())
		signing := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.DataStorage.SigningCert.SecretName}, signing)).To(Succeed())
		Expect(r.Delete(ctx, signing)).To(Succeed())
		Expect(r.validateExplicitTLS(ctx, kn, material)).To(HaveOccurred())
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(HaveOccurred())
		signing.ResourceVersion = ""
		signing.Data = nil
		Expect(r.Create(ctx, signing)).To(Succeed())
		Expect(r.validateDataStorageSigningCertificate(ctx, kn)).To(HaveOccurred())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.AdministratorManaged = nil
		material, err = resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.readRuntimeTLSCA(ctx, kn, material)
		Expect(err).To(HaveOccurred())
		trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
			Data: map[string]string{"service-ca.crt": string(ca.Data["ca.crt"])}}
		Expect(r.Create(ctx, trust)).To(Succeed())
		_, err = r.readRuntimeTLSCA(ctx, kn, material)
		Expect(err).NotTo(HaveOccurred())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, nil)).To(HaveOccurred())
		kn.Spec.TLS.Mode = unitInvalidTLSMode
		_, err = r.validateTLSConfiguration(ctx, kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.TLS.Mode = ""
		_, err = r.validateTLSConfiguration(ctx, kn)
		Expect(err).To(HaveOccurred(), "no OpenShift API is available in this explicit-source fixture")
	})

	It("UT-AUDIT-515-API-021: propagates trust-publication, embedded-input, toggle-RBAC and additional-role failures", func() {
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		faults := interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
			object client.Object, options ...client.CreateOption) error {
			if _, configMap := object.(*corev1.ConfigMap); configMap && object.GetName() == resources.TrustBundleConfigMapName {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Create(ctx, object, options...)
		}}
		r := businessUnitReconciler(kn, faults)
		Expect(r.ensureMigrationPrereqs(ctx, kn)).To(MatchError(ContainSubstring("ensuring generic TLS trust before migration")))
		r = businessUnitReconciler(kn, faults)
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		kn = newMinimalCR()
		r = businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		role, _ := resources.AnsibleRBAC(kn)
		seedBusinessOwnedResources(r, kn, role)
		Expect(r.deployRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring("toggle cleanup")))
		kn.Spec.AdditionalClusterRoles = []string{"customer-role"}
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch,
			key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, role := object.(*rbacv1.ClusterRole); role && key.Name == "customer-role" {
				return fmt.Errorf("%s", unitAPIDenial)
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.deployAdditionalComponentRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(unitAPIDenial)))
		previous := assets.MigrationsFS
		DeferCleanup(func() { assets.MigrationsFS = previous })
		assets.MigrationsFS = embed.FS{}
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.ensureMigrationPrereqs(ctx, kn)).To(MatchError(ContainSubstring("building migration configmap")))
	})

	It("UT-AUDIT-515-API-022: propagates development-TLS read/generation failures and ignores unrelated monitoring events", func() {
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: deniedUnitGet})
		_, _, err := r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("reading development TLS secret")))
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{RotationBefore: "not-a-duration"}
		r = businessUnitReconcilerWithoutProvider(kn, interceptor.Funcs{})
		_, _, err = r.ensureRuntimeTLS(ctx, kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		_, err = r.deployWorkloads(ctx, kn, kn, nil, telemetryMaterialRevisionSet{})
		Expect(err).To(HaveOccurred())
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(true)
		kn.Spec.Monitoring.AlertManager.URL = "not-a-url"
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "alertmanager", Namespace: "monitoring"}})).To(BeEmpty())
		kn.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring.svc:9093"
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "monitoring"}})).To(BeEmpty())
	})
})

func withoutControllerImage(name string) {
	previous, existed := os.LookupEnv(name)
	ExpectWithOffset(1, os.Unsetenv(name)).To(Succeed())
	DeferCleanup(func() {
		if existed {
			Expect(os.Setenv(name, previous)).To(Succeed())
		} else {
			Expect(os.Unsetenv(name)).To(Succeed())
		}
	})
}

const (
	unitAPIDenial       = "injected Kubernetes API denial"
	unitCreateOperation = "create"
	unitUpdateOperation = "update"
	unitPruneOperation  = "prune"
	unitListOperation   = "list"
)

func deniedUnitGet(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}

func deniedUnitCreate(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}

func deniedUnitUpdate(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}

func deniedUnitDelete(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}

func deniedUnitList(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}

func deniedUnitStatus(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return fmt.Errorf("%s", unitAPIDenial)
}
