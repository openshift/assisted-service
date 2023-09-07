/*
Copyright 2021.

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

package controllers

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	toml "github.com/pelletier/go-toml"
	pkgerror "github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// agentServiceConfigName is the one and only name for an AgentServiceConfig
	// supported in the cluster. Any others will be ignored.
	agentServiceConfigName        = "agent"
	serviceName            string = "assisted-service"
	imageServiceName       string = "assisted-image-service"
	webhookServiceName     string = "agentinstalladmission"
	databaseName           string = "postgres"

	databasePasswordLength   int = 16
	agentLocalAuthSecretName     = serviceName + "local-auth" // #nosec

	defaultIngressCertCMName      string = "default-ingress-cert"
	defaultIngressCertCMNamespace string = "openshift-config-managed"

	configmapAnnotation                 = "unsupported.agent-install.openshift.io/assisted-service-configmap"
	imageServiceSkipVerifyTLSAnnotation = "unsupported.agent-install.openshift.io/assisted-image-service-skip-verify-tls"

	assistedConfigHashAnnotation         = "agent-install.openshift.io/config-hash"
	mirrorConfigHashAnnotation           = "agent-install.openshift.io/mirror-hash"
	userConfigHashAnnotation             = "agent-install.openshift.io/user-config-hash"
	imageServiceStatefulSetFinalizerName = imageServiceName + "." + aiv1beta1.Group + "/ai-deprovision"
	agentServiceConfigFinalizerName      = "agentserviceconfig." + aiv1beta1.Group + "/ai-deprovision"

	servingCertAnnotation    = "service.beta.openshift.io/serving-cert-secret-name"
	injectCABundleAnnotation = "service.beta.openshift.io/inject-cabundle"

	defaultNamespace = "default"
)

var (
	servicePort          = intstr.Parse("8090")
	serviceHTTPPort      = intstr.Parse("8091")
	databasePort         = intstr.Parse("5432")
	imageHandlerPort     = intstr.Parse("8080")
	imageHandlerHTTPPort = intstr.Parse("8081")

	minDatabaseStorage   = resource.MustParse("1Gi")
	minFilesystemStorage = resource.MustParse("1Gi")
	minImageStorage      = resource.MustParse("10Gi")
)

type AgentServiceConfigReconcileContext struct {
	Log    logrus.FieldLogger
	Scheme *runtime.Scheme

	// selector and tolerations the Operator runs in and propagates to its deployments
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration

	Recorder record.EventRecorder
}

// AgentServiceConfigReconciler reconciles a AgentServiceConfig object
type AgentServiceConfigReconciler struct {
	client.Client

	AgentServiceConfigReconcileContext

	// Namespace the operator is running in
	Namespace string
}

type component struct {
	name   string
	reason string
	fn     NewComponentFn
}

type ASC struct {
	/* target namespace for creating the resource */
	namespace string

	/* reference to the reconciler state */
	rec *AgentServiceConfigReconcileContext

	/* The client for cluster API, which is used for either hub or spoke cluster client.
	   I.e. it should be set according to the required cluster communication (hub/spoke).
	*/
	Client client.Client

	/* the instance itself */
	Object client.Object

	/* Spec part of AgentServiceConfig CRD family */
	spec *aiv1beta1.AgentServiceConfigSpec

	/* Status part of AgentServiceConfig CRD family */
	conditions *[]conditionsv1.Condition

	/* properties. use this field for cross cluster communication */
	properties map[string]interface{}
}

func initASC(r *AgentServiceConfigReconciler, instance *aiv1beta1.AgentServiceConfig) ASC {
	var asc ASC
	asc.namespace = r.Namespace
	asc.rec = &r.AgentServiceConfigReconcileContext
	asc.Client = r.Client
	asc.Object = instance
	asc.spec = &instance.Spec
	asc.conditions = &instance.Status.Conditions
	return asc
}

type NewComponentFn func(context.Context, logrus.FieldLogger, ASC) (client.Object, controllerutil.MutateFn, error)
type ComponentStatusFn func(context.Context, logrus.FieldLogger, string, appsv1.DeploymentConditionType) error

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="apiregistration.k8s.io",resources=apiservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func (r *AgentServiceConfigReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var asc ASC
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"agent_service_config":           req.Name,
			"agent_service_config_namespace": req.Namespace,
		})

	defer func() {
		log.Info("AgentServiceConfig Reconcile ended")
	}()

	log.Info("AgentServiceConfig Reconcile started")

	instance := &aiv1beta1.AgentServiceConfig{}
	asc = initASC(r, instance)

	// NOTE: ignoring the Namespace that seems to get set on request when syncing on namespaced objects
	// when our AgentServiceConfig is ClusterScoped.
	if err := r.Get(ctx, types.NamespacedName{Name: req.NamespacedName.Name}, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.

			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		log.WithError(err).Error("Failed to get resource", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// We only support one AgentServiceConfig per cluster, and it must be called "agent". This prevents installing
	// AgentService more than once in the cluster.
	if instance.Name != agentServiceConfigName {
		reason := fmt.Sprintf("Invalid name (%s)", instance.Name)
		msg := fmt.Sprintf("Only one AgentServiceConfig supported per cluster and must be named '%s'", agentServiceConfigName)
		log.Info(fmt.Sprintf("%s: %s", reason, msg), req.NamespacedName)
		r.Recorder.Event(instance, "Warning", reason, msg)
		return reconcile.Result{}, nil
	}

	// Ensure relevant finalizers exist (cleanup on deletion)
	if err := ensureFinalizers(ctx, log, asc, agentServiceConfigFinalizerName); err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	if !instance.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Invoke validation funcs
	valid, err := validate(ctx, log, asc)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	if !valid {
		return ctrl.Result{}, nil
	}

	// Remove IPXE HTTP routes if not needed
	if err := cleanHTTPRoute(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	// Reconcile components
	for _, component := range getComponents(asc.spec) {
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			return result, err
		}
	}
	for _, component := range r.getWebhookComponents() {
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			return result, err
		}
	}

	// Ensure image-service StatefulSet is reconciled
	if err := ensureImageServiceStatefulSet(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err

	}

	if err := updateConditions(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}

func updateConditions(ctx context.Context, log *logrus.Entry, asc ASC) error {
	msg := "AgentServiceConfig reconcile completed without error."
	conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionReconcileCompleted,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ReasonReconcileSucceeded,
		Message: msg,
	})

	if err := monitorOperands(ctx, log, asc); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionDeploymentsHealthy,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: err.Error(),
		})
		if updateErr := asc.Client.Status().Update(ctx, asc.Object); updateErr != nil {
			log.WithError(updateErr).Error("Failed to update status")
			return updateErr
		}
		return err
	}

	conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionDeploymentsHealthy,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ReasonDeploymentSucceeded,
		Message: "All the deployments managed by Infrastructure-operator are healthy.",
	})

	if statusErr := asc.Client.Status().Update(ctx, asc.Object); statusErr != nil {
		log.WithError(statusErr).Error("Failed to update status")
		return statusErr
	}

	return nil
}

func getComponents(spec *aiv1beta1.AgentServiceConfigSpec) []component {
	components := []component{
		{"FilesystemStorage", aiv1beta1.ReasonStorageFailure, newFilesystemPVC},
		{"DatabaseStorage", aiv1beta1.ReasonStorageFailure, newDatabasePVC},
		{"ImageServiceService", aiv1beta1.ReasonImageHandlerServiceFailure, newImageServiceService},
		{"AgentService", aiv1beta1.ReasonAgentServiceFailure, newAgentService},
		{"ServiceMonitor", aiv1beta1.ReasonAgentServiceMonitorFailure, newServiceMonitor},
		{"ImageServiceRoute", aiv1beta1.ReasonImageHandlerRouteFailure, newImageServiceRoute},
		{"AgentRoute", aiv1beta1.ReasonAgentRouteFailure, newAgentRoute},
		{"AgentLocalAuthSecret", aiv1beta1.ReasonAgentLocalAuthSecretFailure, newAgentLocalAuthSecret},
		{"DatabaseSecret", aiv1beta1.ReasonPostgresSecretFailure, newPostgresSecret},
		{"ImageServiceServiceAccount", aiv1beta1.ReasonImageHandlerServiceAccountFailure, newImageServiceServiceAccount},
		{"IngressCertConfigMap", aiv1beta1.ReasonIngressCertFailure, newIngressCertCM},
		{"ImageServiceConfigMap", aiv1beta1.ReasonConfigFailure, newImageServiceConfigMap},
		{"AssistedServiceConfigMap", aiv1beta1.ReasonConfigFailure, newAssistedCM},
		{"AssistedServiceDeployment", aiv1beta1.ReasonDeploymentFailure, newAssistedServiceDeployment},
	}
	// Additional routes need to be synced if HTTP iPXE routes are exposed
	if exposeIPXEHTTPRoute(spec) {
		components = append(components,
			component{"ImageServiceIPXERoute", aiv1beta1.ReasonImageHandlerRouteFailure, newImageServiceIPXERoute},
			component{"AgentIPXERoute", aiv1beta1.ReasonAgentRouteFailure, newAgentIPXERoute},
		)
	}
	return components

}

