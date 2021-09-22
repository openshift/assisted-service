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
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
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
	agentServiceConfigName        = "agent"
	serviceName            string = "assisted-service"
	imageServiceName       string = "assisted-image-service"
	databaseName           string = "postgres"

	databasePasswordLength   int = 16
	agentLocalAuthSecretName     = serviceName + "local-auth" // #nosec

	defaultIngressCertCMName      string = "default-ingress-cert"
	defaultIngressCertCMNamespace string = "openshift-config-managed"

	configmapAnnotation = "unsupported.agent-install.openshift.io/assisted-service-configmap"

	assistedConfigHashAnnotation = "agent-install.openshift.io/config-hash"
	mirrorConfigHashAnnotation   = "agent-install.openshift.io/mirror-hash"
	userConfigHashAnnotation     = "agent-install.openshift.io/user-config-hash"

	servingCertAnnotation    = "service.beta.openshift.io/serving-cert-secret-name"
	injectCABundleAnnotation = "service.beta.openshift.io/inject-cabundle"
)

var (
	servicePort      = intstr.Parse("8090")
	databasePort     = intstr.Parse("5432")
	imageHandlerPort = intstr.Parse("8080")
)

// AgentServiceConfigReconciler reconciles a AgentServiceConfig object
type AgentServiceConfigReconciler struct {
	client.Client
	Log      logrus.FieldLogger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Namespace the operator is running in
	Namespace string

	// selector and tolerations the Operator runs in and propagates to its deployments
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
}

type NewComponentFn func(context.Context, logrus.FieldLogger, *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error)

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
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

	for _, component := range []struct {
		name   string
		reason string
		fn     NewComponentFn
	}{
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
		{"ImageServiceDeployment", aiv1beta1.ReasonImageHandlerDeploymentFailure, r.newImageServiceDeployment},
		{"AssistedServiceDeployment", aiv1beta1.ReasonDeploymentFailure, r.newAssistedServiceDeployment},
	} {
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
		Owns(&corev1.ConfigMap{}).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, ingressCMHandler, ingressCMPredicates).
		Complete(r)
}

