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

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	pkgerror "github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
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
	agentServiceConfigName            = "agent"
	serviceName                string = "assisted-service"
	imageServiceName           string = "assisted-image-service"
	webhookServiceName         string = "agentinstalladmission"
	databaseName               string = "postgres"
	kubeconfigSecretVolumeName string = "kubeconfig"
	kubeconfigSecretVolumePath string = "/etc/kube"
	kubeconfigPath             string = "/etc/kube/kubeconfig"
	kubeconfigKeyInSecret      string = "kubeconfig"

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
)

// AgentServiceConfigReconciler reconciles a AgentServiceConfig object
type AgentServiceConfigReconciler struct {
	client.Client
	Log      logrus.FieldLogger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// selector and tolerations the Operator runs in and propagates to its deployments
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration

	K8sApiExtensionsClientFactory k8sclient.K8sApiExtensionsClientFactory
}

type component struct {
	name   string
	reason string
	fn     NewComponentFn
}

type NewComponentFn func(context.Context, logrus.FieldLogger, *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error)
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
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="apiregistration.k8s.io",resources=apiservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;create

func (r *AgentServiceConfigReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
	ascKey := types.NamespacedName{Name: req.NamespacedName.Name}

	//for backwards compatability, if the namespace is assisted-installer we are looking
	//for a cluster scoped CRD. Otherwise, we will look for a namespace-scoped resource
	//and implicitly assume that we are in hypershift mode (L0-L1)
	if req.NamespacedName.Namespace != "" {
		ascKey.Namespace = req.NamespacedName.Namespace
	} else {
		log.Warning("DEBUG ==> ASC is not namespace scopped")
	}

	if err := r.Get(ctx, ascKey, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.

			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		log.WithError(err).Error("Failed to get resource", req.NamespacedName)
		return ctrl.Result{}, err
	}

	log.Info("DEBUG ==> ASC instance is", instance.Name, instance.Namespace)

	if instance.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(instance, agentServiceConfigFinalizerName) {
			controllerutil.AddFinalizer(instance, agentServiceConfigFinalizerName)
			if err := r.Update(ctx, instance); err != nil {
				log.WithError(err).Error("failed to add finalizer to AgentServiceConfig")
				return ctrl.Result{Requeue: true}, err
			}
		}
	} else {
		// do cleanup and remove finalizer
		statefulSet := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imageServiceName,
				Namespace: req.NamespacedName.Namespace,
			},
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(statefulSet), statefulSet); err != nil && !errors.IsNotFound(err) {
			log.WithError(err).Error("failed to get image service stateful set for cleanup")
			return ctrl.Result{Requeue: true}, err
		}
		if err := r.cleanupImageServiceFinalizer(ctx, statefulSet); err != nil {
			log.WithError(err).Error("failed to cleanup image service stateful set")
			return ctrl.Result{Requeue: true}, err
		}

		controllerutil.RemoveFinalizer(instance, agentServiceConfigFinalizerName)
		if err := r.Update(ctx, instance); err != nil {
			log.WithError(err).Error("failed to remove finalizer from AgentServiceConfig")
			return ctrl.Result{Requeue: true}, err
		}

		return ctrl.Result{}, nil
	}

	for _, component := range []component{
		{"FilesystemStorage", aiv1beta1.ReasonStorageFailure, r.newFilesystemPVC},
		{"DatabaseStorage", aiv1beta1.ReasonStorageFailure, r.newDatabasePVC},
		{"ImageServiceService", aiv1beta1.ReasonImageHandlerServiceFailure, r.newImageServiceService},
		{"AgentService", aiv1beta1.ReasonAgentServiceFailure, r.newAgentService},
		{"ServiceMonitor", aiv1beta1.ReasonAgentServiceMonitorFailure, r.newServiceMonitor},
		{"ImageServiceRoute", aiv1beta1.ReasonImageHandlerRouteFailure, r.newImageServiceRoute},
		{"AgentRoute", aiv1beta1.ReasonAgentRouteFailure, r.newAgentRoute},
		{"AgentLocalAuthSecret", aiv1beta1.ReasonAgentLocalAuthSecretFailure, r.newAgentLocalAuthSecret},
		{"DatabaseSecret", aiv1beta1.ReasonPostgresSecretFailure, r.newPostgresSecret},
		{"ImageServiceServiceAccount", aiv1beta1.ReasonImageHandlerServiceAccountFailure, r.newImageServiceServiceAccount},
		{"IngressCertConfigMap", aiv1beta1.ReasonIngressCertFailure, r.newIngressCertCM},
		{"ImageServiceConfigMap", aiv1beta1.ReasonConfigFailure, r.newImageServiceConfigMap},
		{"AssistedServiceConfigMap", aiv1beta1.ReasonConfigFailure, r.newAssistedCM},
		{"AssistedServiceDeployment", aiv1beta1.ReasonDeploymentFailure, r.newAssistedServiceDeployment},
		{"AgentClusterInstallValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, r.newACIWebHook},
		{"AgentClusterInstallMutatingWebHook", aiv1beta1.ReasonMutatingWebHookFailure, r.newACIMutatWebHook},
		{"InfraEnvValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, r.newInfraEnvWebHook},
		{"AgentValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, r.newAgentWebHook},
		{"WebHookService", aiv1beta1.ReasonWebHookServiceFailure, r.newWebHookService},
		{"WebHookServiceDeployment", aiv1beta1.ReasonWebHookDeploymentFailure, r.newWebHookDeployment},
		{"WebHookServiceAccount", aiv1beta1.ReasonWebHookServiceAccountFailure, r.newWebHookServiceAccount},
		{"WebHookClusterRole", aiv1beta1.ReasonWebHookClusterRoleFailure, r.newWebHookClusterRole},
		{"WebHookClusterRoleBinding", aiv1beta1.ReasonWebHookClusterRoleBindingFailure, r.newWebHookClusterRoleBinding},
		{"WebHookAPIService", aiv1beta1.ReasonWebHookAPIServiceFailure, r.newWebHookAPIService},
	} {
		if result, err := r.reconcileComponent(ctx, instance, log, component); err != nil {
			return result, err
		}
	}

	// Additional routes need to be synced if HTTP iPXE routes are exposed
	if r.exposeIPXEHTTPRoute(instance) {
		for _, component := range []component{
			{"ImageServiceIPXERoute", aiv1beta1.ReasonImageHandlerRouteFailure, r.newImageServiceIPXERoute},
			{"AgentIPXERoute", aiv1beta1.ReasonAgentRouteFailure, r.newAgentIPXERoute},
		} {
			if result, err := r.reconcileComponent(ctx, instance, log, component); err != nil {
				return result, err
			}
		}
	} else {
		// Ensure HTTP routes are removed
		for _, service := range []string{serviceName, imageServiceName} {
			if err := r.removeHTTPIPXERoute(ctx, instance, service); err != nil {
				log.WithError(err).Errorf("failed to remove HTTP route for %s", service)
				return ctrl.Result{Requeue: true}, err
			}
		}
	}

	if err := r.reconcileImageServiceStatefulSet(ctx, log, instance); err != nil {
		msg := "Failed to reconcile image-service StatefulSet"
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonImageHandlerStatefulSetFailure,
			Message: msg,
		})
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			log.WithError(statusErr).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, statusErr
		}
	}

	msg := "AgentServiceConfig reconcile completed without error."
	conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionReconcileCompleted,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ReasonReconcileSucceeded,
		Message: msg,
	})

	if err := r.monitorOperands(ctx, log, instance); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionDeploymentsHealthy,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			log.WithError(updateErr).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, updateErr
		}
		return ctrl.Result{Requeue: true}, err
	}

	conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionDeploymentsHealthy,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ReasonDeploymentSucceeded,
		Message: "All the deployments managed by Infrastructure-operator are healthy.",
	})

	return ctrl.Result{}, r.Status().Update(ctx, instance)
}

