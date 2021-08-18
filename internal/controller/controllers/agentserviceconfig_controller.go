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
	"fmt"
	"net/url"
	"os"

	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
	agentServiceConfigName = "agent"

	serviceName              string = "assisted-service"
	databaseName             string = "postgres"
	databasePasswordLength   int    = 16
	servicePort              int32  = 8090
	databasePort             int32  = 5432
	agentLocalAuthSecretName        = serviceName + "local-auth" // #nosec

	defaultIngressCertCMName      string = "default-ingress-cert"
	defaultIngressCertCMNamespace string = "openshift-config-managed"

	configmapAnnotation = "unsupported.agent-install.openshift.io/assisted-service-configmap"

	assistedConfigHashAnnotation = "agent-install.openshift.io/config-hash"
	mirrorConfigHashAnnotation   = "agent-install.openshift.io/mirror-hash"
	userConfigHashAnnotation     = "agent-install.openshift.io/user-config-hash"
)

// AgentServiceConfigReconciler reconciles a AgentServiceConfig object
type AgentServiceConfigReconciler struct {
	client.Client
	Log      logrus.FieldLogger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Namespace the operator is running in
	Namespace string
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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

	// NOTE: ignoring the Namespace that seems to get set on request when syncing on namespaced objects
	// when our AgentServiceConfig is ClusterScoped.
	if err := r.Get(ctx, types.NamespacedName{Name: req.NamespacedName.Name}, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		log.Error(err, "Failed to get resource", req.NamespacedName)
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

	for _, f := range []func(context.Context, logrus.FieldLogger, *aiv1beta1.AgentServiceConfig) error{
		r.ensureFilesystemStorage,
		r.ensureDatabaseStorage,
		r.ensureAgentService,
		r.ensureServiceMonitor,
		r.ensureAgentRoute,
		r.ensureAgentLocalAuthSecret,
		r.ensurePostgresSecret,
		r.ensureIngressCertCM,
		r.ensureAssistedCM,
		r.ensureAssistedServiceDeployment,
	} {
		err := f(ctx, log, instance)
		if err != nil {
			log.Error(err, "Failed reconcile")
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				log.Error(err, "Failed to update status")
				return ctrl.Result{Requeue: true}, statusErr
			}
			return ctrl.Result{Requeue: true}, err
		}
	}

	msg := "AgentServiceConfig reconcile completed without error."
	conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionReconcileCompleted,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ReasonReconcileSucceeded,
		Message: msg,
	})
	return ctrl.Result{}, r.Status().Update(ctx, instance)
}

func (r *AgentServiceConfigReconciler) ensureServiceMonitor(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: r.Namespace}, service); err != nil {
		return err
	}

	sm, mutateFn := r.newServiceMonitor(instance, service)
	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, sm, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonStorageFailure,
			Message: "Failed to ensure Service Monitor: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("ServiceMonitor created")
	}

	return nil
}

