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

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/config"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	logutil "github.com/openshift/assisted-service/pkg/log"
	pkgerror "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	kubeconfigSecretVolumeName string = "kubeconfig"
	kubeconfigSecretVolumePath string = "/etc/kube"
	kubeconfigPath             string = "/etc/kube/kubeconfig"
)

// HypershiftAgentServiceConfigReconciler reconciles a HypershiftAgentServiceConfig object
type HypershiftAgentServiceConfigReconciler struct {
	AgentServiceConfigReconcileContext

	SpokeClients SpokeClientCache

	// Namespace the operator is running in
	Namespace string
}

func (asc *ASC) initHASC(r *HypershiftAgentServiceConfigReconciler, instance *aiv1beta1.HypershiftAgentServiceConfig) {
	asc.namespace = instance.Namespace
	asc.rec = &r.AgentServiceConfigReconcileContext
	asc.Object = instance
	asc.spec = &instance.Spec.AgentServiceConfigSpec
	asc.conditions = instance.Status.Conditions
}

var assistedServiceRBAC_l0 = []component{
	{"AssistedServiceLeaderRole", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRole},
	{"AssistedServiceLeaderRoleBinding", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRoleBinding},
}

var assistedServiceRBAC_l1 = []component{
	{"AssistedServiceRole_l1", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRole},
	{"AssistedServiceRoleBinding_l1", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRoleBinding},
	{"AssistedServiceClusterRole_l1", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceClusterRole},
	{"AssistedServiceClusterRoleBinding_l1", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceClusterRoleBinding},
	{"AssistedServiceServiceAccount_l1", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceServiceAccount},
}

// Adding required resources to rbac
// - customresourcedefinitions: needed for listing hub CRDs
// - leases: needed to allow applying leader election roles on hub
// - roles/rolebindings: needed for applying roles on hub

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

func (hr *HypershiftAgentServiceConfigReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var asc ASC
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, hr.Log).WithFields(
		logrus.Fields{
			"hypershift_service_config":    req.Name,
			"hypershift_service_namespace": req.Namespace,
		})

	log.Info("HypershiftAgentServiceConfig Reconcile started")
	defer log.Info("HypershiftAgentServiceConfig Reconcile ended")

	//read the resource from k8s
	instance := &aiv1beta1.HypershiftAgentServiceConfig{}
	if err := hr.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, err
	}
	asc.initHASC(hr, instance)

	// Creating spoke client using specified kubeconfig secret reference
	spokeClient, err := hr.createSpokeClient(ctx, instance.Spec.KubeconfigSecretRef.Name, instance.Namespace)
	if err != nil {
		log.WithError(err).Error("Failed to create client using specified kubeconfig", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Ensure agent-install CRDs exist on spoke cluster and clean stale ones
	if err := hr.syncSpokeAgentInstallCRDs(ctx, spokeClient); err != nil {
		log.WithError(err).Error(fmt.Sprintf("Failed to sync agent-install CRDs on spoke cluster in namespace '%s'", instance.Namespace))
		return ctrl.Result{}, err
	}

	// Reconcile hub components
	hubComponents := append(assistedServiceRBAC_l0, getComponents()...)
	for _, component := range hubComponents {
		switch component.name {
		case "AssistedServiceDeployment":
			component.fn = newHypershiftAssistedServiceDeployment
		}

		log.Infof(fmt.Sprintf("Reconcile hub component: %s", component.name))
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			return result, err
		}
	}

	// Reconcile spoke components
	asc.rec.Client = spokeClient
	spokeComponents := assistedServiceRBAC_l1
	for _, component := range spokeComponents {
		log.Infof(fmt.Sprintf("Reconcile spoke component: %s", component.name))
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			return result, err
		}
	}

	return ctrl.Result{}, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) createSpokeClient(ctx context.Context, kubeconfigSecretName, namespace string) (spoke_k8s_client.SpokeK8sClient, error) {
	// Fetch kubeconfig secret by specified secret reference
	kubeconfigSecret, err := hr.getKubeconfigSecret(ctx, kubeconfigSecretName, namespace)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to get secret '%s' in '%s' namespace", kubeconfigSecretName, namespace)
	}

	// Create spoke cluster client using kubeconfig secret
	spokeClient, err := hr.SpokeClients.Get(kubeconfigSecret)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to create client using kubeconfig secret '%s'", kubeconfigSecretName)
	}

	return spokeClient, nil
}