func (r *AgentServiceConfigReconciler) reconcileComponent(ctx context.Context, instance *aiv1beta1.AgentServiceConfig, log *logrus.Entry, component component) (ctrl.Result, error) {
	obj, mutateFn, err := component.fn(ctx, log, instance)
	if err != nil {
		msg := "Failed to generate definition for " + component.name
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  component.reason,
			Message: msg,
		})
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			log.WithError(err).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, statusErr
		}
		return ctrl.Result{Requeue: true}, err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, mutateFn); err != nil {
		msg := "Failed to ensure " + component.name
		log.WithError(err).Error(msg)
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  component.reason,
			Message: msg,
		})
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			log.WithError(err).Error("Failed to update status")
			return ctrl.Result{Requeue: true}, statusErr
		}
	} else if result != controllerutil.OperationResultNone {
		log.Info(component.name + " created")
	}
	return ctrl.Result{Requeue: false}, nil
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

func (r *AgentServiceConfigReconciler) monitorOperands(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
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
		if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: instance.Namespace}, deployment); err != nil {
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
	if err := r.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: instance.Namespace}, ss); err != nil {
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

func (r *AgentServiceConfigReconciler) newFilesystemPVC(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
		},
		Spec: instance.Spec.FileSystemStorage,
	}

	requests := getStorageRequests(&instance.Spec.FileSystemStorage)

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		pvc.Spec.Resources.Requests = requests
		return nil
	}

	return pvc, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newDatabasePVC(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: instance.Namespace,
		},
		Spec: instance.Spec.DatabaseStorage,
	}

	requests := getStorageRequests(&instance.Spec.DatabaseStorage)

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		pvc.Spec.Resources.Requests = requests
		return nil
	}

	return pvc, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newAgentService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		addAppLabel(serviceName, &svc.ObjectMeta)
		if svc.ObjectMeta.Annotations == nil {
			svc.ObjectMeta.Annotations = make(map[string]string)
		}
		svc.ObjectMeta.Annotations[servingCertAnnotation] = serviceName
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