func (r *AgentServiceConfigReconciler) getWebhookComponents() []component {
	return []component{
		{"AgentClusterInstallValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newACIWebHook},
		{"AgentClusterInstallMutatingWebHook", aiv1beta1.ReasonMutatingWebHookFailure, newACIMutatWebHook},
		{"InfraEnvValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newInfraEnvWebHook},
		{"AgentValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newAgentWebHook},
		{"WebHookService", aiv1beta1.ReasonWebHookServiceFailure, newWebHookService},
		{"WebHookServiceDeployment", aiv1beta1.ReasonWebHookDeploymentFailure, newWebHookDeployment},
		{"WebHookServiceAccount", aiv1beta1.ReasonWebHookServiceAccountFailure, newWebHookServiceAccount},
		{"WebHookClusterRole", aiv1beta1.ReasonWebHookClusterRoleFailure, newWebHookClusterRole},
		{"WebHookClusterRoleBinding", aiv1beta1.ReasonWebHookClusterRoleBindingFailure, newWebHookClusterRoleBinding},
		{"WebHookAPIService", aiv1beta1.ReasonWebHookAPIServiceFailure, newWebHookAPIService},
	}
}

func reconcileComponent(ctx context.Context, log *logrus.Entry, asc ASC, component component) (ctrl.Result, error) {
	obj, mutateFn, err := component.fn(ctx, log, asc)
	if err != nil {
		msg := "Failed to generate definition for " + component.name
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  component.reason,
			Message: msg,
		})
		if statusErr := asc.Client.Status().Update(ctx, asc.Object); statusErr != nil {
			log.WithError(err).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, statusErr
		}
		return ctrl.Result{Requeue: true}, err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, asc.Client, obj, mutateFn); err != nil {
		msg := "Failed to ensure " + component.name
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  component.reason,
			Message: msg,
		})
		if statusErr := asc.Client.Status().Update(ctx, asc.Object); statusErr != nil {
			log.WithError(err).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, statusErr
		}
	} else if result != controllerutil.OperationResultNone {
		log.Info(component.name + " created")
	}
	return ctrl.Result{Requeue: false}, nil
}

func ensureFinalizers(ctx context.Context, log logrus.FieldLogger, asc ASC, finalizerName string) error {
	if asc.Object.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(asc.Object, finalizerName) {
			controllerutil.AddFinalizer(asc.Object, finalizerName)
			if err := asc.Client.Update(ctx, asc.Object); err != nil {
				log.WithError(err).Error("failed to add finalizer to AgentServiceConfig")
				return err
			}
		}
	} else {
		// do cleanup and remove finalizer
		statefulSet := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imageServiceName,
				Namespace: asc.namespace,
			},
		}
		if err := asc.Client.Get(ctx, client.ObjectKeyFromObject(statefulSet), statefulSet); err != nil && !errors.IsNotFound(err) {
			log.WithError(err).Error("failed to get image service stateful set for cleanup")
			return err
		}
		if err := cleanupImageServiceFinalizer(ctx, asc, statefulSet); err != nil {
			log.WithError(err).Error("failed to cleanup image service stateful set")
			return err
		}

		controllerutil.RemoveFinalizer(asc.Object, finalizerName)
		if err := asc.Client.Update(ctx, asc.Object); err != nil {
			log.WithError(err).Error("failed to remove finalizer from AgentServiceConfig")
			return err
		}
	}
	return nil
}

func cleanHTTPRoute(ctx context.Context, log logrus.FieldLogger, asc ASC) error {
	if !exposeIPXEHTTPRoute(asc.spec) {
		// Ensure HTTP routes are removed
		for _, service := range []string{serviceName, imageServiceName} {
			if err := removeHTTPIPXERoute(ctx, asc, service); err != nil {
				log.WithError(err).Errorf("failed to remove HTTP route for %s", service)
				return err
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ingressCMPredicates := builder.WithPredicates(predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return checkIngressCMName(e.Object) },
		UpdateFunc:  func(e event.UpdateEvent) bool { return checkIngressCMName(e.ObjectNew) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return checkIngressCMName(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return checkIngressCMName(e.Object) },
	})
	ingressCMHandler := handler.EnqueueRequestsFromMapFunc(
		func(_ client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: agentServiceConfigName}}}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.AgentServiceConfig{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&monitoringv1.ServiceMonitor{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&routev1.Route{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&apiregv1.APIService{}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, ingressCMHandler, ingressCMPredicates).
		Complete(r)
}

func monitorOperands(ctx context.Context, log logrus.FieldLogger, asc ASC) error {
	isStatusConditionFalse := func(conditions []appsv1.DeploymentCondition, conditionType appsv1.DeploymentConditionType) bool {
		for _, condition := range conditions {
			if condition.Type == conditionType {
				return condition.Status == corev1.ConditionFalse
			}
		}
		return false
	}

	// monitor deployments
	for _, deployName := range []string{"agentinstalladmission", "assisted-service"} {
		deployment := &appsv1.Deployment{}
		if err := asc.Client.Get(ctx, types.NamespacedName{Name: deployName, Namespace: asc.namespace}, deployment); err != nil {
			return err
		}

		if isStatusConditionFalse(deployment.Status.Conditions, appsv1.DeploymentAvailable) {
			msg := fmt.Sprintf("Deployment %s is not available", deployName)
			log.Error(msg)
			return pkgerror.New(msg)
		}

		if isStatusConditionFalse(deployment.Status.Conditions, appsv1.DeploymentProgressing) {
			msg := fmt.Sprintf("Deployment %s is not progressing", deployName)
			log.Error(msg)
			return pkgerror.New(msg)
		}
	}

	// monitor statefulset
	ss := &appsv1.StatefulSet{}
	if err := asc.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: asc.namespace}, ss); err != nil {
		return err
	}

	desiredReplicas := *ss.Spec.Replicas
	checkReplicas := func(replicas int32, name string) error {
		if replicas != desiredReplicas {
			return fmt.Errorf("StatefulSet %s %s replicas does not match desired replicas", imageServiceName, name)
		}
		return nil
	}
	if err := checkReplicas(ss.Status.Replicas, "created"); err != nil {
		return err
	}
	if err := checkReplicas(ss.Status.ReadyReplicas, "ready"); err != nil {
		return err
	}
	if err := checkReplicas(ss.Status.CurrentReplicas, "current"); err != nil {
		return err
	}
	if err := checkReplicas(ss.Status.UpdatedReplicas, "updated"); err != nil {
		return err
	}

	return nil
}

func newFilesystemPVC(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
		Spec: asc.spec.FileSystemStorage,
	}

	requests := getStorageRequests(&asc.spec.FileSystemStorage)

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, pvc, asc.rec.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		pvc.Spec.Resources.Requests = requests
		return nil
	}

	return pvc, mutateFn, nil
}

func newDatabasePVC(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: asc.namespace,
		},
		Spec: asc.spec.DatabaseStorage,
	}

	requests := getStorageRequests(&asc.spec.DatabaseStorage)

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, pvc, asc.rec.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		pvc.Spec.Resources.Requests = requests
		return nil
	}

	return pvc, mutateFn, nil
}

