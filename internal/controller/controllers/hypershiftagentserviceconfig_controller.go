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
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/config"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	pkgerror "github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	kubeconfigSecretVolumeName                string = "kubeconfig"
	kubeconfigSecretVolumePath                string = "/etc/kube"
	kubeconfigPath                            string = "/etc/kube/kubeconfig"
	hypershiftAgentServiceConfigFinalizerName string = "agentserviceconfig." + aiv1beta1.Group + "/ai-deprovision"

	defaultServingCertNamespace string = "openshift-service-ca-operator"
	defaultServingCertCMName    string = "openshift-service-ca.crt"
)

/** parameter name for exporting the webhook admission clusterIP */
const clusterIPParam string = "ClusterIP"

// HypershiftAgentServiceConfigReconciler reconciles a HypershiftAgentServiceConfig object
type HypershiftAgentServiceConfigReconciler struct {
	client.Client

	AgentServiceConfigReconcileContext

	// A cache for the spoke clients
	SpokeClients SpokeClientCache
}

func initHASC(r *HypershiftAgentServiceConfigReconciler, instance *aiv1beta1.HypershiftAgentServiceConfig,
	client client.Client, properties map[string]interface{}) ASC {

	var asc ASC
	asc.namespace = instance.Namespace
	asc.Client = client
	asc.Object = instance
	asc.spec = &instance.Spec.AgentServiceConfigSpec
	asc.conditions = &instance.Status.Conditions
	asc.properties = properties
	asc.rec = &r.AgentServiceConfigReconcileContext
	return asc
}

var assistedServiceRBAC_hub = []component{
	{"AssistedServiceLeaderRole", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRole},
	{"AssistedServiceLeaderRoleBinding", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRoleBinding},
	{"AssistedServiceServiceAccount", aiv1beta1.ReasonServiceServiceAccount, newAssistedServiceServiceAccount},
	{"WebHookServiceAccount", aiv1beta1.ReasonWebHookServiceAccountFailure, newWebHookServiceAccount},
	{"WebHookClusterRole_hub", aiv1beta1.ReasonWebHookClusterRoleFailure, newWebHookClusterRole},
	{"WebHookClusterRoleBinding_hub", aiv1beta1.ReasonWebHookClusterRoleBindingFailure, newWebHookClusterRoleBinding},
}

var assistedServiceRBAC_spoke = []component{
	{"AssistedServiceRole_spoke", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRole},
	{"AssistedServiceRoleBinding_spoke", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceRoleBinding},
	{"AssistedServiceClusterRole_spoke", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceClusterRole},
	{"AssistedServiceClusterRoleBinding_spoke", aiv1beta1.ReasonRBACConfigurationFailure, newAssistedServiceClusterRoleBinding},
	{"AssistedServiceServiceAccount_spoke", aiv1beta1.ReasonServiceServiceAccount, newAssistedServiceServiceAccount},
	{"WebHookClusterRole_spoke", aiv1beta1.ReasonWebHookClusterRoleFailure, newWebHookClusterRole},
	{"WebHookClusterRoleBinding_spoke", aiv1beta1.ReasonWebHookClusterRoleBindingFailure, newWebHookClusterRoleBinding},
}

