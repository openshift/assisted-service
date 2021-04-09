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
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// agentServiceConfigName is the one and only name for an AgentServiceConfig
	// supported in the cluster. Any others will be ignored.
	agentServiceConfigName = "agent"
	// filesystemPVCName is the name of the PVC created for assisted-service's filesystem.
	filesystemPVCName = "agent-filesystem"
	// databasePVCName is the name of the PVC created for postgresql.
	databasePVCName = "agent-database"

	name                                = "assisted-service"
	databaseName                        = "postgres"
	databaseSecretName                  = databaseName
	databasePort                  int32 = 5432
	servicePort                   int32 = 8090
	assistedServiceDeploymentName       = "assisted-service"

	// assistedServiceContainerName is the Name property of the assisted-service container
	assistedServiceContainerName string = "assisted-service"
	// databaseContainerName is the Name property of the postgres container
	databaseContainerName string = databaseName
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

// +kubebuilder:rbac:groups=adi.io.my.domain,resources=agentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=agentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=agentserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *AgentServiceConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	instance := &adiiov1alpha1.AgentServiceConfig{}

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

	for _, f := range []func(context.Context, *adiiov1alpha1.AgentServiceConfig) error{
		r.ensureFilesystemStorage,
		r.ensureDatabaseStorage,
		r.ensureAgentService,
		r.ensureAgentRoute,
		r.ensurePostgresSecret,
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

	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    adiiov1alpha1.ConditionReconcileCompleted,
		Status:  corev1.ConditionTrue,
		Reason:  adiiov1alpha1.ReasonReconcileSucceeded,
		Message: "AgentServiceConfig reconcile completed without error.",
	})
	return ctrl.Result{}, r.Status().Update(ctx, instance)
}

func (r *AgentServiceConfigReconciler) ensureFilesystemStorage(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	pvc, mutateFn := r.newFilesystemPVC(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonStorageFailure,
			Message: "Failed to ensure filesystem storage: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Filesystem storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureDatabaseStorage(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	pvc, mutateFn := r.newDatabasePVC(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonStorageFailure,
			Message: "Failed to ensure database storage: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Database storage created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentService(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	svc, mutateFn := r.newAgentService(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonAgentServiceFailure,
			Message: "Failed to ensure agent service: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Agent service created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAgentRoute(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	route, mutateFn := r.newAgentRoute(instance)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonAgentRouteFailure,
			Message: "Failed to ensure agent route: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Agent route created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensurePostgresSecret(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	// TODO(djzager): using controllerutil.CreateOrUpdate is convenient but we may
	// want to consider simply creating the secret if we can't find instead of
	// generating a secret every reconcile.
	secret, mutateFn, err := r.newPostgresSecret(instance)
	if err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonPostgresSecretFailure,
			Message: "Failed to generate database secret: " + err.Error(),
		})
		return err
	}

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonPostgresSecretFailure,
			Message: "Failed to ensure database secret: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Database secret created")
	}
	return nil
}

func (r *AgentServiceConfigReconciler) ensureAssistedServiceDeployment(ctx context.Context, instance *adiiov1alpha1.AgentServiceConfig) error {
	// must have the route in order to populate SERVICE_BASE_URL for the service
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: r.Namespace}, route)
	if err != nil || route.Spec.Host == "" {
		if err == nil {
			err = fmt.Errorf("Route's host is empty")
		}
		r.Log.Info("Failed to get route or route's host is empty")
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonDeploymentFailure,
			Message: "Failed to get route for assisted service: " + err.Error(),
		})
		return err
	}

	deployment, mutateFn := r.newAssistedServiceDeployment(instance, route.Spec.Host)

	if result, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, mutateFn); err != nil {
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ConditionReconcileCompleted,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ReasonDeploymentFailure,
			Message: "Failed to ensure assisted service deployment: " + err.Error(),
		})
		return err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Assisted service deployment created")
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.AgentServiceConfig{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&routev1.Route{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *AgentServiceConfigReconciler) newFilesystemPVC(instance *adiiov1alpha1.AgentServiceConfig) (*corev1.PersistentVolumeClaim, controllerutil.MutateFn) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      filesystemPVCName,
			Namespace: r.Namespace,
		},
		Spec: instance.Spec.FileSystemStorage,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		// TODO(djzager): this is a map that should be copied to avoid
		// unexpected side effects in the cache.
		pvc.Spec.Resources.Requests = instance.Spec.FileSystemStorage.Resources.Requests
		return nil
	}

	return pvc, mutateFn
}