func newAgentService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, svc, asc.rec.Scheme); err != nil {
			return err
		}
		addAppLabel(serviceName, &svc.ObjectMeta)
		setAnnotation(&svc.ObjectMeta, servingCertAnnotation, serviceName)
		if len(svc.Spec.Ports) != 2 {
			svc.Spec.Ports = []corev1.ServicePort{}
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{}, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = serviceName
		svc.Spec.Ports[0].Port = int32(servicePort.IntValue())
		svc.Spec.Ports[0].TargetPort = servicePort
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Ports[1].Name = fmt.Sprintf("%s-http", serviceName)
		svc.Spec.Ports[1].Port = int32(serviceHTTPPort.IntValue())
		svc.Spec.Ports[1].TargetPort = serviceHTTPPort
		svc.Spec.Ports[1].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": serviceName}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return nil
	}

	return svc, mutateFn, nil
}

func newImageServiceService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, svc, asc.rec.Scheme); err != nil {
			return err
		}
		addAppLabel(serviceName, &svc.ObjectMeta)
		setAnnotation(&svc.ObjectMeta, servingCertAnnotation, imageServiceName)
		if len(svc.Spec.Ports) != 2 {
			svc.Spec.Ports = []corev1.ServicePort{}
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{}, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = imageServiceName
		svc.Spec.Ports[0].Port = int32(imageHandlerPort.IntValue())
		svc.Spec.Ports[0].TargetPort = imageHandlerPort
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Ports[1].Name = fmt.Sprintf("%s-http", imageServiceName)
		svc.Spec.Ports[1].Port = int32(imageHandlerHTTPPort.IntValue())
		svc.Spec.Ports[1].TargetPort = imageHandlerHTTPPort
		svc.Spec.Ports[1].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": imageServiceName}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return nil
	}

	return svc, mutateFn, nil
}

func newServiceMonitor(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, sm, asc.rec.Scheme); err != nil {
			return err
		}

		addAppLabel(serviceName, &sm.ObjectMeta)
		sm.Spec.Endpoints = []monitoringv1.Endpoint{{
			Port: serviceName,
			TLSConfig: &monitoringv1.TLSConfig{
				CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
				SafeTLSConfig: monitoringv1.SafeTLSConfig{
					ServerName: fmt.Sprintf("%s.%s.svc", serviceName, asc.namespace),
				},
			},
		}}
		sm.Spec.Selector = metav1.LabelSelector{
			MatchLabels: map[string]string{"app": serviceName},
		}
		return nil
	}

	return sm, mutateFn, nil
}

func newAgentRoute(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
	}
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   serviceName,
			Weight: &weight,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString(serviceName),
		},
		WildcardPolicy: routev1.WildcardPolicyNone,
		TLS:            &routev1.TLSConfig{Termination: routev1.TLSTerminationReencrypt},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, route, asc.rec.Scheme); err != nil {
			return err
		}
		// Only update what is specified above in routeSpec.
		// If we update the entire route.Spec with
		// route.Spec = routeSpec
		// it would overwrite any existing values for route.Spec.Host
		route.Spec.To = routeSpec.To
		route.Spec.Port = routeSpec.Port
		route.Spec.WildcardPolicy = routeSpec.WildcardPolicy
		route.Spec.TLS = routeSpec.TLS
		return nil
	}

	return route, mutateFn, nil
}

func newHTTPRoute(ctx context.Context, log logrus.FieldLogger, asc ASC, serviceToExpose string) (client.Object, controllerutil.MutateFn, error) {
	// In order to create plain http route we need https route to be created first to copy its host
	httpsRoute := &routev1.Route{}
	if err := asc.Client.Get(ctx, types.NamespacedName{Name: serviceToExpose, Namespace: asc.namespace}, httpsRoute); err != nil {
		log.WithError(err).Errorf("Failed to get https route for %s service", serviceToExpose)
		return nil, nil, err
	}
	if httpsRoute.Spec.Host == "" {
		log.Infof("https route for %s service found, but host not yet set", serviceToExpose)
		return nil, nil, nil
	}

	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ipxe", serviceToExpose),
			Namespace: asc.namespace,
		},
		Spec: routev1.RouteSpec{
			Host: httpsRoute.Spec.Host,
		},
	}
	routeSpec := routev1.RouteSpec{
		Path: "/",
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   serviceToExpose,
			Weight: &weight,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString(fmt.Sprintf("%s-http", serviceToExpose)),
		},
		WildcardPolicy: routev1.WildcardPolicyNone,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, route, asc.rec.Scheme); err != nil {
			return err
		}
		// Only update what is specified above in routeSpec.
		// If we update the entire route.Spec with
		// route.Spec = routeSpec
		// it would overwrite any existing values for route.Spec.Host
		route.Spec.To = routeSpec.To
		route.Spec.Port = routeSpec.Port
		route.Spec.WildcardPolicy = routeSpec.WildcardPolicy
		route.Spec.TLS = routeSpec.TLS
		route.Spec.Path = routeSpec.Path
		return nil
	}

	return route, mutateFn, nil
}

func removeHTTPIPXERoute(ctx context.Context, asc ASC, serviceToExpose string) error {
	route := &routev1.Route{}
	routeName := fmt.Sprintf("%s-ipxe", serviceToExpose)
	namespacedName := types.NamespacedName{Name: routeName, Namespace: asc.namespace}
	if err := asc.Client.Get(ctx, namespacedName, route); err == nil {
		err = asc.Client.Delete(ctx, &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      routeName,
				Namespace: asc.namespace,
			},
		})
		if !errors.IsNotFound(err) {
			return err
		} else {
			return nil
		}
	}
	return nil
}

func newAgentIPXERoute(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	return newHTTPRoute(ctx, log, asc, serviceName)
}

func newImageServiceRoute(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: asc.namespace,
		},
	}
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   imageServiceName,
			Weight: &weight,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString(imageServiceName),
		},
		WildcardPolicy: routev1.WildcardPolicyNone,
		TLS:            &routev1.TLSConfig{Termination: routev1.TLSTerminationReencrypt},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, route, asc.rec.Scheme); err != nil {
			return err
		}
		// Only update what is specified above in routeSpec.
		// If we update the entire route.Spec with
		// route.Spec = routeSpec
		// it would overwrite any existing values for route.Spec.Host
		route.Spec.To = routeSpec.To
		route.Spec.Port = routeSpec.Port
		route.Spec.WildcardPolicy = routeSpec.WildcardPolicy
		route.Spec.TLS = routeSpec.TLS
		return nil
	}

	return route, mutateFn, nil
}

func newImageServiceIPXERoute(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	return newHTTPRoute(ctx, log, asc, imageServiceName)
}

func newAgentLocalAuthSecret(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentLocalAuthSecretName,
			Namespace: asc.namespace,
			Labels: map[string]string{
				BackupLabel: BackupLabelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, secret, asc.rec.Scheme); err != nil {
			return err
		}
		_, privateKeyPresent := secret.Data["ec-private-key.pem"]
		_, publicKeyPresent := secret.Data["ec-public-key.pem"]
		if !privateKeyPresent && !publicKeyPresent {
			publicKey, privateKey, err := gencrypto.ECDSAKeyPairPEM()
			if err != nil {
				return err
			}
			if secret.Data == nil {
				secret.Data = map[string][]byte{}
			}
			secret.Data["ec-private-key.pem"] = []byte(privateKey)
			secret.Data["ec-public-key.pem"] = []byte(publicKey)
		}
		return nil
	}

	return secret, mutateFn, nil
}

func newPostgresSecret(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: asc.namespace,
			Labels: map[string]string{
				BackupLabel: BackupLabelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	mutateFn := func() error {
		err := controllerutil.SetControllerReference(asc.Object, secret, asc.rec.Scheme)
		if err != nil {
			return err
		}

		// the password should not change so only calculate it if a new object is going to be created
		var pass string
		if secret.ObjectMeta.CreationTimestamp.IsZero() {
			pass, err = generatePassword(databasePasswordLength)
			if err != nil {
				return err
			}

			secret.StringData = map[string]string{
				"db.host":     "localhost",
				"db.user":     "admin",
				"db.password": pass,
				"db.name":     "installer",
				"db.port":     databasePort.String(),
			}
		}
		return nil
	}

	return secret, mutateFn, nil
}

func newImageServiceServiceAccount(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		err := controllerutil.SetControllerReference(asc.Object, sa, asc.rec.Scheme)
		if err != nil {
			return err
		}

		return nil
	}

	return sa, mutateFn, nil
}