func (hr *HypershiftAgentServiceConfigReconciler) getWebhookComponents_spoke() []component {
	return []component{
		{"AgentClusterInstallValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newACIWebHook},
		{"AgentClusterInstallMutatingWebHook", aiv1beta1.ReasonMutatingWebHookFailure, newACIMutatWebHook},
		{"InfraEnvValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newInfraEnvWebHook},
		{"AgentValidatingWebHook", aiv1beta1.ReasonValidatingWebHookFailure, newAgentWebHook},
		{"WebHookHostedService", aiv1beta1.ReasonWebHookServiceFailure, newHeadlessWebHookService},
		{"WebHookEndpoint", aiv1beta1.ReasonWebHookEndpointFailure, newWebHookEndpoint},
		{"WebHookAPIService", aiv1beta1.ReasonWebHookAPIServiceFailure, newHypershiftWebHookAPIService},
	}
}

func (hr *HypershiftAgentServiceConfigReconciler) getWebhookComponents_hub() []component {
	return []component{
		{"WebHookService", aiv1beta1.ReasonWebHookServiceFailure, newWebHookService},
		{"WebHookServiceDeployment", aiv1beta1.ReasonWebHookDeploymentFailure, newHypershiftWebHookDeployment},
	}
}

// Adding required resources to rbac
// - customresourcedefinitions: needed for listing hub CRDs
// - leases: needed to allow applying leader election roles on hub
// - roles/rolebindings: needed for applying roles on hub

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

func (hr *HypershiftAgentServiceConfigReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var result reconcile.Result
	var valid bool

	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, hr.Log).WithFields(
		logrus.Fields{
			"hypershift_service_config":    req.Name,
			"hypershift_service_namespace": req.Namespace,
		})

	log.Info("HypershiftAgentServiceConfig Reconcile started")
	defer log.Info("HypershiftAgentServiceConfig Reconcile ended")

	// read the resource from k8s
	instance := &aiv1beta1.HypershiftAgentServiceConfig{}
	if err = hr.Get(ctx, req.NamespacedName, instance); err != nil {
		log.WithError(err).Errorf("Failed to get HypershiftAgentServiceConfig %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Create a map for properties that are shared between ASC objects
	ascProperties := make(map[string]interface{})

	// Initialize ASC to reconcile components according to instance context
	asc := initHASC(hr, instance, hr.Client, ascProperties)

	// Creating spoke client using specified kubeconfig secret reference
	log.Info("creating spoke client on namespace")
	spokeClient, err := hr.createSpokeClient(ctx, log, instance.Spec.KubeconfigSecretRef.Name, asc)
	if err != nil {
		log.WithError(err).Errorf("Failed to create client using %s on namespace %s", instance.Spec.KubeconfigSecretRef.Name, req.NamespacedName)
		return ctrl.Result{Requeue: true}, err
	}

	// Ensure agent-install CRDs exist on spoke cluster and clean stale ones
	log.Info("synching agent-install CRDs on spoke cluster in namespace")
	if err = hr.ensureSyncSpokeAgentInstallCRDs(ctx, log, spokeClient, asc); err != nil {
		log.WithError(err).Error(fmt.Sprintf("Failed to sync agent-install CRDs on spoke cluster in namespace '%s'", instance.Namespace))
		return ctrl.Result{}, err
	}

	// Ensure relevant finalizers exist (cleanup on deletion)
	log.Infof("handling finalizers %s", hypershiftAgentServiceConfigFinalizerName)
	if err = ensureFinalizers(ctx, log, asc, hypershiftAgentServiceConfigFinalizerName); err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	if !instance.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Invoke validation funcs
	valid, err = validate(ctx, log, asc)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	if !valid {
		return ctrl.Result{}, nil
	}

	// Remove IPXE HTTP routes if not needed
	log.Infof("cleaning ipxe http route")
	if err = cleanHTTPRoute(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	// Reconcile hub components
	log.Info("reconciling hub components ... ")
	if result, err = hr.reconcileHubComponents(ctx, log, asc); err != nil {
		return result, err
	}

	// Ensure image-service StatefulSet is reconciled
	if err = ensureImageServiceStatefulSet(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	log.Info("read certificate from hub")
	if err = readServiceCertificate(ctx, log, asc); err != nil {
		return ctrl.Result{}, err
	}

	// Initialize spokeASC with the spoke client
	spokeASC := initHASC(hr, instance, spokeClient, ascProperties)

	// Ensure instance namespace exists on spoke cluster
	if err = hr.ensureSpokeNamespace(ctx, log, spokeASC); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile spoke components
	log.Info("reconciling spoke components ... ")
	if result, err = hr.reconcileSpokeComponents(ctx, log, spokeASC); err != nil {
		return result, err
	}

	return updateConditions(ctx, log, asc)
}

func (hr *HypershiftAgentServiceConfigReconciler) reconcileHubComponents(ctx context.Context, log *logrus.Entry, asc ASC) (ctrl.Result, error) {
	hubComponents := []component{}
	hubComponents = append(hubComponents, assistedServiceRBAC_hub...)
	hubComponents = append(hubComponents, getComponents(asc.spec)...)
	hubComponents = append(hubComponents, hr.getWebhookComponents_hub()...)

	// Reconcile hub components
	for _, component := range hubComponents {
		switch component.name {
		case "AssistedServiceDeployment":
			component.fn = newHypershiftAssistedServiceDeployment
		}

		log.Infof(fmt.Sprintf("Reconcile hub component: %s", component.name))
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			log.WithError(err).Errorf("Failed to reconcile hub component %s", component.name)
			return result, err
		}
	}

	// save the webhook service clusterIP
	if err := readAdmissionClusterIP(ctx, log, asc); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	//reconcile Konnectivity agent
	if result, err := ensureKonnectivityAgent(ctx, log, asc); err != nil {
		return result, err
	}

	return ctrl.Result{}, nil
}

func ensureKonnectivityAgent(ctx context.Context, log *logrus.Entry, asc ASC) (ctrl.Result, error) {
	log.Info("reconciling the assisted-service konnectivity agent")
	konnectivity := component{"KonnectivityAgent", aiv1beta1.ReasonKonnectivityAgentFailure, newKonnectivityAgentDeployment}
	if result, err := reconcileComponent(ctx, log, asc, konnectivity); err != nil {
		log.WithError(err).Errorf("Failed to reconcile konnectivity agent")
		return result, err
	}

	return ctrl.Result{}, nil
}

func readServiceCertificate(ctx context.Context, log *logrus.Entry, asc ASC) error {
	sourceCM := &corev1.ConfigMap{}
	if err := asc.Client.Get(ctx, types.NamespacedName{Name: defaultServingCertCMName, Namespace: defaultServingCertNamespace}, sourceCM); err != nil {
		log.WithError(err).Error("Failed to get default webhook serving cert config map")
		return err
	}
	asc.properties["ca"] = sourceCM.Data
	return nil
}

func readAdmissionClusterIP(ctx context.Context, log *logrus.Entry, asc ASC) error {
	svc := &corev1.Service{}
	if err := asc.Client.Get(ctx, types.NamespacedName{Name: webhookServiceName, Namespace: asc.namespace}, svc); err != nil {
		return err
	}
	asc.properties[clusterIPParam] = svc.Spec.ClusterIP
	log.Infof("saving webhook clusterIP on the hub: %s", svc.Spec.ClusterIP)
	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) reconcileSpokeComponents(ctx context.Context, log *logrus.Entry, asc ASC) (ctrl.Result, error) {
	spokeComponents := []component{}
	spokeComponents = append(spokeComponents, assistedServiceRBAC_spoke...)
	spokeComponents = append(spokeComponents, hr.getWebhookComponents_spoke()...)

	// Reconcile spoke components
	for _, component := range spokeComponents {
		log.Infof(fmt.Sprintf("Reconcile spoke component: %s", component.name))
		if result, err := reconcileComponent(ctx, log, asc, component); err != nil {
			log.WithError(err).Errorf("Failed to reconcile spoke component %s", component.name)
			return result, err
		}
	}
	return ctrl.Result{}, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) createSpokeClient(ctx context.Context, log *logrus.Entry, kubeconfigSecretName string, asc ASC) (spoke_k8s_client.SpokeK8sClient, error) {
	// Fetch kubeconfig secret by specified secret reference
	kubeconfigSecret, err := hr.getKubeconfigSecret(ctx, log, kubeconfigSecretName, asc.namespace)
	if err != nil {
		reason := aiv1beta1.ReasonKubeconfigSecretFetchFailure
		if err1 := hr.updateReconcileCondition(ctx, asc, reason, err.Error(), corev1.ConditionFalse); err1 != nil {
			return nil, err1
		}
		return nil, pkgerror.Wrapf(err, "Failed to get secret '%s' in '%s' namespace", kubeconfigSecretName, asc.namespace)
	}

	// Create spoke cluster client using kubeconfig secret
	spokeClient, err := hr.SpokeClients.Get(kubeconfigSecret)
	if err != nil {
		reason := aiv1beta1.ReasonSpokeClientCreationFailure
		msg := fmt.Sprintf("Failed to create kubeconfig client: %s", err.Error())
		if err1 := hr.updateReconcileCondition(ctx, asc, reason, msg, corev1.ConditionFalse); err1 != nil {
			return nil, err1
		}
		return nil, pkgerror.Wrapf(err, "Failed to create client using kubeconfig secret '%s'", kubeconfigSecretName)
	}

	return spokeClient, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) updateReconcileCondition(ctx context.Context, asc ASC, reason, msg string, status corev1.ConditionStatus) error {
	conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionReconcileCompleted,
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
	if err := asc.Client.Status().Update(ctx, asc.Object); err != nil {
		return pkgerror.Wrapf(err, "Failed to update status")
	}
	return nil
}

// Return kubeconfig secret by name and namespace
func (hr *HypershiftAgentServiceConfigReconciler) getKubeconfigSecret(ctx context.Context, log *logrus.Entry, kubeconfigSecretName, namespace string) (*corev1.Secret, error) {
	secretRef := types.NamespacedName{Namespace: namespace, Name: kubeconfigSecretName}
	secret, err := getSecret(ctx, hr.Client, hr, secretRef)
	if err != nil {
		msg := fmt.Sprintf("Failed to get '%s' secret in '%s' namespace (check `kubeconfigSecretRef` property)", secretRef.Name, secretRef.Namespace)
		log.WithError(err).Error(msg)
		return nil, pkgerror.Errorf(msg)
	}
	_, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, pkgerror.Errorf("Secret '%s' does not contain '%s' key value", secretRef.Name, "kubeconfig")
	}
	return secret, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) ensureSyncSpokeAgentInstallCRDs(ctx context.Context, log *logrus.Entry, spokeClient client.Client, asc ASC) error {
	if err := hr.syncSpokeAgentInstallCRDs(ctx, log, spokeClient); err != nil {
		reason := aiv1beta1.ReasonSpokeClusterCRDsSyncFailure
		msg := fmt.Sprintf("Failed to sync agent-install CRDs on spoke cluster: %s", err.Error())
		log.WithError(err).Error(msg)
		if err1 := hr.updateReconcileCondition(ctx, asc, reason, msg, corev1.ConditionFalse); err1 != nil {
			return err1
		}
		return err
	}
	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) syncSpokeAgentInstallCRDs(ctx context.Context, log *logrus.Entry, spokeClient client.Client) error {
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
		return pkgerror.Wrapf(err, "Failed to create agent-install CRDs on spoke cluster")
	}

	// Delete agent-install CRDs that don't exist locally
	if err := hr.cleanStaleSpokeAgentInstallCRDs(ctx, spokeClient, localCRDs); err != nil {
		log.WithError(err).Warn("Failed to remove stale agent-install CRDs from spoke cluster in namespace")
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

// Returns 'agent-install' CRDs by querying the specified client, and filter according their group.
// I.e. we need the CRDs created in config/crd/resources.yaml ('agent-install' and 'extensions.hive' groups).
func (hr *HypershiftAgentServiceConfigReconciler) getAgentInstallCRDs(ctx context.Context, c client.Client) (*apiextensionsv1.CustomResourceDefinitionList, error) {
	crds := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := c.List(ctx, crds); err != nil {
		return nil, pkgerror.Wrap(err, "Failed to list CRDs")
	}

	// Filter CRDs by 'agent-install.openshift.io' and 'extensions.hive.openshift.io' groups
	// (to include our agentclusterinstalls hive extension)
	agentInstallCRDs := funk.Filter(crds.Items, func(crd apiextensionsv1.CustomResourceDefinition) bool {
		return crd.Spec.Group == aiv1beta1.Group || crd.Spec.Group == hiveext.Group
	}).([]apiextensionsv1.CustomResourceDefinition)

	crds.Items = agentInstallCRDs
	return crds, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) ensureSpokeNamespace(ctx context.Context, log *logrus.Entry, asc ASC) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: asc.namespace}}
	mutate := func() error {
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, asc.Client, ns, mutate)
	if err != nil {
		log.WithError(err).Errorf("Failed to create spoke namespace %s", asc.namespace)
	} else if result != controllerutil.OperationResultNone {
		log.Infof("Namespace %s %s", asc.namespace, result)
	}

	return err
}

// SetupWithManager sets up the controller with the Manager.
func (hr *HypershiftAgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.HypershiftAgentServiceConfig{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&apiregv1.APIService{})

	if hr.IsOpenShift {
		b = b.Owns(&monitoringv1.ServiceMonitor{}).Owns(&routev1.Route{})
	}

	return b.Complete(hr)
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

func newHeadlessWebHookService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		addAppLabel(webhookServiceName, &svc.ObjectMeta)
		setAnnotation(&svc.ObjectMeta, servingCertAnnotation, webhookServiceName)
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{})
		}
		svc.Spec.Ports[0].Name = webhookServiceName
		svc.Spec.Ports[0].Port = 443
		svc.Spec.Ports[0].TargetPort = intstr.IntOrString{Type: intstr.Int, IntVal: 9443}
		svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		return nil
	}

	return svc, mutateFn, nil
}