func (r *AgentServiceConfigReconciler) newImageServiceService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		addAppLabel(serviceName, &svc.ObjectMeta)
		if svc.ObjectMeta.Annotations == nil {
			svc.ObjectMeta.Annotations = make(map[string]string)
		}
		svc.ObjectMeta.Annotations[servingCertAnnotation] = imageServiceName
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

func (r *AgentServiceConfigReconciler) newServiceMonitor(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: instance.Namespace}, service); err != nil {
		return nil, nil, err
	}

	endpoints := make([]monitoringv1.Endpoint, len(service.Spec.Ports))
	for i := range service.Spec.Ports {
		endpoints[i].Port = service.Spec.Ports[i].Name
	}

	labels := make(map[string]string)
	for k, v := range service.ObjectMeta.Labels {
		labels[k] = v
	}

	smSpec := monitoringv1.ServiceMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: labels,
		},
		Endpoints: endpoints,
	}

	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ObjectMeta.Name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: smSpec,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, sm, r.Scheme); err != nil {
			return err
		}

		sm.Spec = smSpec
		sm.ObjectMeta.Labels = labels
		return nil
	}

	return sm, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newAgentRoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) newHTTPRoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig, serviceToExpose string) (client.Object, controllerutil.MutateFn, error) {
	// In order to create plain http route we need https route to be created first to copy its host
	httpsRoute := &routev1.Route{}
	if err := r.Get(ctx, types.NamespacedName{Name: serviceToExpose, Namespace: instance.Namespace}, httpsRoute); err != nil {
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
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) removeHTTPIPXERoute(ctx context.Context, instance *aiv1beta1.AgentServiceConfig, serviceToExpose string) error {
	route := &routev1.Route{}
	routeName := fmt.Sprintf("%s-ipxe", serviceToExpose)
	namespacedName := types.NamespacedName{Name: routeName, Namespace: instance.Namespace}
	if err := r.Get(ctx, namespacedName, route); err == nil {
		err = r.Client.Delete(ctx, &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      routeName,
				Namespace: instance.Namespace,
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

func (r *AgentServiceConfigReconciler) newAgentIPXERoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	return r.newHTTPRoute(ctx, log, instance, serviceName)
}

func (r *AgentServiceConfigReconciler) newImageServiceRoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) newImageServiceIPXERoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	return r.newHTTPRoute(ctx, log, instance, imageServiceName)
}

func (r *AgentServiceConfigReconciler) newAgentLocalAuthSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentLocalAuthSecretName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				BackupLabel: BackupLabelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) newPostgresSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				BackupLabel: BackupLabelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	mutateFn := func() error {
		err := controllerutil.SetControllerReference(instance, secret, r.Scheme)
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

func (r *AgentServiceConfigReconciler) newImageServiceServiceAccount(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		err := controllerutil.SetControllerReference(instance, sa, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	}

	return sa, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newIngressCertCM(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	sourceCM := &corev1.ConfigMap{}

	if err := r.Get(ctx, types.NamespacedName{Name: defaultIngressCertCMName, Namespace: defaultIngressCertCMNamespace}, sourceCM); err != nil {
		log.WithError(err).Error("Failed to get default ingress cert config map")
		return nil, nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultIngressCertCMName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) newImageServiceConfigMap(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
			return err
		}

		if cm.ObjectMeta.Annotations == nil {
			cm.ObjectMeta.Annotations = make(map[string]string)
		}
		cm.ObjectMeta.Annotations[injectCABundleAnnotation] = "true"
		return nil
	}

	return cm, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) urlForRoute(ctx context.Context, routeName string, instance *aiv1beta1.AgentServiceConfig) (string, error) {
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: routeName, Namespace: instance.Namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("%s route host is empty", routeName)
		}
		return "", err
	}

	u := &url.URL{Scheme: "https", Host: route.Spec.Host}
	return u.String(), nil
}