func newIngressCertCM(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	sourceCM := &corev1.ConfigMap{}

	if err := asc.Client.Get(ctx, types.NamespacedName{Name: defaultIngressCertCMName, Namespace: defaultIngressCertCMNamespace}, sourceCM); err != nil {
		log.WithError(err).Error("Failed to get default ingress cert config map")
		return nil, nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultIngressCertCMName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, cm, asc.rec.Scheme); err != nil {
			return err
		}
		cm.Data = make(map[string]string)
		for k, v := range sourceCM.Data {
			cm.Data[k] = v
		}
		return nil
	}

	return cm, mutateFn, nil
}

func newImageServiceConfigMap(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, cm, asc.rec.Scheme); err != nil {
			return err
		}

		setAnnotation(&cm.ObjectMeta, injectCABundleAnnotation, "true")
		return nil
	}

	return cm, mutateFn, nil
}

func urlForRoute(ctx context.Context, asc ASC, routeName string) (string, error) {
	route := &routev1.Route{}
	err := asc.Client.Get(ctx, types.NamespacedName{Name: routeName, Namespace: asc.namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("%s route host is empty", routeName)
		}
		return "", err
	}

	u := &url.URL{Scheme: "https", Host: route.Spec.Host}
	return u.String(), nil
}

// unauthenticatedRegistries appends mirror registries and user-specified unauthenticated registries to the default list
func unauthenticatedRegistries(ctx context.Context, asc ASC) string {
	unauthenticatedRegistries := []string{"quay.io", "registry.svc.ci.openshift.org"}
	if asc.spec.MirrorRegistryRef != nil {
		cm := &corev1.ConfigMap{}
		// Any errors in the following code block is not handled since they indicate a problem with the
		// format of the mirror registry config, and an incorrectly formatted config does not mean that
		// the public container registries should not be set.
		if err := asc.Client.Get(ctx, types.NamespacedName{Name: asc.spec.MirrorRegistryRef.Name, Namespace: asc.namespace}, cm); err == nil {
			if contents, ok := cm.Data[mirrorRegistryRefRegistryConfKey]; ok {
				if tomlTree, err := toml.Load(contents); err == nil {
					if registries, ok := tomlTree.Get("unqualified-search-registries").([]interface{}); ok {
						for _, registry := range registries {
							if registryStr, ok := registry.(string); ok {
								unauthenticatedRegistries = append(unauthenticatedRegistries, registryStr)
							}
						}
					}
				}
			}
		}
	}
	if asc.spec.UnauthenticatedRegistries != nil {
		unauthenticatedRegistries = append(unauthenticatedRegistries, asc.spec.UnauthenticatedRegistries...)
	}

	return strings.Join(funk.UniqString(unauthenticatedRegistries), ",")
}

//go:embed default_controller_hw_requirements.json
var defaultControllerHardwareRequirements string

func newAssistedCM(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	serviceURL, err := urlForRoute(ctx, asc, serviceName)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", serviceName)
		return nil, nil, err
	}

	imageServiceURL, err := urlForRoute(ctx, asc, imageServiceName)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", imageServiceName)
		return nil, nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, cm, asc.rec.Scheme); err != nil {
			return err
		}

		cm.Data = map[string]string{
			"SERVICE_BASE_URL":       serviceURL,
			"IMAGE_SERVICE_BASE_URL": imageServiceURL,

			// image overrides
			"AGENT_DOCKER_IMAGE":     AgentImage(),
			"CONTROLLER_IMAGE":       ControllerImage(),
			"INSTALLER_IMAGE":        InstallerImage(),
			"SELF_VERSION":           ServiceImage(),
			"OS_IMAGES":              getOSImages(log, asc.spec),
			"MUST_GATHER_IMAGES":     getMustGatherImages(log, asc.spec),
			"ISO_IMAGE_TYPE":         "minimal-iso",
			"S3_USE_SSL":             "false",
			"LOG_LEVEL":              "info",
			"LOG_FORMAT":             "text",
			"INSTALL_RH_CA":          "false",
			"REGISTRY_CREDS":         "",
			"DEPLOY_TARGET":          "k8s",
			"STORAGE":                "filesystem",
			"ISO_WORKSPACE_BASE_DIR": "/data",
			"ISO_CACHE_DIR":          "/data/cache",

			// from configmap
			"AUTH_TYPE":                   "local",
			"BASE_DNS_DOMAINS":            "",
			"CHECK_CLUSTER_VERSION":       "True",
			"CREATE_S3_BUCKET":            "False",
			"ENABLE_KUBE_API":             "True",
			"ENABLE_AUTO_ASSIGN":          "True",
			"ENABLE_SINGLE_NODE_DNSMASQ":  "True",
			"IPV6_SUPPORT":                "True",
			"JWKS_URL":                    "https://api.openshift.com/.well-known/jwks.json",
			"PUBLIC_CONTAINER_REGISTRIES": unauthenticatedRegistries(ctx, asc),
			"HW_VALIDATOR_REQUIREMENTS":   defaultControllerHardwareRequirements,
			// The user may opt-out by using the telemetry opt-out method (removing cloud.openshift.com from their OCM pull secret)
			"ENABLE_DATA_COLLECTION": "True",
			"DATA_UPLOAD_ENDPOINT":   "https://console.redhat.com/api/ingress/v1/upload",

			"NAMESPACE":       asc.namespace,
			"INSTALL_INVOKER": "assisted-installer-operator",

			// enable https
			"SERVE_HTTPS":            "True",
			"HTTPS_CERT_FILE":        "/etc/assisted-tls-config/tls.crt",
			"HTTPS_KEY_FILE":         "/etc/assisted-tls-config/tls.key",
			"SERVICE_CA_CERT_PATH":   "/etc/assisted-ingress-cert/ca-bundle.crt",
			"SKIP_CERT_VERIFICATION": "False",
		}

		copyEnv(cm.Data, "HTTP_PROXY")
		copyEnv(cm.Data, "HTTPS_PROXY")
		copyEnv(cm.Data, "NO_PROXY")
		return nil
	}

	return cm, mutateFn, nil
}

func ensureVolume(volumes []corev1.Volume, vol corev1.Volume) []corev1.Volume {
	var found bool
	for i := range volumes {
		if volumes[i].Name == vol.Name {
			found = true
			volumes[i].VolumeSource = vol.VolumeSource
			break
		}
	}
	if !found {
		volumes = append(volumes, vol)
	}
	return volumes
}