// Return kubeconfig secret by name and namespace
func (hr *HypershiftAgentServiceConfigReconciler) getKubeconfigSecret(ctx context.Context, kubeconfigSecretName, namespace string) (*corev1.Secret, error) {
	secretRef := types.NamespacedName{Namespace: namespace, Name: kubeconfigSecretName}
	secret, err := getSecret(ctx, hr.Client, hr, secretRef)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to get '%s' secret in '%s' namespace", secretRef.Name, secretRef.Namespace)
	}
	_, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, pkgerror.Errorf("Secret '%s' does not contain '%s' key value", secretRef.Name, "kubeconfig")
	}
	return secret, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) syncSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client) error {
	// Fetch agent-install CRDs using in-cluster client
	localCRDs, err := hr.getAgentInstallCRDs(ctx, hr.Client)
	if err != nil {
		return err
	}
	if len(localCRDs.Items) == 0 {
		return pkgerror.New("agent-install CRDs are not available")
	}

	// Ensure local agent-install CRDs exist on the spoke cluster
	if err := hr.ensureSpokeAgentInstallCRDs(ctx, spokeClient, localCRDs); err != nil {
		return pkgerror.New("Failed to create agent-install CRDs on spoke cluster")
	}

	// Delete agent-install CRDs that don't exist locally
	if err := hr.cleanStaleSpokeAgentInstallCRDs(ctx, spokeClient, localCRDs); err != nil {
		hr.Log.WithError(err).Warn("Failed to remove stale agent-install CRDs from spoke cluster in namespace")
		return nil
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) ensureSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client, localCRDs *apiextensionsv1.CustomResourceDefinitionList) error {
	// Ensure CRDs exist on the spoke cluster
	for _, item := range localCRDs.Items {
		crd := item
		c := crd.DeepCopy()
		c.ResourceVersion = "" // ResourceVersion should not be set on objects to be created
		mutate := func() error {
			c.Spec = crd.Spec
			c.Annotations = crd.Annotations
			c.Labels = crd.Labels
			return nil
		}
		result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, c, mutate)
		if err != nil {
			return pkgerror.Wrapf(err, "Failed to create CRD '%s' on spoke cluster", crd.Name)
		}
		if result != controllerutil.OperationResultNone {
			hr.Log.Infof("CRD '%s' %s on spoke cluster", crd.Name, result)
		}
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) cleanStaleSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client, localCRDs *apiextensionsv1.CustomResourceDefinitionList) error {
	// Fetch agent-install CRDs using spoke client
	spokeCRDs, err := hr.getAgentInstallCRDs(ctx, spokeClient)
	if err != nil {
		return err
	}

	// Remove spoke CRDs that don't exist locally in-cluster
	for _, item := range spokeCRDs.Items {
		crd := item
		existsInCluster := funk.Contains(localCRDs.Items, func(localCRD apiextensionsv1.CustomResourceDefinition) bool {
			return localCRD.Name == crd.Name
		})
		if !existsInCluster {
			if err := spokeClient.Delete(ctx, crd.DeepCopy()); err != nil {
				hr.Log.WithError(err).Warnf("Failed to delete CRD '%s' from spoke cluster", crd.Name)
			}
		}
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) getAgentInstallCRDs(ctx context.Context, c client.Client) (*apiextensionsv1.CustomResourceDefinitionList, error) {
	crds := &apiextensionsv1.CustomResourceDefinitionList{}
	listOpts := []client.ListOption{
		client.HasLabels{fmt.Sprintf("operators.coreos.com/assisted-service-operator.%s", hr.Namespace)},
	}
	if err := c.List(ctx, crds, listOpts...); err != nil {
		return nil, pkgerror.Wrap(err, "Failed to list CRDs")
	}
	return crds, nil
}

// SetupWithManager sets up the controller with the Manager.
func (hr *HypershiftAgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.HypershiftAgentServiceConfig{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(hr)
}

func parseFile(fileName string, dest interface{}) error {
	//read object
	bytes, err := config.RbacFiles.ReadFile(fileName)
	if err != nil {
		return err
	}

	//decode yaml file into client object
	into := unstructured.Unstructured{}
	_, _, err = k8scheme.Codecs.UniversalDecoder().Decode(bytes, nil, &into)
	if err != nil {
		return err
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(into.UnstructuredContent(), dest)
	if err != nil {
		return err
	}
	return nil
}

func createRoleFn(namespace, roleFile string) (client.Object, controllerutil.MutateFn, error) {
	template := rbacv1.Role{}

	err := parseFile(roleFile, &template)
	if err != nil {
		return nil, nil, err
	}

	cr := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      template.Name,
			Namespace: namespace,
		},
		Rules: template.Rules,
	}

	mutateFn := func() error {
		cr.Rules = template.Rules
		return nil
	}
	return &cr, mutateFn, nil
}

func createClusterRoleFn(namespace, roleFile string) (client.Object, controllerutil.MutateFn, error) {
	template := rbacv1.ClusterRole{}

	err := parseFile(roleFile, &template)
	if err != nil {
		return nil, nil, err
	}

	cr := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: template.Name,
		},
		Rules: template.Rules,
	}

	mutateFn := func() error {
		cr.Rules = template.Rules
		return nil
	}
	return &cr, mutateFn, nil
}