func (r *AgentServiceConfigReconciler) ensureFilesystemStorage(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	pvc, mutateFn := r.newPVC(instance, serviceName, instance.Spec.FileSystemStorage)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonStorageFailure,
			Message: "Failed to ensure filesystem storage: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Filesystem storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureDatabaseStorage(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	pvc, mutateFn := r.newPVC(instance, databaseName, instance.Spec.DatabaseStorage)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonStorageFailure,
			Message: "Failed to ensure database storage: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Database storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	svc, mutateFn := r.newAgentService(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonAgentServiceFailure,
			Message: "Failed to ensure agent service: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Agent service created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentRoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	route, mutateFn := r.newAgentRoute(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonAgentRouteFailure,
			Message: "Failed to ensure agent route: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Agent route created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentLocalAuthSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	secret, mutateFn, err := r.newAgentLocalAuthSecret(instance)
	if err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonAgentLocalAuthSecretFailure,
			Message: "Failed to generate agent local auth secret: " + err.Error(),
		})
		return err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonAgentLocalAuthSecretFailure,
			Message: "Failed to ensure agent local auth secret: " + err.Error(),
		})
		return err
	} else {
		switch result {
		case controllerutil.OperationResultCreated:
			log.Info("Agent local auth secret created")
		case controllerutil.OperationResultUpdated:
			log.Info("Agent local auth secret updated")
		}
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensurePostgresSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	// TODO(djzager): using controllerutil.CreateOrUpdate is convenient but we may
	// want to consider simply creating the secret if we can't find instead of
	// generating a secret every reconcile.
	secret, mutateFn, err := r.newPostgresSecret(instance)
	if err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonPostgresSecretFailure,
			Message: "Failed to generate database secret: " + err.Error(),
		})
		return err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonPostgresSecretFailure,
			Message: "Failed to ensure database secret: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Database secret created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAssistedServiceDeployment(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	deployment, mutateFn, err := r.newAssistedServiceDeployment(ctx, log, instance)
	if err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to generate assisted service deployment: " + err.Error(),
		})
		return err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to ensure assisted service deployment: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Assisted service deployment created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureIngressCertCM(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	sourceCM := &corev1.ConfigMap{}

	if err := r.Get(ctx, types.NamespacedName{Name: defaultIngressCertCMName, Namespace: defaultIngressCertCMNamespace}, sourceCM); err != nil {
		log.Error(err, "Failed to get default ingress cert config map")
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to get default ingress cert config map: " + err.Error(),
		})
		return err
	}

	cm, mutateFn := r.newIngressCertCM(instance, sourceCM)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to ensure ingress cert config map: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Ingress config map created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) newAssistedCM(log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig, serviceURL *url.URL) (*corev1.ConfigMap, controllerutil.MutateFn) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
			return err
		}

		cm.Data = map[string]string{
			"SERVICE_BASE_URL": serviceURL.String(),

			// image overrides
			"AGENT_DOCKER_IMAGE":     AgentImage(),
			"CONTROLLER_IMAGE":       ControllerImage(),
			"INSTALLER_IMAGE":        InstallerImage(),
			"SELF_VERSION":           ServiceImage(),
			"OPENSHIFT_VERSIONS":     r.getOpenshiftVersions(log, instance),
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
			"ENABLE_SINGLE_NODE_DNSMASQ":  "True",
			"IPV6_SUPPORT":                "True",
			"JWKS_URL":                    "https://api.openshift.com/.well-known/jwks.json",
			"PUBLIC_CONTAINER_REGISTRIES": "quay.io,registry.svc.ci.openshift.org",
			"HW_VALIDATOR_REQUIREMENTS":   `[{"version":"default","master":{"cpu_cores":4,"ram_mib":16384,"disk_size_gb":120,"installation_disk_speed_threshold_ms":10,"network_latency_threshold_ms":100,"packet_loss_percentage":0},"worker":{"cpu_cores":2,"ram_mib":8192,"disk_size_gb":120,"installation_disk_speed_threshold_ms":10,"network_latency_threshold_ms":1000,"packet_loss_percentage":10},"sno":{"cpu_cores":8,"ram_mib":32768,"disk_size_gb":120,"installation_disk_speed_threshold_ms":10}}]`,

			"NAMESPACE":       r.Namespace,
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

	return cm, mutateFn
}

func copyEnv(config map[string]string, key string) {
	if value, ok := os.LookupEnv(key); ok {
		config[key] = value
	}
}

func (r *AgentServiceConfigReconciler) ensureAssistedCM(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) error {
	// must have the route in order to populate SERVICE_BASE_URL for the service
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: r.Namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("Route's host is empty")
		}
		log.Info("Failed to get route or route's host is empty")
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to get route for assisted service: " + err.Error(),
		})
		return err
	}

	serviceURL := &url.URL{Scheme: "https", Host: route.Spec.Host}
	cm, mutateFn := r.newAssistedCM(log, instance, serviceURL)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to ensure assisted settings config map: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Assisted settings config map created")
	}
	return nil
}

func checkIngressCMName(obj metav1.Object) bool {
	return obj.GetNamespace() == defaultIngressCertCMNamespace && obj.GetName() == defaultIngressCertCMName
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
		Owns(&corev1.Secret{}).
		Owns(&routev1.Route{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, ingressCMHandler, ingressCMPredicates).
		Complete(r)
}