func newImageServiceStatefulSet(ctx context.Context, log logrus.FieldLogger, asc ASC) (*appsv1.StatefulSet, controllerutil.MutateFn) {
	skipVerifyTLS, ok := asc.Object.GetAnnotations()[imageServiceSkipVerifyTLSAnnotation]
	if !ok {
		skipVerifyTLS = "false"
	}

	deploymentLabels := map[string]string{
		"app": imageServiceName,
	}

	imageServiceBaseURL := getImageService(ctx, log, asc)

	container := corev1.Container{
		Name:  imageServiceName,
		Image: ImageServiceImage(),
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: int32(imageHandlerPort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
			{
				ContainerPort: int32(imageHandlerHTTPPort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			{Name: "LISTEN_PORT", Value: imageHandlerPort.String()},
			{Name: "HTTP_LISTEN_PORT", Value: imageHandlerHTTPPort.String()},
			{Name: "RHCOS_VERSIONS", Value: getOSImages(log, asc.spec)},
			{Name: "HTTPS_CERT_FILE", Value: "/etc/image-service/certs/tls.crt"},
			{Name: "HTTPS_KEY_FILE", Value: "/etc/image-service/certs/tls.key"},
			{Name: "HTTPS_CA_FILE", Value: "/etc/image-service/ca-bundle/service-ca.crt"},
			{Name: "ASSISTED_SERVICE_SCHEME", Value: "https"},
			{Name: "ASSISTED_SERVICE_HOST", Value: serviceName + "." + asc.namespace + ".svc:" + servicePort.String()},
			{Name: "IMAGE_SERVICE_BASE_URL", Value: imageServiceBaseURL},
			{Name: "INSECURE_SKIP_VERIFY", Value: skipVerifyTLS},
			{Name: "DATA_DIR", Value: "/data"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "tls-certs", MountPath: "/etc/image-service/certs"},
			{Name: "service-cabundle", MountPath: "/etc/image-service/ca-bundle"},
			{Name: "image-service-data", MountPath: "/data"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   imageHandlerPort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 30,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/live",
					Port:   imageHandlerPort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if value, ok := os.LookupEnv(key); ok {
			container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: value})
		}
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: asc.namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
					Name:   imageServiceName,
				},
			},
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, statefulSet, asc.rec.Scheme); err != nil {
			return err
		}
		controllerutil.AddFinalizer(statefulSet, imageServiceStatefulSetFinalizerName)

		var replicas int32 = 1
		statefulSet.Spec.Replicas = &replicas

		statefulSet.Spec.Template.Spec.Containers = []corev1.Container{container}
		statefulSet.Spec.Template.Spec.ServiceAccountName = imageServiceName

		volumes := ensureVolume(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: imageServiceName,
				},
			},
		})
		volumes = ensureVolume(volumes, corev1.Volume{
			Name: "service-cabundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: imageServiceName,
					},
				},
			},
		})

		if asc.spec.ImageStorage != nil {
			var found bool
			for i, claim := range statefulSet.Spec.VolumeClaimTemplates {
				if claim.ObjectMeta.Name == "image-service-data" {
					found = true
					statefulSet.Spec.VolumeClaimTemplates[i].Spec.Resources.Requests = getStorageRequests(asc.spec.ImageStorage)
				}
			}
			if !found {
				statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "image-service-data",
						},
						Spec: *asc.spec.ImageStorage,
					},
				}
			}
			newVols := make([]corev1.Volume, 0)
			for i := range volumes {
				if volumes[i].Name != "image-service-data" {
					newVols = append(newVols, volumes[i])
				}
			}
			volumes = newVols
		} else {
			statefulSet.Spec.VolumeClaimTemplates = nil

			volumes = ensureVolume(volumes, corev1.Volume{
				Name: "image-service-data",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}

		statefulSet.Spec.Template.Spec.Volumes = volumes

		if asc.rec.NodeSelector != nil {
			statefulSet.Spec.Template.Spec.NodeSelector = asc.rec.NodeSelector
		} else {
			statefulSet.Spec.Template.Spec.NodeSelector = map[string]string{}
		}

		if asc.rec.Tolerations != nil {
			statefulSet.Spec.Template.Spec.Tolerations = asc.rec.Tolerations
		} else {
			statefulSet.Spec.Template.Spec.Tolerations = []corev1.Toleration{}
		}
		return nil
	}

	return statefulSet, mutateFn
}

func cleanupImageServiceFinalizer(ctx context.Context, asc ASC, statefulSet *appsv1.StatefulSet) error {
	if !controllerutil.ContainsFinalizer(statefulSet, imageServiceStatefulSetFinalizerName) {
		return nil
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := asc.Client.List(ctx, pvcList, client.MatchingLabels{"app": imageServiceName}); err != nil {
		return err
	}

	for i := range pvcList.Items {
		if err := asc.Client.Delete(ctx, &pvcList.Items[i]); err != nil {
			return err
		}
	}

	controllerutil.RemoveFinalizer(statefulSet, imageServiceStatefulSetFinalizerName)
	if err := asc.Client.Update(ctx, statefulSet); err != nil {
		return err
	}

	return nil
}

func ensureImageServiceStatefulSet(ctx context.Context, log logrus.FieldLogger, asc ASC) error {
	if err := reconcileImageServiceStatefulSet(ctx, log, asc); err != nil {
		msg := "Failed to reconcile image-service StatefulSet"
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonImageHandlerStatefulSetFailure,
			Message: msg,
		})
		if statusErr := asc.Client.Status().Update(ctx, asc.Object); statusErr != nil {
			log.WithError(statusErr).Error("Failed to update status")
			return statusErr
		}
	}
	return nil
}

func reconcileImageServiceStatefulSet(ctx context.Context, log logrus.FieldLogger, asc ASC) error {
	var err error
	defer func() {
		// delete old deployment if it exists and we've created the new stateful set correctly
		// TODO: this can be removed when we no longer support upgrading from a release that used deployments for the image service
		// NOTE: this relies on the err local variable being set correctly when the function returns
		if err == nil {
			_ = asc.Client.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      imageServiceName,
					Namespace: asc.namespace,
				},
			})
		}
	}()

	statefulSet, mutateFn := newImageServiceStatefulSet(ctx, log, asc)

	key := client.ObjectKeyFromObject(statefulSet)
	if err = asc.Client.Get(ctx, key, statefulSet); err != nil {
		// if the statefulset doesn't exist, create it
		if !errors.IsNotFound(err) {
			return err
		}
		log.Info("Creating image service stateful set")
		if err = mutateFn(); err != nil {
			return err
		}
		if err = asc.Client.Create(ctx, statefulSet); err != nil {
			return err
		}
		return nil
	}

	if !statefulSet.DeletionTimestamp.IsZero() {
		if err = cleanupImageServiceFinalizer(ctx, asc, statefulSet); err != nil {
			return err
		}
		return nil
	}

	existing := statefulSet.DeepCopyObject().(*appsv1.StatefulSet)
	if err = mutateFn(); err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(existing, statefulSet) {
		// no update needed
		return nil
	}

	if equality.Semantic.DeepEqual(existing.Spec.VolumeClaimTemplates, statefulSet.Spec.VolumeClaimTemplates) {
		log.Info("Updating image service statful set in-place")
		// if we're updating something other than the volumes, just do a regular update
		if err = asc.Client.Update(ctx, statefulSet); err != nil {
			return err
		}
		return nil
	}

	log.Info("Deleting image service stateful set on volume claim template update")

	// need to delete and re-create statefulset because the volumes have changed
	if err = asc.Client.Delete(ctx, existing); err != nil {
		return err
	}

	return nil
}