//go:embed default_controller_hw_requirements.json
var defaultControllerHardwareRequirements string

func (r *AgentServiceConfigReconciler) newAssistedCM(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	serviceURL, err := r.urlForRoute(ctx, serviceName, instance)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", serviceName)
		return nil, nil, err
	}

	imageServiceURL, err := r.urlForRoute(ctx, imageServiceName, instance)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", imageServiceName)
		return nil, nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
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
			"OS_IMAGES":              r.getOSImages(log, instance),
			"MUST_GATHER_IMAGES":     r.getMustGatherImages(log, instance),
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
			"PUBLIC_CONTAINER_REGISTRIES": "quay.io,registry.svc.ci.openshift.org",
			"HW_VALIDATOR_REQUIREMENTS":   defaultControllerHardwareRequirements,

			"NAMESPACE":       instance.Namespace,
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

func (r *AgentServiceConfigReconciler) newImageServiceStatefulSet(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (*appsv1.StatefulSet, controllerutil.MutateFn) {
	skipVerifyTLS, ok := instance.ObjectMeta.GetAnnotations()[imageServiceSkipVerifyTLSAnnotation]
	if !ok {
		skipVerifyTLS = "false"
	}

	deploymentLabels := map[string]string{
		"app": imageServiceName,
	}

	imageServiceBaseURL := r.getImageService(ctx, log, instance)

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
			{Name: "RHCOS_VERSIONS", Value: r.getOSImages(log, instance)},
			{Name: "HTTPS_CERT_FILE", Value: "/etc/image-service/certs/tls.crt"},
			{Name: "HTTPS_KEY_FILE", Value: "/etc/image-service/certs/tls.key"},
			{Name: "HTTPS_CA_FILE", Value: "/etc/image-service/ca-bundle/service-ca.crt"},
			{Name: "ASSISTED_SERVICE_SCHEME", Value: "https"},
			{Name: "ASSISTED_SERVICE_HOST", Value: serviceName + "." + instance.Namespace + ".svc:" + servicePort.String()},
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
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, statefulSet, r.Scheme); err != nil {
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

		if instance.Spec.ImageStorage != nil {
			var found bool
			for i, claim := range statefulSet.Spec.VolumeClaimTemplates {
				if claim.ObjectMeta.Name == "image-service-data" {
					found = true
					statefulSet.Spec.VolumeClaimTemplates[i].Spec.Resources.Requests = getStorageRequests(instance.Spec.ImageStorage)
				}
			}
			if !found {
				statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "image-service-data",
						},
						Spec: *instance.Spec.ImageStorage,
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

		if r.NodeSelector != nil {
			statefulSet.Spec.Template.Spec.NodeSelector = r.NodeSelector
		} else {
			statefulSet.Spec.Template.Spec.NodeSelector = map[string]string{}
		}

		if r.Tolerations != nil {
			statefulSet.Spec.Template.Spec.Tolerations = r.Tolerations
		} else {
			statefulSet.Spec.Template.Spec.Tolerations = []corev1.Toleration{}
		}
		return nil
	}

	return statefulSet, mutateFn
}

func (r *AgentServiceConfigReconciler) cleanupImageServiceFinalizer(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	if !controllerutil.ContainsFinalizer(statefulSet, imageServiceStatefulSetFinalizerName) {
		return nil
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList, client.MatchingLabels{"app": imageServiceName}, client.InNamespace(statefulSet.Namespace)); err != nil {
		return err
	}

	for i := range pvcList.Items {
		if err := r.Client.Delete(ctx, &pvcList.Items[i]); err != nil {
			return err
		}
	}

	controllerutil.RemoveFinalizer(statefulSet, imageServiceStatefulSetFinalizerName)
	if err := r.Update(ctx, statefulSet); err != nil {
		return err
	}

	return nil
}

func (r *AgentServiceConfigReconciler) reconcileImageServiceStatefulSet(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	var err error
	defer func() {
		// delete old deployment if it exists and we've created the new stateful set correctly
		// TODO: this can be removed when we no longer support upgrading from a release that used deployments for the image service
		// NOTE: this relies on the err local variable being set correctly when the function returns
		if err == nil {
			_ = r.Client.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      imageServiceName,
					Namespace: instance.Namespace,
				},
			})
		}
	}()

	statefulSet, mutateFn := r.newImageServiceStatefulSet(ctx, log, instance)

	key := client.ObjectKeyFromObject(statefulSet)
	if err = r.Client.Get(ctx, key, statefulSet); err != nil {
		// if the statefulset doesn't exist, create it
		if !errors.IsNotFound(err) {
			return err
		}
		log.Info("Creating image service stateful set")
		if err = mutateFn(); err != nil {
			return err
		}
		if err = r.Client.Create(ctx, statefulSet); err != nil {
			return err
		}
		return nil
	}

	if !statefulSet.DeletionTimestamp.IsZero() {
		if err = r.cleanupImageServiceFinalizer(ctx, statefulSet); err != nil {
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
		if err = r.Client.Update(ctx, statefulSet); err != nil {
			return err
		}
		return nil
	}

	log.Info("Deleting image service stateful set on volume claim template update")

	// need to delete and re-create statefulset because the volumes have changed
	if err = r.Client.Delete(ctx, existing); err != nil {
		return err
	}

	return nil
}