func newWebHookEndpoint(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {

	//retrieve the admission service IP from the ASC context
	//the reconciler should fill this value before reconciling
	//the endpoint
	clusterIP := asc.properties[clusterIPParam]
	if clusterIP == "" || clusterIP == nil {
		return nil, nil, pkgerror.New("missing webhook admision service clusterIP")
	}

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: asc.namespace,
		},
	}

	endpointAddress := corev1.EndpointAddress{
		IP: clusterIP.(string),
	}

	endpointPort := corev1.EndpointPort{
		Name:     webhookServiceName,
		Port:     443,
		Protocol: corev1.ProtocolTCP,
	}

	ep.Subsets = []corev1.EndpointSubset{
		{
			Addresses: []corev1.EndpointAddress{endpointAddress},
			Ports:     []corev1.EndpointPort{endpointPort},
		},
	}

	mutateFn := func() error {
		//TODO: services and endpoints on the spoke can not be controlled by our controller
		//this is an open integration issue
		ep.Subsets = []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{endpointAddress},
				Ports:     []corev1.EndpointPort{endpointPort},
			},
		}
		return nil
	}
	return ep, mutateFn, nil
}

func newHypershiftWebHookAPIService(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	obj, _, _ := newWebHookAPIService(ctx, log, asc)
	as := obj.(*apiregv1.APIService)

	data, ok := asc.properties["ca"]
	if !ok {
		err := pkgerror.Errorf("no ca data on context %v on namespace %s", asc.properties, asc.namespace)
		log.WithError(err)
		return nil, nil, err
	}

	cmdata, ok := data.(map[string]string)
	if !ok {
		err := pkgerror.Errorf("bad format of ca context %v on namespace %s", asc.properties, asc.namespace)
		log.WithError(err)
		return nil, nil, err
	}

	ca, ok := cmdata["service-ca.crt"]
	if !ok {
		err := pkgerror.Errorf("Missing ca certificate for API service on namespace %s", asc.namespace)
		log.WithError(err)
		return nil, nil, err
	}

	mutateFn := func() error {
		baseApiServiceSpec(as, asc.namespace)
		as.Spec.CABundle = []byte(ca)
		return nil
	}
	return as, mutateFn, nil
}