func (r *AgentServiceConfigReconciler) newFilesystemPVC(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.Namespace,
		},
		Spec: instance.Spec.FileSystemStorage,
	}

	requests := map[corev1.ResourceName]resource.Quantity{}
	for key, value := range instance.Spec.FileSystemStorage.Resources.Requests {
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

	return pvc, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newDatabasePVC(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: r.Namespace,
		},
		Spec: instance.Spec.DatabaseStorage,
	}

	requests := map[corev1.ResourceName]resource.Quantity{}
	for key, value := range instance.Spec.DatabaseStorage.Resources.Requests {
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

	return pvc, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newAgentService(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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
		svc.ObjectMeta.Annotations[servingCertAnnotation] = serviceName
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = serviceName
		svc.Spec.Ports[0].Port = int32(servicePort.IntValue())
		svc.Spec.Ports[0].TargetPort = servicePort
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
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
		svc.ObjectMeta.Annotations[servingCertAnnotation] = imageServiceName
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = imageServiceName
		svc.Spec.Ports[0].Port = int32(imageHandlerPort.IntValue())
		svc.Spec.Ports[0].TargetPort = imageHandlerPort
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": imageServiceName}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return nil
	}

	return svc, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newServiceMonitor(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: r.Namespace}, service); err != nil {
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
			Namespace: r.Namespace,
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

func (r *AgentServiceConfigReconciler) newImageServiceRoute(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: r.Namespace,
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

func (r *AgentServiceConfigReconciler) newAgentLocalAuthSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
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

func (r *AgentServiceConfigReconciler) newPostgresSecret(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseName,
			Namespace: r.Namespace,
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
			Namespace: r.Namespace,
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

	return cm, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newImageServiceConfigMap(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: r.Namespace,
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

func (r *AgentServiceConfigReconciler) urlForRoute(ctx context.Context, routeName string) (string, error) {
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: routeName, Namespace: r.Namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("%s route host is empty", routeName)
		}
		return "", err
	}

	u := &url.URL{Scheme: "https", Host: route.Spec.Host}
	return u.String(), nil
}

func (r *AgentServiceConfigReconciler) newAssistedCM(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	serviceURL, err := r.urlForRoute(ctx, serviceName)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", serviceName)
		return nil, nil, err
	}

	imageServiceURL, err := r.urlForRoute(ctx, imageServiceName)
	if err != nil {
		log.WithError(err).Warnf("Failed to get URL for route %s", imageServiceName)
		return nil, nil, err
	}

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
			"SERVICE_BASE_URL":       serviceURL,
			"IMAGE_SERVICE_BASE_URL": imageServiceURL,

			// image overrides
			"AGENT_DOCKER_IMAGE":     AgentImage(),
			"CONTROLLER_IMAGE":       ControllerImage(),
			"INSTALLER_IMAGE":        InstallerImage(),
			"SELF_VERSION":           ServiceImage(),
			"OPENSHIFT_VERSIONS":     r.getOpenshiftVersions(log, instance),
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

	return cm, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newImageServiceDeployment(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	deploymentLabels := map[string]string{
		"app": imageServiceName,
	}

	container := corev1.Container{
		Name:            imageServiceName,
		Image:           ImageServiceImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: int32(imageHandlerPort.IntValue()),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			{Name: "LISTEN_PORT", Value: imageHandlerPort.String()},
			{Name: "RHCOS_VERSIONS", Value: r.getOSImages(log, instance)},
			{Name: "HTTPS_CERT_FILE", Value: "/etc/image-service/certs/tls.crt"},
			{Name: "HTTPS_KEY_FILE", Value: "/etc/image-service/certs/tls.key"},
			{Name: "HTTPS_CA_FILE", Value: "/etc/image-service/ca-bundle/service-ca.crt"},
			{Name: "ASSISTED_SERVICE_SCHEME", Value: "https"},
			{Name: "ASSISTED_SERVICE_HOST", Value: serviceName + "." + r.Namespace + ".svc:" + servicePort.String()},
			{Name: "REQUEST_AUTH_TYPE", Value: "param"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "tls-certs", MountPath: "/etc/image-service/certs"},
			{Name: "service-cabundle", MountPath: "/etc/image-service/ca-bundle"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   imageHandlerPort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: imageServiceName,
				},
			},
		},
		{
			Name: "service-cabundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: imageServiceName,
					},
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageServiceName,
			Namespace: r.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
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
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
			return err
		}
		var replicas int32 = 1
		deployment.Spec.Replicas = &replicas

		deployment.Spec.Template.Spec.Containers = []corev1.Container{container}
		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.ServiceAccountName = imageServiceName

		if r.NodeSelector != nil {
			nodeSelector := make(map[string]string)
			for key, value := range r.NodeSelector {
				nodeSelector[key] = value
			}
			deployment.Spec.Template.Spec.NodeSelector = nodeSelector
		}
		if r.Tolerations != nil {
			deployment.Spec.Template.Spec.Tolerations = append([]corev1.Toleration{}, r.Tolerations...)
		}
		return nil
	}

	return deployment, mutateFn, nil
}