func (r *AgentServiceConfigReconciler) newAssistedServiceDeployment(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	var assistedConfigHash, mirrorConfigHash, userConfigHash string
	var err error
	var secret *corev1.Secret

	// Get hash of generated assisted config
	assistedConfigHash, err = r.getCMHash(ctx, types.NamespacedName{Name: serviceName, Namespace: instance.Namespace})
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

	if instance.Spec.KubeconfigSecretRef != nil {
		// kubeconfig of an external control plane
		envSecrets = append(envSecrets, corev1.EnvVar{Name: "KUBECONFIG", Value: kubeconfigPath})

		// Validate kubeconfig secret reference specified in ASC
		secret, err = r.validateKubeconfigSecretRef(ctx, instance)
		if err != nil {
			return nil, nil, err
		}

		// Deploy agent-install CRDs on the external cluster (using specified kubeconfig in ASC)
		if err = r.deployAgentInstallCRDs(secret, instance.Namespace, log); err != nil {
			return nil, nil, err
		}
	}

	if r.exposeIPXEHTTPRoute(instance) {
		envSecrets = append(envSecrets, corev1.EnvVar{Name: "HTTP_LISTEN_PORT", Value: serviceHTTPPort.String()})
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "bucket-filesystem", MountPath: "/data"},
		{Name: "tls-certs", MountPath: "/etc/assisted-tls-config"},
		{Name: "ingress-cert", MountPath: "/etc/assisted-ingress-cert"},
	}
	if instance.Spec.KubeconfigSecretRef != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: kubeconfigSecretVolumeName, MountPath: kubeconfigSecretVolumePath})
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
	userConfigName, ok := instance.ObjectMeta.GetAnnotations()[configmapAnnotation]
	if ok {
		log.Infof("ConfigMap %s from namespace %s being used to configure assisted-service deployment", userConfigName, instance.Namespace)
		userConfigHash, err = r.getCMHash(ctx, types.NamespacedName{Name: userConfigName, Namespace: instance.Namespace})
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
		EnvFrom:      envFrom,
		Env:          envSecrets,
		VolumeMounts: volumeMounts,
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

	if instance.Spec.KubeconfigSecretRef != nil {
		// kubeconfig of an external control plane
		volumes = append(volumes,
			corev1.Volume{
				Name: kubeconfigSecretVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: instance.Spec.KubeconfigSecretRef.Name,
					},
				},
			},
		)
	}

	if instance.Spec.MirrorRegistryRef != nil {
		cm := &corev1.ConfigMap{}
		namespacedName := types.NamespacedName{Name: instance.Spec.MirrorRegistryRef.Name, Namespace: instance.Namespace}
		err := r.Get(ctx, namespacedName, cm)
		if err != nil {
			return nil, nil, err
		}

		mirrorConfigHash, err = checksumMap(cm.Data)
		if err != nil {
			return nil, nil, err
		}

		// require that the registries config key be specified before continuing
		if _, ok := cm.Data[mirrorRegistryRefRegistryConfKey]; !ok {
			err = fmt.Errorf("Mirror registry configmap %s missing key %s", instance.Spec.MirrorRegistryRef.Name, mirrorRegistryRefRegistryConfKey)
			return nil, nil, err
		}

		// make sure configmap is being backed up
		if err := ensureConfigMapIsLabelled(ctx, r.Client, cm, namespacedName); err != nil {
			return nil, nil, pkgerror.Wrapf(err, "Unable to mark mirror configmap for backup")
		}

		volume := corev1.Volume{
			Name: mirrorRegistryConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: *instance.Spec.MirrorRegistryRef,
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
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
			return err
		}
		var replicas int32 = 1
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Strategy = deploymentStrategy

		// Handle our hashed configMap(s)
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations[assistedConfigHashAnnotation] = assistedConfigHash
		deployment.Spec.Template.Annotations[mirrorConfigHashAnnotation] = mirrorConfigHash
		deployment.Spec.Template.Annotations[userConfigHashAnnotation] = userConfigHash

		deployment.Spec.Template.Spec.Containers = []corev1.Container{serviceContainer, postgresContainer}
		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccountName

		if r.NodeSelector != nil {
			deployment.Spec.Template.Spec.NodeSelector = r.NodeSelector
		} else {
			deployment.Spec.Template.Spec.NodeSelector = map[string]string{}
		}

		if r.Tolerations != nil {
			deployment.Spec.Template.Spec.Tolerations = r.Tolerations
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
func (r *AgentServiceConfigReconciler) getMustGatherImages(log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) string {
	if instance.Spec.MustGatherImages == nil {
		return MustGatherImages()
	}
	mustGatherVersions := make(versions.MustGatherVersions)
	for _, specImage := range instance.Spec.MustGatherImages {
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
func (r *AgentServiceConfigReconciler) getOSImages(log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) string {
	if instance.Spec.OSImages == nil {
		return OSImages()
	}

	osImages := make(models.OsImages, 0)
	for i := range instance.Spec.OSImages {
		osImage := models.OsImage{
			OpenshiftVersion: &instance.Spec.OSImages[i].OpenshiftVersion,
			URL:              &instance.Spec.OSImages[i].Url,
			Version:          &instance.Spec.OSImages[i].Version,
			CPUArchitecture:  &instance.Spec.OSImages[i].CPUArchitecture,
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
func (r *AgentServiceConfigReconciler) exposeIPXEHTTPRoute(instance *aiv1beta1.AgentServiceConfig) bool {
	switch instance.Spec.IPXEHTTPRoute {
	case aiv1beta1.IPXEHTTPRouteEnabled:
		return true
	case aiv1beta1.IPXEHTTPRouteDisabled:
		return false
	default:
		return false
	}
}

func (r *AgentServiceConfigReconciler) getCMHash(ctx context.Context, namespacedName types.NamespacedName) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, namespacedName, cm); err != nil {
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

func (r *AgentServiceConfigReconciler) validateKubeconfigSecretRef(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) (*corev1.Secret, error) {
	secretRef := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.KubeconfigSecretRef.Name}
	secret, err := getSecret(ctx, r.Client, r, secretRef)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to get '%s' secret in '%s' namespace", secretRef.Name, secretRef.Namespace)
	}
	_, ok := secret.Data[kubeconfigKeyInSecret]
	if !ok {
		return nil, pkgerror.Errorf("Secret '%s' does not contain '%s' key value", secretRef.Name, kubeconfigKeyInSecret)
	}
	return secret, nil
}

func (r *AgentServiceConfigReconciler) newInfraEnvWebHook(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newAgentWebHook(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newACIMutatWebHook(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newACIWebHook(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newWebHookServiceAccount(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	sa := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentinstalladmission",
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		return nil
	}
	return &sa, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newWebHookClusterRoleBinding(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	roleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     "system:openshift:assisted-installer:agentinstalladmission",
	}
	subjects := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Namespace: instance.Namespace,
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

func (r *AgentServiceConfigReconciler) newWebHookClusterRole(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newWebHookService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		addAppLabel(webhookServiceName, &svc.ObjectMeta)
		if svc.ObjectMeta.Annotations == nil {
			svc.ObjectMeta.Annotations = make(map[string]string)
		}
		svc.ObjectMeta.Annotations[servingCertAnnotation] = webhookServiceName
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

func (r *AgentServiceConfigReconciler) newWebHookAPIService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	as := &apiregv1.APIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "v1.admission.agentinstall.openshift.io",
			Namespace: instance.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, as, r.Scheme); err != nil {
			return err
		}

		if as.ObjectMeta.Annotations == nil {
			as.ObjectMeta.Annotations = make(map[string]string)
		}
		as.ObjectMeta.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
		as.Spec.Group = "admission.agentinstall.openshift.io"
		as.Spec.GroupPriorityMinimum = 1000
		as.Spec.VersionPriority = 15
		as.Spec.Version = "v1"
		as.Spec.Service = &apiregv1.ServiceReference{
			Name:      "agentinstalladmission",
			Namespace: instance.Namespace,
		}
		return nil
	}
	return as, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newWebHookDeployment(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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
			Namespace: instance.Namespace,
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
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
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

func (r *AgentServiceConfigReconciler) getImageService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) string {
	imageServiceURL, err := r.urlForRoute(ctx, imageServiceName, instance)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", imageServiceName)
		return ""
	}
	return imageServiceURL
}

func (r *AgentServiceConfigReconciler) deployAgentInstallCRDs(secret *corev1.Secret, namespace string, log logrus.FieldLogger) error {
	// Get in-cluster client
	kubeClient, err := r.K8sApiExtensionsClientFactory.CreateFromInClusterConfig()
	if err != nil {
		return err
	}

	// Fetch all agent-install CRDs in cluster
	crdClient := kubeClient.ApiextensionsV1().CustomResourceDefinitions()
	crds, err := crdClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("operators.coreos.com/assisted-service-operator.%s=", namespace),
	})
	if err != nil || len(crds.Items) == 0 {
		log.Error(err)
		return pkgerror.New("agent-install CRDs are not available")
	}

	// Get external cluster client (using kubeconfig specified in ASC)
	kubeClient, err = r.K8sApiExtensionsClientFactory.CreateFromSecret(secret)
	if err != nil {
		return err
	}

	// Create the CRDs on the external cluster
	crdClient = kubeClient.ApiextensionsV1().CustomResourceDefinitions()
	for _, crd := range crds.Items {
		// ResourceVersion should not be set on objects to be created
		crd.ResourceVersion = ""
		c := crd
		_, err1 := crdClient.Create(context.TODO(), &c, metav1.CreateOptions{})
		if err1 != nil {
			log.Debug(pkgerror.Wrapf(err1, "Ignore '%s' CRD creation failure - probably already exists", crd.Name))
			continue
		}
		log.Info(fmt.Sprintf("Created agent-install CRD on external cluster: '%s'", crd.Name))
	}

	return nil
}