func (r *AgentServiceConfigReconciler) newDatabasePVC(instance *adiiov1alpha1.AgentServiceConfig) (*corev1.PersistentVolumeClaim, controllerutil.MutateFn) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databasePVCName,
			Namespace: r.Namespace,
		},
		Spec: instance.Spec.DatabaseStorage,
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
			return err
		}
		// Everything else is immutable once bound.
		// TODO(djzager): this is a map that should be copied to avoid
		// unexpected side effects in the cache.
		pvc.Spec.Resources.Requests = instance.Spec.DatabaseStorage.Resources.Requests
		return nil
	}

	return pvc, mutateFn
}

func (r *AgentServiceConfigReconciler) newAgentService(instance *adiiov1alpha1.AgentServiceConfig) (*corev1.Service, controllerutil.MutateFn) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		addAppLabel(name, &svc.ObjectMeta)
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		// For convenience targetPort, when unset, is set to the same as port
		// https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service
		// so we don't set it.
		svc.Spec.Ports[0].Name = name
		svc.Spec.Ports[0].Port = servicePort
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Selector = map[string]string{"app": name}
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer
		return nil
	}

	return svc, mutateFn
}

func (r *AgentServiceConfigReconciler) newAgentRoute(instance *adiiov1alpha1.AgentServiceConfig) (*routev1.Route, controllerutil.MutateFn) {
	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace,
		},
	}
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   name,
			Weight: &weight,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString(name),
		},
		WildcardPolicy: routev1.WildcardPolicyNone,
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