func (r *AgentServiceConfigReconciler) newAssistedServiceDeployment(ctx context.Context, log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) (client.Object, controllerutil.MutateFn, error) {
	var assistedConfigHash, mirrorConfigHash, userConfigHash string

	// Get hash of generated assisted config
	assistedConfigHash, err := r.getCMHash(ctx, types.NamespacedName{Name: serviceName, Namespace: r.Namespace})
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
		log.Infof("ConfigMap %s from namespace %s being used to configure assisted-service deployment", userConfigName, r.Namespace)
		userConfigHash, err = r.getCMHash(ctx, types.NamespacedName{Name: userConfigName, Namespace: r.Namespace})
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
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   servicePort,
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/ready",
					Port:   servicePort,
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

	if instance.Spec.MirrorRegistryRef != nil {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.MirrorRegistryRef.Name, Namespace: r.Namespace}, cm)
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
			nodeSelector := make(map[string]string)
			for key, value := range r.NodeSelector {
				nodeSelector[key] = value
			}
			deployment.Spec.Template.Spec.NodeSelector = nodeSelector
		}
		if r.Tolerations != nil {
			deployment.Spec.Template.Spec.Tolerations = append([]corev1.Toleration{}, r.Tolerations...)
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
// 1. If mustGatherImages field is not present in the AgentServiceConfig's Spec
//    it returns the value of MUST_GATHER_IMAGES env variable. This is also the
//    fallback behavior in case of a processing error
//
// 2. If mustGatherImages field is present in the AgentServiceConfig's Spec it
//    converts the structure to the one that can be recognize by the service
//    and returns it as a JSON string
//
// 3. In case both sources are present, the Spec values overrides the env
//    values
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
// 1. If osImages field is not present in the AgentServiceConfig's Spec
//    it returns the value of OS_IMAGES env variable.
//    This is also the fallback behavior in case of a processing error.
//
// 2. If osImages field is present in the AgentServiceConfig's Spec it
//    converts the structure to the one that can be recognize by the service
//    and returns it as a JSON string.
//
// 3. In case both sources are present, the Spec values overrides the env
//    values.
func (r *AgentServiceConfigReconciler) getOSImages(log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) string {
	if instance.Spec.OSImages == nil {
		return OSImages()
	}

	osImages := make(models.OsImages, 0)
	for i := range instance.Spec.OSImages {
		osImage := models.OsImage{
			OpenshiftVersion: &instance.Spec.OSImages[i].OpenshiftVersion,
			URL:              &instance.Spec.OSImages[i].Url,
			RootfsURL:        &instance.Spec.OSImages[i].RootFSUrl,
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

// getOpenshiftVersions returns the value of OPENSHIFT_VERSIONS variable
// to be stored in the service's ConfigMap
//
// 1. If osImages field is not present in the AgentServiceConfig's Spec
//    it returns the value of OPENSHIFT_VERSIONS env variable. This is
//    also the fallback behavior in case there are no valid versions
//    in the Spec
//
// 2. if osImages field is present in the AgentServiceConfig's Spec it
//    converts the structure to the one that can be recognize by the service
//    and returns it as a JSON string
//
// 3. In case both sources are present, the Spec values overrides the env
//    values
func (r *AgentServiceConfigReconciler) getOpenshiftVersions(log logrus.FieldLogger, instance *aiv1beta1.AgentServiceConfig) string {
	if instance.Spec.OSImages == nil {
		return OpenshiftVersions()
	}

	openshiftVersions := make(models.OpenshiftVersions)
	for i, image := range instance.Spec.OSImages {
		key, err := getVersionKey(image.OpenshiftVersion)
		if err != nil {
			log.WithError(err).Error(fmt.Sprintf("Problem parsing OpenShift version %v, skipping.", image.OpenshiftVersion))
			continue
		}

		openshiftVersion := models.OpenshiftVersion{
			DisplayName:  &key,
			RhcosVersion: &instance.Spec.OSImages[i].Version,
			RhcosImage:   &instance.Spec.OSImages[i].Url,
			RhcosRootfs:  &instance.Spec.OSImages[i].RootFSUrl,
		}

		// the last entry for a particular OpenShift version takes precedence.
		openshiftVersions[key] = openshiftVersion
	}

	if len(openshiftVersions) == 0 {
		log.Info("No valid OS Image specified, returning default", "OpenShift Versions", OpenshiftVersions())
		return OpenshiftVersions()
	}

	encodedVersions, err := json.Marshal(openshiftVersions)
	if err != nil {
		log.WithError(err).Error(fmt.Sprintf("Problem marshaling versions (%v) to string, returning default %v", openshiftVersions, OpenshiftVersions()))
		return OpenshiftVersions()
	}

	return string(encodedVersions)
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