func newKonnectivityAgentDeployment(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	//read the existing konnectivity deployment
	ka := &appsv1.Deployment{}
	if e := asc.Client.Get(ctx, types.NamespacedName{Name: "konnectivity-agent", Namespace: asc.namespace}, ka); e != nil {
		err := pkgerror.Wrap(e, fmt.Sprintf("Failed to retrieve konnectivity-agent Deployment from namespace %s", asc.namespace))
		log.WithError(err)
		return nil, nil, err
	}
	ka.ObjectMeta = metav1.ObjectMeta{
		Name:      "konnectivity-agent-assisted-service",
		Namespace: asc.namespace,
	}
	ka.OwnerReferences = nil

	//retrieve the admission service IP from the ASC context
	//the reconciler should fill this value before reconciling
	//the endpoint
	clusterIP := asc.properties[clusterIPParam]
	if clusterIP == "" || clusterIP == nil {
		return nil, nil, pkgerror.New("missing webhook admision service clusterIP")
	}

	//TODO: this element can not be controlled by this current controller
	//      this is an intgeration issue to be resolved with hypershift
	mutateFn := func() error {
		for i, arg := range ka.Spec.Template.Spec.Containers[0].Args {
			if strings.HasPrefix(arg, "ipv4=") {
				ka.Spec.Template.Spec.Containers[0].Args[i] = "ipv4=" + clusterIP.(string)
				break
			}
		}
		return nil
	}
	return ka, mutateFn, nil
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

func newHypershiftWebHookDeployment(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	obj, mutateFn, err := newWebHookDeployment(ctx, log, asc)
	if err != nil {
		log.WithError(err).Errorf("Failed to define Webhook Deployment scheme")
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

		// add the kubeconfig to the container command
		commands := append(make([]string, 0), deployment.Spec.Template.Spec.Containers[0].Command...)
		commands = append(commands,
			"--authorization-kubeconfig=/etc/kube/kubeconfig",
			"--authentication-kubeconfig=/etc/kube/kubeconfig",
			"--kubeconfig=/etc/kube/kubeconfig")
		deployment.Spec.Template.Spec.Containers[0].Command = commands

		return nil
	}
	return deployment, newMutateFn, nil
}