func newAssistedServiceDeployment(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	var assistedConfigHash, mirrorConfigHash, userConfigHash string

	// Get hash of generated assisted config
	assistedConfigHash, err := getCMHash(ctx, asc, types.NamespacedName{Name: serviceName, Namespace: asc.namespace})
	if err != nil {
		return nil, nil, err
	}

	envSecrets := []corev1.EnvVar{
		// database
		newSecretEnvVar("DB_HOST", "db.host", databaseName),
		newSecretEnvVar("DB_NAME", "db.name", databaseName),
		newSecretEnvVar("DB_PASS", "db.password", databaseName),
		newSecretEnvVar("DB_PORT", "db.port", databaseName),
		newSecretEnvVar("DB_USER", "db.user", databaseName),

		// local auth secret
		newSecretEnvVar("EC_PUBLIC_KEY_PEM", "ec-public-key.pem", agentLocalAuthSecretName),
		newSecretEnvVar("EC_PRIVATE_KEY_PEM", "ec-private-key.pem", agentLocalAuthSecretName),
	}

	if exposeIPXEHTTPRoute(asc.spec) {
		envSecrets = append(envSecrets, corev1.EnvVar{Name: "HTTP_LISTEN_PORT", Value: serviceHTTPPort.String()})
	}

	envFrom := []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: serviceName,
				},
			},
		},
	}

	// User is responsible for:
	// - knowing to restart assisted-service when specifying the configmap via annotation
	// - removing the annotation when the configmap is deleted
	userConfigName, ok := asc.Object.GetAnnotations()[configmapAnnotation]
	if ok {
		log.Infof("ConfigMap %s from namespace %s being used to configure assisted-service deployment", userConfigName, asc.namespace)
		userConfigHash, err = getCMHash(ctx, asc, types.NamespacedName{Name: userConfigName, Namespace: asc.namespace})
		if err != nil {
			return nil, nil, err
		}

		envFrom = append(envFrom, []corev1.EnvFromSource{
			{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: userConfigName,
					},
				},
			},
		}...,
		)
	}

	serviceContainer := corev1.Container{
		Name:  serviceName,
		Image: ServiceImage(),
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: int32(servicePort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
			{
				ContainerPort: int32(serviceHTTPPort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		EnvFrom: envFrom,
		Env:     envSecrets,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "bucket-filesystem", MountPath: "/data"},
			{Name: "tls-certs", MountPath: "/etc/assisted-tls-config"},
			{Name: "ingress-cert", MountPath: "/etc/assisted-ingress-cert"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 30,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   servicePort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/ready",
					Port:   servicePort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	postgresContainer := corev1.Container{
		Name:  databaseName,
		Image: DatabaseImage(),
		Ports: []corev1.ContainerPort{
			{
				Name:          databaseName,
				ContainerPort: int32(databasePort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			newSecretEnvVar("POSTGRESQL_DATABASE", "db.name", databaseName),
			newSecretEnvVar("POSTGRESQL_USER", "db.user", databaseName),
			newSecretEnvVar("POSTGRESQL_PASSWORD", "db.password", databaseName),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "postgresdb",
				MountPath: "/var/lib/pgsql/data",
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "bucket-filesystem",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: serviceName,
				},
			},
		},
		{
			Name: "postgresdb",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: databaseName,
				},
			},
		},
		{
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: serviceName,
				},
			},
		},
		{
			Name: "ingress-cert",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaultIngressCertCMName,
					},
				},
			},
		},
	}

	if asc.spec.MirrorRegistryRef != nil {
		cm := &corev1.ConfigMap{}
		namespacedName := types.NamespacedName{Name: asc.spec.MirrorRegistryRef.Name, Namespace: asc.namespace}
		err := asc.Client.Get(ctx, namespacedName, cm)
		if err != nil {
			return nil, nil, err
		}

		mirrorConfigHash, err = checksumMap(cm.Data)
		if err != nil {
			return nil, nil, err
		}

		// require that the registries config key be specified before continuing
		if _, ok := cm.Data[mirrorRegistryRefRegistryConfKey]; !ok {
			err = fmt.Errorf("Mirror registry configmap %s missing key %s", asc.spec.MirrorRegistryRef.Name, mirrorRegistryRefRegistryConfKey)
			return nil, nil, err
		}

		// make sure configmap is being backed up
		if err := ensureConfigMapIsLabelled(ctx, asc.Client, cm, namespacedName); err != nil {
			return nil, nil, pkgerror.Wrapf(err, "Unable to mark mirror configmap for backup")
		}

		volume := corev1.Volume{
			Name: mirrorRegistryConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: *asc.spec.MirrorRegistryRef,
					DefaultMode:          swag.Int32(420),
					Items: []corev1.KeyToPath{
						{
							Key:  mirrorRegistryRefRegistryConfKey,
							Path: common.MirrorRegistriesConfigFile,
						},
					},
				},
			},
		}

		serviceContainer.VolumeMounts = append(
			serviceContainer.VolumeMounts,
			corev1.VolumeMount{
				Name:      mirrorRegistryConfigVolume,
				MountPath: common.MirrorRegistriesConfigDir,
			},
		)

		if _, ok := cm.Data[mirrorRegistryRefCertKey]; ok {
			volume.VolumeSource.ConfigMap.Items = append(
				volume.VolumeSource.ConfigMap.Items,
				corev1.KeyToPath{
					Key:  mirrorRegistryRefCertKey,
					Path: common.MirrorRegistriesCertificateFile,
				},
			)

			serviceContainer.VolumeMounts = append(
				serviceContainer.VolumeMounts,
				corev1.VolumeMount{
					Name:      mirrorRegistryConfigVolume,
					MountPath: common.MirrorRegistriesCertificatePath,
					SubPath:   common.MirrorRegistriesCertificateFile,
				},
			)
		}

		// add our mirror registry config to volumes
		volumes = append(volumes, volume)
	}

	deploymentLabels := map[string]string{
		"app": serviceName,
	}

	deploymentStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RecreateDeploymentStrategyType,
	}

	serviceAccountName := ServiceAccountName()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
					Name:   serviceName,
				},
			},
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, deployment, asc.rec.Scheme); err != nil {
			return err
		}
		var replicas int32 = 1
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Strategy = deploymentStrategy

		// Handle our hashed configMap(s)
		meta := &deployment.Spec.Template.ObjectMeta
		setAnnotation(meta, assistedConfigHashAnnotation, assistedConfigHash)
		setAnnotation(meta, mirrorConfigHashAnnotation, mirrorConfigHash)
		setAnnotation(meta, userConfigHashAnnotation, userConfigHash)

		deployment.Spec.Template.Spec.Containers = []corev1.Container{serviceContainer, postgresContainer}
		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccountName

		if asc.rec.NodeSelector != nil {
			deployment.Spec.Template.Spec.NodeSelector = asc.rec.NodeSelector
		} else {
			deployment.Spec.Template.Spec.NodeSelector = map[string]string{}
		}

		if asc.rec.Tolerations != nil {
			deployment.Spec.Template.Spec.Tolerations = asc.rec.Tolerations
		} else {
			deployment.Spec.Template.Spec.Tolerations = []corev1.Toleration{}
		}

		return nil
	}
	return deployment, mutateFn, nil
}

func copyEnv(config map[string]string, key string) {
	if value, ok := os.LookupEnv(key); ok {
		config[key] = value
	}
}

func checkIngressCMName(obj metav1.Object) bool {
	return obj.GetNamespace() == defaultIngressCertCMNamespace && obj.GetName() == defaultIngressCertCMName
}

// getMustGatherImages returns the value of MUST_GATHER_IMAGES variable
// to be stored in the service's ConfigMap
//
//  1. If mustGatherImages field is not present in the AgentServiceConfig's Spec
//     it returns the value of MUST_GATHER_IMAGES env variable. This is also the
//     fallback behavior in case of a processing error
//
//  2. If mustGatherImages field is present in the AgentServiceConfig's Spec it
//     converts the structure to the one that can be recognize by the service
//     and returns it as a JSON string
//
//  3. In case both sources are present, the Spec values overrides the env
//     values
func getMustGatherImages(log logrus.FieldLogger, spec *aiv1beta1.AgentServiceConfigSpec) string {
	if spec.MustGatherImages == nil {
		return MustGatherImages()
	}
	mustGatherVersions := make(versions.MustGatherVersions)
	for _, specImage := range spec.MustGatherImages {
		versionKey, err := getVersionKey(specImage.OpenshiftVersion)
		if err != nil {
			log.WithError(err).Error(fmt.Sprintf("Problem parsing OpenShift version %v, skipping.", specImage.OpenshiftVersion))
			continue
		}
		if mustGatherVersions[versionKey] == nil {
			mustGatherVersions[versionKey] = make(versions.MustGatherVersion)
		}
		mustGatherVersions[versionKey][specImage.Name] = specImage.Url
	}

	if len(mustGatherVersions) == 0 {
		return MustGatherImages()
	}
	encodedVersions, err := json.Marshal(mustGatherVersions)
	if err != nil {
		log.WithError(err).Error(fmt.Sprintf("Problem marshaling must gather images (%v) to string, returning default %v", mustGatherVersions, MustGatherImages()))
		return MustGatherImages()
	}

	return string(encodedVersions)
}