func (r *AgentServiceConfigReconciler) newPostgresSecret(instance *adiiov1alpha1.AgentServiceConfig) (*corev1.Secret, controllerutil.MutateFn, error) {
	pass, err := generatePassword()
	if err != nil {
		return nil, nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      databaseSecretName,
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

func (r *AgentServiceConfigReconciler) newAssistedServiceDeployment(instance *adiiov1alpha1.AgentServiceConfig, host string) (*appsv1.Deployment, controllerutil.MutateFn) {
	serviceEnv := []corev1.EnvVar{
		{Name: "SERVICE_BASE_URL", Value: host},

		// TODO: FIX ME!!!
		{Name: "SKIP_CERT_VERIFICATION", Value: "True"},

		// database
		newSecretEnvVar("DB_HOST", "db.host", databaseSecretName),
		newSecretEnvVar("DB_NAME", "db.name", databaseSecretName),
		newSecretEnvVar("DB_PASS", "db.password", databaseSecretName),
		newSecretEnvVar("DB_PORT", "db.port", databaseSecretName),
		newSecretEnvVar("DB_USER", "db.user", databaseSecretName),

		// image overrides
		{Name: "AGENT_DOCKER_IMAGE", Value: AgentImage()},
		{Name: "CONTROLLER_IMAGE", Value: ControllerImage()},
		{Name: "INSTALLER_IMAGE", Value: InstallerImage()},
		{Name: "SELF_VERSION", Value: ServiceImage()},

		{Name: "ISO_IMAGE_TYPE", Value: "minimal-iso"},
		{Name: "S3_USE_SSL", Value: "false"},
		{Name: "LOG_LEVEL", Value: "info"},
		{Name: "LOG_FORMAT", Value: "text"},
		{Name: "INSTALL_RH_CA", Value: "false"},
		{Name: "REGISTRY_CREDS", Value: ""},
		{Name: "AWS_SHARED_CREDENTIALS_FILE", Value: "/etc/.aws/credentials"},
		{Name: "DEPLOY_TARGET", Value: "k8s"},
		{Name: "STORAGE", Value: "filesystem"},
		{Name: "ISO_WORKSPACE_BASE_DIR", Value: "/data"},
		{Name: "ISO_CACHE_DIR", Value: "/data/cache"},

		// from configmap
		{Name: "AUTH_TYPE", Value: "none"},
		{Name: "BASE_DNS_DOMAINS", Value: ""},
		{Name: "CHECK_CLUSTER_VERSION", Value: "False"},
		{Name: "CREATE_S3_BUCKET", Value: "False"},
		{Name: "ENABLE_KUBE_API", Value: "True"},
		{Name: "ENABLE_SINGLE_NODE_DNSMASQ", Value: "True"},
		{Name: "IPV6_SUPPORT", Value: "False"},
		{Name: "JWKS_URL", Value: "https://api.openshift.com/.well-known/jwks.json"},
		{Name: "OPENSHIFT_VERSIONS", Value: `{"4.6":{"display_name":"4.6.16","release_image":"quay.io/openshift-release-dev/ocp-release:4.6.16-x86_64","rhcos_image":"https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/4.6.8/rhcos-4.6.8-x86_64-live.x86_64.iso","rhcos_version":"46.82.202012051820-0","support_level":"production"},"4.7":{"display_name":"4.7.2","release_image":"quay.io/openshift-release-dev/ocp-release:4.7.2-x86_64","rhcos_image":"https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.7/4.7.0/rhcos-4.7.0-x86_64-live.x86_64.iso","rhcos_version":"47.83.202102090044-0","support_level":"production"}}`},
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
	}

	serviceContainer := corev1.Container{
		Name:  assistedServiceContainerName,
		Image: ServiceImage(),
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: servicePort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: serviceEnv,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "bucket-filesystem",
				MountPath: "/data",
			},
		},
		Resources: corev1.ResourceRequirements{},
		LivenessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(int(servicePort)),
				},
			},
		},
	}

	postgresContainer := corev1.Container{
		Name:            databaseContainerName,
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
			newSecretEnvVar("POSTGRESQL_DATABASE", "db.name", databaseSecretName),
			newSecretEnvVar("POSTGRESQL_USER", "db.user", databaseSecretName),
			newSecretEnvVar("POSTGRESQL_PASSWORD", "db.password", databaseSecretName),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "postgresdb",
				MountPath: "/var/lib/pgsql/data",
			},
		},
		Resources: corev1.ResourceRequirements{},
	}

	volumes := []corev1.Volume{
		{
			Name: "bucket-filesystem",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: filesystemPVCName,
				},
			},
		},
		{
			Name: "postgresdb",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: databasePVCName,
				},
			},
		},
	}

	deploymentLabels := map[string]string{
		"app": assistedServiceDeploymentName,
	}

	deploymentStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RecreateDeploymentStrategyType,
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      assistedServiceDeploymentName,
			Namespace: r.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
					Name:   assistedServiceDeploymentName,
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

// generatePassword generates a password of a given length out of the acceptable
// ASCII characters suitable for a password
// taken from https://github.com/CrunchyData/postgres-operator/blob/383dfa95991553352623f14d3d0d4c9193795855/internal/util/secrets.go#L75
func generatePassword() (string, error) {
	length := 16
	password := make([]byte, length)

	// passwordCharLower is the lowest ASCII character to use for generating a
	// password, which is 40
	passwordCharLower := int64(40)
	// passwordCharUpper is the highest ASCII character to use for generating a
	// password, which is 126
	passwordCharUpper := int64(126)
	// passwordCharExclude is a map of characters that we choose to exclude from
	// the password to simplify usage in the shell. There is still enough entropy
	// that exclusion of these characters is OK.
	passwordCharExclude := "`\\"

	// passwordCharSelector is a "big int" that we need to select the random ASCII
	// character for the password. Since the random integer generator looks for
	// values from [0,X), we need to force this to be [40,126]
	passwordCharSelector := big.NewInt(passwordCharUpper - passwordCharLower)

	i := 0

	for i < length {
		val, err := rand.Int(rand.Reader, passwordCharSelector)
		// if there is an error generating the random integer, return
		if err != nil {
			return "", err
		}

		char := byte(passwordCharLower + val.Int64())

		// if the character is in the exclusion list, continue
		if idx := strings.IndexAny(string(char), passwordCharExclude); idx > -1 {
			continue
		}

		password[i] = char
		i++
	}

	return string(password), nil
}