func createClusterRoleBindingFn(namespace, roleFile string) (client.Object, controllerutil.MutateFn, error) {
	template := rbacv1.ClusterRoleBinding{}

	err := parseFile(roleFile, &template)
	if err != nil {
		return nil, nil, err
	}

	cr := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: template.Name,
		},
		Subjects: template.Subjects,
		RoleRef:  template.RoleRef,
	}

	mutateFn := func() error {
		cr.Subjects = template.Subjects
		cr.RoleRef = template.RoleRef
		return nil
	}
	return &cr, mutateFn, nil
}

func createRoleBindingFn(namespace, roleFile string) (client.Object, controllerutil.MutateFn, error) {
	template := rbacv1.RoleBinding{}

	err := parseFile(roleFile, &template)
	if err != nil {
		return nil, nil, err
	}

	cr := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      template.Name,
			Namespace: namespace,
		},
		Subjects: template.Subjects,
		RoleRef:  template.RoleRef,
	}

	mutateFn := func() error {
		cr.Subjects = template.Subjects
		cr.RoleRef = template.RoleRef
		return nil
	}
	return &cr, mutateFn, nil
}

func createServiceAccountFn(name, namespace string) (client.Object, controllerutil.MutateFn, error) {
	sa := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	mutateFn := func() error {
		return nil
	}
	return &sa, mutateFn, nil
}

func newAssistedServiceRole(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	roleFile := "rbac/base/role.yaml"
	return createRoleFn(asc.namespace, roleFile)
}

func newAssistedServiceRoleBinding(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	roleFile := "rbac/base/role_binding.yaml"
	return createRoleBindingFn(asc.namespace, roleFile)
}

func newAssistedServiceClusterRole(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	roleFile := "rbac/role.yaml"
	return createClusterRoleFn(asc.namespace, roleFile)
}

func newAssistedServiceClusterRoleBinding(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	roleFile := "rbac/role_binding.yaml"
	return createClusterRoleBindingFn(asc.namespace, roleFile)
}

func newAssistedServiceServiceAccount(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	return createServiceAccountFn("assisted-service", asc.namespace)
}

func newHypershiftAssistedServiceDeployment(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	obj, mutateFn, err := newAssistedServiceDeployment(ctx, log, asc)
	if err != nil {
		return nil, nil, err
	}

	deployment := obj.(*appsv1.Deployment)
	newMutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, deployment, asc.rec.Scheme); err != nil {
			return err
		}

		if err := mutateFn(); err != nil {
			return err
		}

		// Add volume with kubeconfig secret
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: kubeconfigSecretVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: asc.Object.(*aiv1beta1.HypershiftAgentServiceConfig).Spec.KubeconfigSecretRef.Name,
					},
				},
			},
		)

		// Add kubeconfig volume mount
		serviceContainer := deployment.Spec.Template.Spec.Containers[0]
		serviceContainer.VolumeMounts = append(serviceContainer.VolumeMounts, corev1.VolumeMount{
			Name:      kubeconfigSecretVolumeName,
			MountPath: kubeconfigSecretVolumePath,
		})

		// Add env var to the kubeconfig file on mounted path
		serviceContainer.Env = append(serviceContainer.Env, corev1.EnvVar{Name: "KUBECONFIG", Value: kubeconfigPath})
		deployment.Spec.Template.Spec.Containers[0] = serviceContainer

		return nil
	}

	return deployment, newMutateFn, nil
}