// getOSImages returns the value of OS_IMAGES variable
// to be stored in the service's ConfigMap
//
//  1. If osImages field is not present in the AgentServiceConfig's Spec
//     it returns the value of OS_IMAGES env variable.
//     This is also the fallback behavior in case of a processing error.
//
//  2. If osImages field is present in the AgentServiceConfig's Spec it
//     converts the structure to the one that can be recognize by the service
//     and returns it as a JSON string.
//
//  3. In case both sources are present, the Spec values overrides the env
//     values.
func getOSImages(log logrus.FieldLogger, spec *aiv1beta1.AgentServiceConfigSpec) string {
	if spec.OSImages == nil {
		return OSImages()
	}

	osImages := make(models.OsImages, 0)
	for i := range spec.OSImages {
		osImage := models.OsImage{
			OpenshiftVersion: &spec.OSImages[i].OpenshiftVersion,
			URL:              &spec.OSImages[i].Url,
			Version:          &spec.OSImages[i].Version,
			CPUArchitecture:  &spec.OSImages[i].CPUArchitecture,
		}
		osImages = append(osImages, &osImage)
	}

	if len(osImages) == 0 {
		log.Info("No valid OS Image specified, returning default", "OS Images", OSImages())
		return OSImages()
	}

	encodedOSImages, err := json.Marshal(osImages)
	if err != nil {
		log.WithError(err).Error(fmt.Sprintf("Problem marshaling OSImages (%v) to string, returning default %v", osImages, OSImages()))
		return OSImages()
	}

	return string(encodedOSImages)
}

// exposeIPXEHTTPRoute returns true if spec.IPXEHTTPRoute is set to true
func exposeIPXEHTTPRoute(spec *aiv1beta1.AgentServiceConfigSpec) bool {
	switch spec.IPXEHTTPRoute {
	case aiv1beta1.IPXEHTTPRouteEnabled:
		return true
	case aiv1beta1.IPXEHTTPRouteDisabled:
		return false
	default:
		return false
	}
}

func getCMHash(ctx context.Context, asc ASC, namespacedName types.NamespacedName) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := asc.Client.Get(ctx, namespacedName, cm); err != nil {
		return "", err
	}
	return checksumMap(cm.Data)
}

func getVersionKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return openshiftVersion, err
	}

	// put string in x.y format
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

func newSecretEnvVar(name, key, secretName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				Key: key,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
			},
		},
	}
}

func newInfraEnvWebHook(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	fp := admregv1.Fail
	se := admregv1.SideEffectClassNone
	path := "/apis/admission.agentinstall.openshift.io/v1/infraenvvalidators"
	webhooks := []admregv1.ValidatingWebhook{
		{
			Name:          "infraenvvalidators.admission.agentinstall.openshift.io",
			FailurePolicy: &fp,
			SideEffects:   &se,
			AdmissionReviewVersions: []string{
				"v1",
			},
			ClientConfig: admregv1.WebhookClientConfig{
				Service: &admregv1.ServiceReference{
					Namespace: defaultNamespace,
					Name:      "kubernetes",
					Path:      &path,
				},
			},
			Rules: []admregv1.RuleWithOperations{
				{
					Operations: []admregv1.OperationType{
						admregv1.Update,
					},
					Rule: admregv1.Rule{
						APIGroups: []string{
							"agent-install.openshift.io",
						},
						APIVersions: []string{
							"v1beta1",
						},
						Resources: []string{
							"infraenvs",
						},
					},
				},
			},
		},
	}

	aci := admregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "infraenvvalidators.admission.agentinstall.openshift.io",
		},
		Webhooks: webhooks,
	}

	mutateFn := func() error {
		aci.Webhooks = webhooks
		return nil
	}
	return &aci, mutateFn, nil
}

func newAgentWebHook(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	fp := admregv1.Fail
	se := admregv1.SideEffectClassNone
	path := "/apis/admission.agentinstall.openshift.io/v1/agentvalidators"
	webhooks := []admregv1.ValidatingWebhook{
		{
			Name:          "agentvalidators.admission.agentinstall.openshift.io",
			FailurePolicy: &fp,
			SideEffects:   &se,
			AdmissionReviewVersions: []string{
				"v1",
			},
			ClientConfig: admregv1.WebhookClientConfig{
				Service: &admregv1.ServiceReference{
					Namespace: defaultNamespace,
					Name:      "kubernetes",
					Path:      &path,
				},
			},
			Rules: []admregv1.RuleWithOperations{
				{
					Operations: []admregv1.OperationType{
						admregv1.Update,
					},
					Rule: admregv1.Rule{
						APIGroups: []string{
							"agent-install.openshift.io",
						},
						APIVersions: []string{
							"v1beta1",
						},
						Resources: []string{
							"agents",
						},
					},
				},
			},
		},
	}

	agent := admregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentvalidators.admission.agentinstall.openshift.io",
		},
		Webhooks: webhooks,
	}

	mutateFn := func() error {
		agent.Webhooks = webhooks
		return nil
	}
	return &agent, mutateFn, nil
}

func newACIMutatWebHook(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	fp := admregv1.Fail
	se := admregv1.SideEffectClassNone
	path := "/apis/admission.agentinstall.openshift.io/v1/agentclusterinstallmutators"
	webhooks := []admregv1.MutatingWebhook{
		{
			Name:          "agentclusterinstallmutators.admission.agentinstall.openshift.io",
			FailurePolicy: &fp,
			SideEffects:   &se,
			AdmissionReviewVersions: []string{
				"v1",
			},
			ClientConfig: admregv1.WebhookClientConfig{
				Service: &admregv1.ServiceReference{
					Namespace: defaultNamespace,
					Name:      "kubernetes",
					Path:      &path,
				},
			},
			Rules: []admregv1.RuleWithOperations{
				{
					Operations: []admregv1.OperationType{
						admregv1.Update,
						admregv1.Create,
					},
					Rule: admregv1.Rule{
						APIGroups: []string{
							"extensions.hive.openshift.io",
						},
						APIVersions: []string{
							"v1beta1",
						},
						Resources: []string{
							"agentclusterinstalls",
						},
					},
				},
			},
		},
	}

	aci := admregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentclusterinstallmutators.admission.agentinstall.openshift.io",
		},
		Webhooks: webhooks,
	}

	mutateFn := func() error {
		aci.Webhooks = webhooks
		return nil
	}
	return &aci, mutateFn, nil
}

func newACIWebHook(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	fp := admregv1.Fail
	se := admregv1.SideEffectClassNone
	path := "/apis/admission.agentinstall.openshift.io/v1/agentclusterinstallvalidators"
	webhooks := []admregv1.ValidatingWebhook{
		{
			Name:          "agentclusterinstallvalidators.admission.agentinstall.openshift.io",
			FailurePolicy: &fp,
			SideEffects:   &se,
			AdmissionReviewVersions: []string{
				"v1",
			},
			ClientConfig: admregv1.WebhookClientConfig{
				Service: &admregv1.ServiceReference{
					Namespace: defaultNamespace,
					Name:      "kubernetes",
					Path:      &path,
				},
			},
			Rules: []admregv1.RuleWithOperations{
				{
					Operations: []admregv1.OperationType{
						admregv1.Update,
						admregv1.Create,
					},
					Rule: admregv1.Rule{
						APIGroups: []string{
							"extensions.hive.openshift.io",
						},
						APIVersions: []string{
							"v1beta1",
						},
						Resources: []string{
							"agentclusterinstalls",
						},
					},
				},
			},
		},
	}

	aci := admregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentclusterinstallvalidators.admission.agentinstall.openshift.io",
		},
		Webhooks: webhooks,
	}

	mutateFn := func() error {
		aci.Webhooks = webhooks
		return nil
	}
	return &aci, mutateFn, nil
}

func newWebHookServiceAccount(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	return createServiceAccountFn("agentinstalladmission", asc.namespace)
}

func newWebHookClusterRoleBinding(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	roleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     "system:openshift:assisted-installer:agentinstalladmission",
	}
	subjects := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Namespace: asc.namespace,
			Name:      "agentinstalladmission",
		},
	}
	crb := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentinstalladmission-agentinstall-agentinstalladmission",
		},
		RoleRef:  roleRef,
		Subjects: subjects,
	}

	mutateFn := func() error {
		crb.Subjects = subjects
		crb.RoleRef = roleRef
		return nil
	}
	return &crb, mutateFn, nil
}

func newWebHookClusterRole(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"configmaps",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"admissionregistration.k8s.io",
			},
			Resources: []string{
				"validatingwebhookconfigurations",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"namespaces",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"authorization.k8s.io",
			},
			Resources: []string{
				"subjectaccessreviews",
			},
			Verbs: []string{
				"create",
			},
		},
	}
	cr := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "system:openshift:assisted-installer:agentinstalladmission",
		},
		Rules: rules,
	}

	mutateFn := func() error {
		cr.Rules = rules
		return nil
	}
	return &cr, mutateFn, nil
}

