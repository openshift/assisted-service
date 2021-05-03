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
	"strconv"

	"github.com/go-logr/logr"
	"github.com/go-openapi/swag"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/assisted-service/internal/common"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/gencrypto"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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
)

// AgentServiceConfigReconciler reconciles a AgentServiceConfig object
type AgentServiceConfigReconciler struct {
	client.Client
	Log      logr.Logger
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
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *AgentServiceConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		r.Log.Error(err, "Failed to get resource", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// We only support one AgentServiceConfig per cluster, and it must be called "agent". This prevents installing
	// AgentService more than once in the cluster.
	if instance.Name != agentServiceConfigName {
		reason := fmt.Sprintf("Invalid name (%s)", instance.Name)
		msg := fmt.Sprintf("Only one AgentServiceConfig supported per cluster and must be named '%s'", agentServiceConfigName)
		r.Log.Info(fmt.Sprintf("%s: %s", reason, msg), req.NamespacedName)
		r.Recorder.Event(instance, "Warning", reason, msg)
		return reconcile.Result{}, nil
	}

	for _, f := range []func(context.Context, *aiv1beta1.AgentServiceConfig) error{
		r.ensureFilesystemStorage,
		r.ensureDatabaseStorage,
		r.ensureAgentService,
		r.ensureAgentRoute,
		r.ensureAgentLocalAuthSecret,
		r.ensurePostgresSecret,
		r.ensureIngressCertCM,
		r.ensureAssistedServiceDeployment,
	} {
		err := f(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed reconcile")
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				r.Log.Error(err, "Failed to update status")
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

func (r *AgentServiceConfigReconciler) ensureFilesystemStorage(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
		r.Log.Info("Filesystem storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureDatabaseStorage(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
		r.Log.Info("Database storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentService(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
		r.Log.Info("Agent service created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentRoute(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
		r.Log.Info("Agent route created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentLocalAuthSecret(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
			r.Log.Info("Agent local auth secret created")
		case controllerutil.OperationResultUpdated:
			r.Log.Info("Agent local auth secret updated")
		}
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensurePostgresSecret(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
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
		r.Log.Info("Database secret created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAssistedServiceDeployment(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
	// must have the route in order to populate SERVICE_BASE_URL for the service
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: r.Namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("Route's host is empty")
		}
		r.Log.Info("Failed to get route or route's host is empty")
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to get route for assisted service: " + err.Error(),
		})
		return err
	}

	if instance.Spec.MirrorRegistryRef != nil {
		err = r.validateMirrorRegistriesConfigMap(ctx, instance)
		if err != nil {
			conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ConditionReconcileCompleted,
				Status:  corev1.ConditionFalse,
				Reason:  aiv1beta1.ReasonDeploymentFailure,
				Message: err.Error(),
			})
			return err
		}
	}

	serviceURL := &url.URL{Scheme: "https", Host: route.Spec.Host}
	deployment, mutateFn := r.newAssistedServiceDeployment(instance, serviceURL)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, mutateFn); err != nil {
		conditionsv1.SetStatusConditionNoHeartbeat(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ReasonDeploymentFailure,
			Message: "Failed to ensure assisted service deployment: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Assisted service deployment created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureIngressCertCM(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
	sourceCM := &corev1.ConfigMap{}

	if err := r.Get(ctx, types.NamespacedName{Name: defaultIngressCertCMName, Namespace: defaultIngressCertCMNamespace}, sourceCM); err != nil {
		r.Log.Error(err, "Failed to get default ingress cert config map")
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
		r.Log.Info("Ingress config map created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) validateMirrorRegistriesConfigMap(ctx context.Context, instance *aiv1beta1.AgentServiceConfig) error {
	mirrorCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.MirrorRegistryRef.Name, Namespace: r.Namespace}, mirrorCM)
	if err != nil {
		r.Log.Info("Failed to get mirror registries ConfigMap")
		return err
	}
	keysToValidate := []string{mirrorRegistryRefCertKey, mirrorRegistryRefRegistryConfKey}
	for _, key := range keysToValidate {
		if _, ok := mirrorCM.Data[key]; !ok {
			r.Log.Info("mirror registries configmap %s does not contain key %s", instance.Spec.MirrorRegistryRef.Name, key)
			err = fmt.Errorf("%s key missing in the mirrror registries configmap %s", key, instance.Spec.MirrorRegistryRef.Name)
			return err
		}
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
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&routev1.Route{}).
		Owns(&appsv1.Deployment{}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, ingressCMHandler, ingressCMPredicates).
		Complete(r)
}

func (r *AgentServiceConfigReconciler) newPVC(instance *aiv1beta1.AgentServiceConfig, name string, spec corev1.PersistentVolumeClaimSpec) (*corev1.PersistentVolumeClaim, controllerutil.MutateFn) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace,
		},
		Spec: spec,
	}

	requests := map[corev1.ResourceName]resource.Quantity{}
	for key, value := range spec.Resources.Requests {
		requests[key] = value
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		pvc.Spec.Resources.Requests = requests
		return nil
	}

	return pvc, mutateFn
}

func (r *AgentServiceConfigReconciler) newAgentService(instance *aiv1beta1.AgentServiceConfig) (*corev1.Service, controllerutil.MutateFn) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.Namespace,
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
		svc.ObjectMeta.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = serviceName
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = serviceName
		svc.Spec.Ports[0].Port = servicePort
		// since intstr.FromInt() doesn't take an int32, just use what FromInt() would return
		svc.Spec.Ports[0].TargetPort = intstr.IntOrString{Type: intstr.Int, IntVal: servicePort}
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": serviceName}
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		return nil
	}

	return svc, mutateFn
}

func (r *AgentServiceConfigReconciler) newAgentRoute(instance *aiv1beta1.AgentServiceConfig) (*routev1.Route, controllerutil.MutateFn) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.Namespace,
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
		route.Spec = routeSpec
		return nil
	}

	return route, mutateFn
}

func (r *AgentServiceConfigReconciler) newAgentLocalAuthSecret(instance *aiv1beta1.AgentServiceConfig) (*corev1.Secret, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentLocalAuthSecretName,
			Namespace: r.Namespace,
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

func (r *AgentServiceConfigReconciler) newPostgresSecret(instance *aiv1beta1.AgentServiceConfig) (*corev1.Secret, controllerutil.MutateFn, error) {
	pass, err := generatePassword(databasePasswordLength)
	if err != nil {
		return nil, nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: r.Namespace,
		},
		StringData: map[string]string{
			"db.host":     "localhost",
			"db.user":     "admin",
			"db.password": pass,
			"db.name":     "installer",
			"db.port":     strconv.Itoa(int(databasePort)),
		},
		Type: corev1.SecretTypeOpaque,
	}

	// Only setting the owner reference to prevent clobbering the generated password.
	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
			return err
		}
		return nil
	}

	return secret, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newAssistedServiceDeployment(instance *aiv1beta1.AgentServiceConfig, serviceURL *url.URL) (*appsv1.Deployment, controllerutil.MutateFn) {

	// User is responsible for knowing to restart assisted-service
	var envFrom []corev1.EnvFromSource
	annotations := instance.ObjectMeta.GetAnnotations()
	configmapName, ok := annotations[configmapAnnotation]
	if ok {
		r.Log.Info("ConfigMap %v being used to configure assisted-service deployment", configmapName)
		envFrom = []corev1.EnvFromSource{
			{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configmapName,
					},
				},
			},
		}
	}

	serviceEnv := []corev1.EnvVar{
		{Name: "SERVICE_BASE_URL", Value: getEnvVar("SERVICE_BASE_URL", serviceURL.String())},

		// database
		newSecretEnvVar("DB_HOST", "db.host", databaseName),
		newSecretEnvVar("DB_NAME", "db.name", databaseName),
		newSecretEnvVar("DB_PASS", "db.password", databaseName),
		newSecretEnvVar("DB_PORT", "db.port", databaseName),
		newSecretEnvVar("DB_USER", "db.user", databaseName),

		// local auth secret
		newSecretEnvVar("EC_PUBLIC_KEY_PEM", "ec-public-key.pem", agentLocalAuthSecretName),
		newSecretEnvVar("EC_PRIVATE_KEY_PEM", "ec-private-key.pem", agentLocalAuthSecretName),

		// image overrides
		{Name: "AGENT_DOCKER_IMAGE", Value: AgentImage()},
		{Name: "CONTROLLER_IMAGE", Value: ControllerImage()},
		{Name: "INSTALLER_IMAGE", Value: InstallerImage()},
		{Name: "SELF_VERSION", Value: ServiceImage()},
		{Name: "OPENSHIFT_VERSIONS", Value: OpenshiftVersions()},

		{Name: "ISO_IMAGE_TYPE", Value: "minimal-iso"},
		{Name: "S3_USE_SSL", Value: "false"},
		{Name: "LOG_LEVEL", Value: "info"},
		{Name: "LOG_FORMAT", Value: "text"},
		{Name: "INSTALL_RH_CA", Value: "false"},
		{Name: "REGISTRY_CREDS", Value: ""},
		{Name: "DEPLOY_TARGET", Value: "k8s"},
		{Name: "STORAGE", Value: "filesystem"},
		{Name: "ISO_WORKSPACE_BASE_DIR", Value: "/data"},
		{Name: "ISO_CACHE_DIR", Value: "/data/cache"},

		// from configmap
		{Name: "AUTH_TYPE", Value: "local"},
		{Name: "BASE_DNS_DOMAINS", Value: ""},
		{Name: "CHECK_CLUSTER_VERSION", Value: "False"},
		{Name: "CREATE_S3_BUCKET", Value: "False"},
		{Name: "ENABLE_KUBE_API", Value: "True"},
		{Name: "ENABLE_SINGLE_NODE_DNSMASQ", Value: "True"},
		{Name: "IPV6_SUPPORT", Value: "True"},
		{Name: "JWKS_URL", Value: "https://api.openshift.com/.well-known/jwks.json"},
		{Name: "PUBLIC_CONTAINER_REGISTRIES", Value: "quay.io,registry.svc.ci.openshift.org"},
		{Name: "WITH_AMS_SUBSCRIPTIONS", Value: "False"},

		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},

		// enable https
		{Name: "SERVE_HTTPS", Value: "True"},
		{Name: "HTTPS_CERT_FILE", Value: "/etc/assisted-tls-config/tls.crt"},
		{Name: "HTTPS_KEY_FILE", Value: "/etc/assisted-tls-config/tls.key"},
		{Name: "SERVICE_CA_CERT_PATH", Value: "/etc/assisted-ingress-cert/ca-bundle.crt"},
		{Name: "SKIP_CERT_VERIFICATION", Value: "False"},
	}

	serviceContainer := corev1.Container{
		Name:  serviceName,
		Image: ServiceImage(),
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: servicePort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		EnvFrom: envFrom,
		Env:     serviceEnv,
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
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt(int(servicePort)),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/ready",
					Port:   intstr.FromInt(int(servicePort)),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	postgresContainer := corev1.Container{
		Name:            databaseName,
		Image:           DatabaseImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				Name:          databaseName,
				ContainerPort: databasePort,
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

	if instance.Spec.MirrorRegistryRef != nil {
		mirrorVolumeMounts := []corev1.VolumeMount{
			{Name: mirrorRegistryCertVolume, MountPath: common.MirrorRegistriesCertificatePath, SubPath: common.MirrorRegistriesCertificateFile},
			{Name: mirrorRegistryConfVolume, MountPath: common.MirrorRegistriesConfigDir},
		}
		mirrorVolumes := []corev1.Volume{
			{
				Name: mirrorRegistryCertVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: *instance.Spec.MirrorRegistryRef,
						DefaultMode:          swag.Int32(420),
						Optional:             swag.Bool(true),
						Items: []corev1.KeyToPath{
							{
								Key:  mirrorRegistryRefCertKey,
								Path: common.MirrorRegistriesCertificateFile,
							},
						},
					},
				},
			},
			{
				Name: mirrorRegistryConfVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: *instance.Spec.MirrorRegistryRef,
						DefaultMode:          swag.Int32(420),
						Optional:             swag.Bool(true),
						Items: []corev1.KeyToPath{
							{
								Key:  mirrorRegistryRefRegistryConfKey,
								Path: common.MirrorRegistriesConfigFile,
							},
						},
					},
				},
			},
		}
		serviceContainer.VolumeMounts = append(serviceContainer.VolumeMounts, mirrorVolumeMounts...)
		volumes = append(volumes, mirrorVolumes...)
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
			Namespace: r.Namespace,
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
		deployment.Spec.Template.Spec.Containers = []corev1.Container{serviceContainer, postgresContainer}
		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccountName

		return nil
	}
	return deployment, mutateFn
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

func (r *AgentServiceConfigReconciler) newIngressCertCM(instance *aiv1beta1.AgentServiceConfig, sourceCM *corev1.ConfigMap) (*corev1.ConfigMap, controllerutil.MutateFn) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultIngressCertCMName,
			Namespace: r.Namespace,
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

	return cm, mutateFn
}