func newWebHookService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, svc, asc.rec.Scheme); err != nil {
			return err
		}
		addAppLabel(webhookServiceName, &svc.ObjectMeta)
		setAnnotation(&svc.ObjectMeta, servingCertAnnotation, webhookServiceName)
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = webhookServiceName
		svc.Spec.Ports[0].Port = 443
		svc.Spec.Ports[0].TargetPort = intstr.IntOrString{Type: intstr.Int, IntVal: 9443}
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": webhookServiceName}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return nil
	}

	return svc, mutateFn, nil
}

func baseApiServiceSpec(as *apiregv1.APIService, namespace string) {
	as.Spec.Group = "admission.agentinstall.openshift.io"
	as.Spec.GroupPriorityMinimum = 1000
	as.Spec.VersionPriority = 15
	as.Spec.Version = "v1"
	as.Spec.Service = &apiregv1.ServiceReference{
		Name:      "agentinstalladmission",
		Namespace: namespace,
	}
}

func newWebHookAPIService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	as := &apiregv1.APIService{
		ObjectMeta: metav1.ObjectMeta{
			Name: "v1.admission.agentinstall.openshift.io",
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, as, asc.rec.Scheme); err != nil {
			return err
		}

		setAnnotation(&as.ObjectMeta, "service.beta.openshift.io/inject-cabundle", "true")
		baseApiServiceSpec(as, asc.namespace)
		return nil
	}
	return as, mutateFn, nil
}

func newWebHookDeployment(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	serviceContainer := corev1.Container{
		Name:  "agentinstalladmission",
		Image: ServiceImage(),
		Command: []string{
			"/assisted-service-admission",
			"--secure-port=9443",
			"--audit-log-path=-",
			"--tls-cert-file=/var/serving-cert/tls.crt",
			"--tls-private-key-file=/var/serving-cert/tls.key",
		},
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 9443,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "serving-cert", MountPath: "/var/serving-cert"},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/healthz",
					Port:   intstr.FromInt(int(9443)),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "serving-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "agentinstalladmission",
				},
			},
		},
	}

	deploymentLabels := map[string]string{
		"app":                   "agentinstalladmission",
		"agentinstalladmission": "true",
	}

	deploymentStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RecreateDeploymentStrategyType,
	}

	serviceAccountName := "agentinstalladmission"

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentinstalladmission",
			Namespace: asc.namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
					Name:   "agentinstalladmission",
				},
			},
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, deployment, asc.rec.Scheme); err != nil {
			return err
		}
		var replicas int32 = 2
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Strategy = deploymentStrategy

		deployment.Spec.Template.Spec.Containers = []corev1.Container{serviceContainer}
		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccountName

		return nil
	}
	return deployment, mutateFn, nil
}

func getStorageRequests(pvcSpec *corev1.PersistentVolumeClaimSpec) map[corev1.ResourceName]resource.Quantity {
	requests := map[corev1.ResourceName]resource.Quantity{}
	for key, value := range pvcSpec.Resources.Requests {
		requests[key] = value
	}
	return requests
}

func getImageService(ctx context.Context, log logrus.FieldLogger, asc ASC) string {
	imageServiceURL, err := urlForRoute(ctx, asc, imageServiceName)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", imageServiceName)
		return ""
	}
	return imageServiceURL
}

func validate(ctx context.Context, log logrus.FieldLogger, asc ASC) (bool, error) {
	// Validate the storage configuration. If that returns warnings then generate the
	// corresponding events. If it returns errors then update the conditions and stop the
	// reconciliation.
	warnings, failures, err := validateStorage(ctx, log, asc)
	if err != nil {
		log.WithError(err).Error("Failed to validate storage configuration")
		return false, err
	}
	for _, warning := range warnings {
		asc.rec.Recorder.Event(asc.Object, "Warning", aiv1beta1.ReasonStorageFailure, warning)
	}
	if len(failures) > 0 {
		log.Error("Storage configuration isn't valid")
		conditionsv1.SetStatusConditionNoHeartbeat(
			asc.conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ConditionReconcileCompleted,
				Status:  corev1.ConditionFalse,
				Reason:  aiv1beta1.ReasonStorageFailure,
				Message: fmt.Sprintf("%s.", strings.Join(failures, ". ")),
			},
		)
		err = asc.Client.Status().Update(ctx, asc.Object)
		if err != nil {
			log.WithError(err).Error("Failed to update status")
			return false, err
		}
		return false, nil
	}

	// If we are here then the storage configuration validation succeeded, so we may need to
	// remove a previous failure condition:
	condition := conditionsv1.FindStatusCondition(
		*asc.conditions,
		aiv1beta1.ConditionReconcileCompleted,
	)
	if condition != nil && condition.Reason == aiv1beta1.ReasonStorageFailure {
		conditionsv1.RemoveStatusCondition(
			asc.conditions,
			aiv1beta1.ConditionReconcileCompleted,
		)
	}

	return true, nil
}

// validateStorage checks that the sizes of the storage volumes for the database, the file system
// and the images are acceptable.
//
// Returns two slices of strings containing warnings and failures. Warnings are intended for
// creation of events. Failures are intended for reporting in conditions, and for stopping the
// reconciliation.
//
// If a volume size is smaller than the minimum it will generate a warning. It will generate an
// error only if the corresponding persistent volume claim (or the stateful set for the image
// storage) has not been created yet. This is for backwards compatility, to prevent breaking
// environments that were created before this validation was introduced. Those environments may be
// working correctly even if the size was smaller that the minimum, because they aren't consuming
// that space or because the actual volume was larger then the initial request.
func validateStorage(ctx context.Context, log logrus.FieldLogger, asc ASC) (warnings, failures []string, err error) {

	// Check the size of the database storage:
	databaseStorage := asc.spec.DatabaseStorage.Resources.Requests.Storage()
	if databaseStorage.Cmp(minDatabaseStorage) < 0 {
		message := fmt.Sprintf(
			"Database storage %s is too small, it must be at least %s",
			databaseStorage, &minDatabaseStorage,
		)
		warnings = append(warnings, message)
		key := client.ObjectKey{
			Namespace: asc.namespace,
			Name:      databaseName,
		}
		var tmp corev1.PersistentVolumeClaim
		err = asc.Client.Get(ctx, key, &tmp)
		if errors.IsNotFound(err) {
			err = nil
			failures = append(failures, message)
		}
		if err != nil {
			return
		}
	}

	// Check the size of the filesystem storage:
	filesystemStorage := asc.spec.FileSystemStorage.Resources.Requests.Storage()
	if filesystemStorage.Cmp(minFilesystemStorage) < 0 {
		message := fmt.Sprintf(
			"Filesystem storage %s is too small, it must be at least %s",
			filesystemStorage, &minFilesystemStorage,
		)
		warnings = append(warnings, message)
		key := client.ObjectKey{
			Namespace: asc.namespace,
			Name:      serviceName,
		}
		var tmp corev1.PersistentVolumeClaim
		err = asc.Client.Get(ctx, key, &tmp)
		if errors.IsNotFound(err) {
			failures = append(failures, message)
			err = nil
		}
		if err != nil {
			return
		}
	}

	// Check the size of the image storage:
	if asc.spec.ImageStorage != nil {
		imageStorage := asc.spec.ImageStorage.Resources.Requests.Storage()
		if imageStorage.Cmp(minImageStorage) < 0 {
			message := fmt.Sprintf(
				"Image storage %s is too small, it must be at least %s",
				imageStorage, &minImageStorage,
			)
			warnings = append(warnings, message)
			key := client.ObjectKey{
				Namespace: asc.namespace,
				Name:      imageServiceName,
			}
			var tmp appsv1.StatefulSet
			err = asc.Client.Get(ctx, key, &tmp)
			if errors.IsNotFound(err) {
				err = nil
				failures = append(failures, message)
			}
			if err != nil {
				return
			}
		}
	}

	return
}
