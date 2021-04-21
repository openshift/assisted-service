/*
Copyright 2020.

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
	"io/ioutil"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	AgentPlatformError                = "AgentPlatformError"
	AgentPlatformCondition            = "AgentPlatformCondition"
	AgentPlatformState                = "AgentPlatformState"
	AgentPlatformStateInfo            = "AgentPlatformStateInfo"
	adminPasswordSecretStringTemplate = "%s-admin-password"
	adminKubeConfigStringTemplate     = "%s-admin-kubeconfig"
	InstallConfigOverrides            = v1beta1.Group + "/install-config-overrides"
	ProvisionFailedReason             = "ProvisionFailed"
	InstallAttemptsLimitReachedReason = "InstallAttemptsLimitReached"
)

const HighAvailabilityModeNone = "None"
const defaultRequeueAfterOnError = 10 * time.Second
const defaultOCPVersion = "4.8" // TODO: remove after MGMT-4491 is resoled

var installationStatuses = []string{
	models.ClusterStatusPreparingForInstallation,
	models.ClusterStatusInstallingPendingUserAction,
	models.ClusterStatusInstalling,
	models.ClusterStatusFinalizing,
}

// ClusterDeploymentsReconciler reconciles a Cluster object
type ClusterDeploymentsReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	Scheme           *runtime.Scheme
	Installer        bminventory.InstallerInternals
	ClusterApi       cluster.API
	HostApi          host.API
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;create
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch

func (r *ClusterDeploymentsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	clusterDeployment := &hivev1.ClusterDeployment{}
	err := r.Get(ctx, req.NamespacedName, clusterDeployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return r.deregisterClusterIfNeeded(ctx, req.NamespacedName)
		}
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{Requeue: true}, nil
	}

	// ignore unsupported platforms
	if !isSupportedPlatform(clusterDeployment) {
		return ctrl.Result{}, nil
	}

	cluster, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !clusterDeployment.Spec.Installed {
			return r.createNewCluster(ctx, req.NamespacedName, clusterDeployment)
		}
		if !r.isSNO(clusterDeployment) {
			return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment)
		}
	}
	if err != nil {
		return r.updateState(ctx, clusterDeployment, nil, err)
	}

	if hasFailedToProvision(clusterDeployment, cluster) {
		setProvisionFailureError(clusterDeployment, cluster)
	}

	var updated bool
	var result ctrl.Result
	// check for updates from user, compare spec and update if needed
	updated, result, err = r.updateIfNeeded(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateState(ctx, clusterDeployment, cluster, err)
	}

	if updated {
		return result, err
	}

	// check for install config overrides and update if needed
	updated, result, err = r.updateInstallConfigOverrides(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateState(ctx, clusterDeployment, cluster, err)
	}

	if updated {
		return result, err
	}

	// In case the Cluster is a Day 1 cluster and is installed, update the Metadata and create secrets for credentials
	if *cluster.Status == models.ClusterStatusInstalled && swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		if !clusterDeployment.Spec.Installed {
			err = r.updateClusterMetadata(ctx, clusterDeployment, cluster)
			if err != nil {
				return r.updateState(ctx, clusterDeployment, cluster, err)
			}
		}
		// Delete Day1 Cluster
		err = r.Installer.DeregisterClusterInternal(ctx, installer.DeregisterClusterParams{
			ClusterID: *cluster.ID,
		})
		if err != nil {
			return r.updateState(ctx, clusterDeployment, cluster, err)
		}
		if !r.isSNO(clusterDeployment) {
			//Create Day2 cluster
			return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment)
		}
		return r.updateState(ctx, clusterDeployment, cluster, nil)
	}

	if swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		// Day 1
		return r.installDay1(ctx, clusterDeployment, cluster)

	} else if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		// Day 2
		return r.installDay2Hosts(ctx, clusterDeployment, cluster)
	}

	return r.updateState(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) installDay1(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) (ctrl.Result, error) {
	ready, err := r.isReadyForInstallation(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateState(ctx, clusterDeployment, cluster, err)
	}
	if ready {
		r.Log.Infof("Installing clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
		var ic *common.Cluster
		ic, err = r.Installer.InstallClusterInternal(ctx, installer.InstallClusterParams{
			ClusterID: *cluster.ID,
		})
		if err != nil {
			return r.updateState(ctx, clusterDeployment, cluster, err)
		}
		return r.updateState(ctx, clusterDeployment, ic, nil)
	}
	return r.updateState(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) installDay2Hosts(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) (ctrl.Result, error) {

	for _, h := range cluster.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, cluster.ID.String(), h.ID.String())
		if err != nil {
			return r.updateState(ctx, clusterDeployment, cluster, err)
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			r.Log.Infof("Installing Day2 host %s in %s %s", *h.ID, clusterDeployment.Name, clusterDeployment.Namespace)
			err = r.Installer.InstallSingleDay2HostInternal(ctx, *cluster.ID, *h.ID)
			if err != nil {
				return r.updateState(ctx, clusterDeployment, cluster, err)
			}
		}
	}
	return r.updateState(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) updateClusterMetadata(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) error {

	s, err := r.ensureAdminPasswordSecret(ctx, cluster, c)
	if err != nil {
		return err
	}
	k, err := r.ensureKubeConfigSecret(ctx, cluster, c)
	if err != nil {
		return err
	}
	cluster.Spec.Installed = true
	cluster.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		ClusterID: c.OpenshiftClusterID.String(),
		InfraID:   c.ID.String(),
		AdminPasswordSecretRef: corev1.LocalObjectReference{
			Name: s.Name,
		},
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: k.Name,
		},
	}
	return r.Update(ctx, cluster)
}

func (r *ClusterDeploymentsReconciler) ensureAdminPasswordSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminPasswordSecretStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	cred, err := r.Installer.GetCredentialsInternal(ctx, installer.GetCredentialsParams{
		ClusterID: *c.ID,
	})
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"username": []byte(cred.Username),
		"password": []byte(cred.Password),
	}
	return r.createClusterCredentialSecret(ctx, cluster, c, name, data, "kubeadmincreds")
}

func (r *ClusterDeploymentsReconciler) ensureKubeConfigSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	respBody, _, err := r.Installer.DownloadClusterKubeconfigInternal(ctx, installer.DownloadClusterKubeconfigParams{
		ClusterID: *c.ID,
	})
	if err != nil {
		return nil, err
	}
	respBytes, err := ioutil.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"kubeconfig": respBytes,
	}

	return r.createClusterCredentialSecret(ctx, cluster, c, name, data, "kubeconfig")
}

func (r *ClusterDeploymentsReconciler) createClusterCredentialSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster, name string, data map[string][]byte, secretType string) (*corev1.Secret, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
		},
		Data: data,
	}

	s.Labels = AddLabel(s.Labels, "hive.openshift.io/cluster-deployment-name", cluster.Name)
	s.Labels = AddLabel(s.Labels, "hive.openshift.io/secret-type", secretType)

	deploymentGVK, err := apiutil.GVKForObject(cluster, r.Scheme)
	if err != nil {
		r.Log.WithError(err).Errorf("error getting GVK for clusterdeployment")
		return nil, err
	}

	s.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         deploymentGVK.GroupVersion().String(),
		Kind:               deploymentGVK.Kind,
		Name:               cluster.Name,
		UID:                cluster.UID,
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}}
	return s, r.Create(ctx, s)
}

func (r *ClusterDeploymentsReconciler) isReadyForInstallation(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (bool, error) {
	if ready, _ := r.ClusterApi.IsReadyForInstallation(c); !ready {
		return false, nil
	}

	readyHosts := 0
	for _, h := range c.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, c.ID.String(), h.ID.String())
		if err != nil {
			return false, err
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			readyHosts += 1
		}
	}

	expectedHosts := cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents +
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents
	return readyHosts == expectedHosts, nil
}

func isSupportedPlatform(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.Platform.AgentBareMetal == nil ||
		cluster.Spec.Provisioning.InstallStrategy == nil ||
		cluster.Spec.Provisioning.InstallStrategy.Agent == nil {
		return false
	}
	return true
}

func isUserManagedNetwork(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		return true
	}
	return false
}

func (r *ClusterDeploymentsReconciler) updateIfNeeded(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment,
	cluster *common.Cluster) (bool, ctrl.Result, error) {

	update := false
	notifyInfraEnv := false
	var infraEnv *v1beta1.InfraEnv

	params := &models.ClusterUpdateParams{}

	spec := clusterDeployment.Spec
	updateString := func(new, old string, target **string) {
		if new != old {
			*target = swag.String(new)
			update = true
		}
	}

	updateString(spec.ClusterName, cluster.Name, &params.Name)
	updateString(spec.BaseDomain, cluster.BaseDNSDomain, &params.BaseDNSDomain)

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR, cluster.ClusterNetworkCidr, &params.ClusterNetworkCidr)
		hostPrefix := int64(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
		if hostPrefix > 0 && hostPrefix != cluster.ClusterNetworkHostPrefix {
			params.ClusterNetworkHostPrefix = swag.Int64(hostPrefix)
			update = true
		}
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork[0], cluster.ServiceNetworkCidr, &params.ServiceNetworkCidr)
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork[0].CIDR, cluster.MachineNetworkCidr, &params.MachineNetworkCidr)
	}

	updateString(spec.Platform.AgentBareMetal.APIVIP, cluster.APIVip, &params.APIVip)
	updateString(spec.Platform.AgentBareMetal.IngressVIP, cluster.IngressVip, &params.IngressVip)
	updateString(spec.Provisioning.InstallStrategy.Agent.SSHPublicKey, cluster.SSHPublicKey, &params.SSHPublicKey)

	if userManagedNetwork := isUserManagedNetwork(clusterDeployment); userManagedNetwork != swag.BoolValue(cluster.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(userManagedNetwork)
	}
	pullSecretData, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, clusterDeployment.Namespace)
	if err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, "failed to get pull secret for update")
	}
	// TODO: change isInfraEnvUpdate to false, once clusterDeployment pull-secret can differ from infraEnv
	if pullSecretData != cluster.PullSecret {
		params.PullSecret = swag.String(pullSecretData)
		update = true
		notifyInfraEnv = true
	}
	if !update {
		return update, ctrl.Result{}, nil
	}
	var updatedCluster *common.Cluster
	if update {
		updatedCluster, err = r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
			ClusterUpdateParams: params,
			ClusterID:           *cluster.ID,
		})
		if err != nil {
			return handleUpdateError(err)
		}
	}

	infraEnv, err = getInfraEnvByClusterDeployment(ctx, r.Client, clusterDeployment)
	if err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, fmt.Sprintf("failed to search for infraEnv for clusterDeployment %s", clusterDeployment.Name))
	}

	r.Log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
	reply, err := r.updateState(ctx, clusterDeployment, updatedCluster, nil)
	if err == nil && notifyInfraEnv && infraEnv != nil {
		r.Log.Infof("Notify that infraEnv %s should re-generate the image for clusterDeployment %s", infraEnv.Name, clusterDeployment.ClusterName)
		r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	}
	return update, reply, err
}
func (r *ClusterDeploymentsReconciler) updateInstallConfigOverrides(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment,
	cluster *common.Cluster) (bool, ctrl.Result, error) {
	// handle InstallConfigOverrides
	update := false
	annotations := clusterDeployment.ObjectMeta.GetAnnotations()
	installConfigOverrides := annotations[InstallConfigOverrides]
	if cluster.InstallConfigOverrides != installConfigOverrides {
		cluster.InstallConfigOverrides = installConfigOverrides
		update = true
	}
	var updatedCluster *common.Cluster
	var err error
	if update {
		updatedCluster, err = r.Installer.UpdateClusterInstallConfigInternal(ctx, installer.UpdateClusterInstallConfigParams{
			ClusterID:           *cluster.ID,
			InstallConfigParams: cluster.InstallConfigOverrides,
		})
		if err != nil {
			return handleUpdateError(err)
		}
		r.Log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
		reply, err := r.updateState(ctx, clusterDeployment, updatedCluster, nil)
		return update, reply, err
	}
	return update, ctrl.Result{}, nil
}

func (r *ClusterDeploymentsReconciler) isSNO(cluster *hivev1.ClusterDeployment) bool {
	return cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	r.Log.Infof("Creating a new clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return ctrl.Result{}, nil
	}

	clusterParams := &models.ClusterCreateParams{
		BaseDNSDomain:         spec.BaseDomain,
		Name:                  swag.String(spec.ClusterName),
		OpenshiftVersion:      swag.String(defaultOCPVersion),
		OlmOperators:          nil, // TODO: handle operators
		PullSecret:            swag.String(pullSecret),
		VipDhcpAllocation:     swag.Bool(false),
		IngressVip:            spec.Platform.AgentBareMetal.IngressVIP,
		SSHPublicKey:          spec.Provisioning.InstallStrategy.Agent.SSHPublicKey,
		UserManagedNetworking: swag.Bool(isUserManagedNetwork(clusterDeployment)),
	}

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		clusterParams.ClusterNetworkCidr = swag.String(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR)
		clusterParams.ClusterNetworkHostPrefix = int64(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
	}

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		clusterParams.ServiceNetworkCidr = swag.String(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork[0])
	}

	if spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		clusterParams.HighAvailabilityMode = swag.String(HighAvailabilityModeNone)
	}

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: clusterParams,
	})

	// TODO: handle specific errors, 5XX retry, 4XX update status with the error
	return r.updateState(ctx, clusterDeployment, c, err)
}

func (r *ClusterDeploymentsReconciler) createNewDay2Cluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	r.Log.Infof("Creating a new Day2 Cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec
	id := strfmt.UUID(uuid.New().String())
	apiVipDnsname := fmt.Sprintf("api.%s.%s", spec.ClusterName, spec.BaseDomain)
	clusterParams := &models.AddHostsClusterCreateParams{
		APIVipDnsname:    swag.String(apiVipDnsname),
		Name:             swag.String(spec.ClusterName),
		OpenshiftVersion: swag.String(defaultOCPVersion),
		ID:               &id,
	}

	c, err := r.Installer.RegisterAddHostsClusterInternal(ctx, &key, installer.RegisterAddHostsClusterParams{
		NewAddHostsClusterParams: clusterParams,
	})

	// TODO: handle specific errors, 5XX retry, 4XX update status with the error
	return r.updateState(ctx, clusterDeployment, c, err)
}

func (r *ClusterDeploymentsReconciler) updateState(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster,
	err error) (ctrl.Result, error) {

	reply := ctrl.Result{}
	if cluster != nil {
		r.syncClusterState(clusterDeployment, cluster)
	}

	if err != nil {
		r.Log.WithError(err).Errorf("Updating state with error for %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
		setClusterApiError(err, clusterDeployment)
		reply.RequeueAfter = defaultRequeueAfterOnError
	}

	if err = r.Status().Update(ctx, clusterDeployment); err != nil {
		r.Log.WithError(err).Errorf("failed set state for %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
		return ctrl.Result{Requeue: true}, err
	}
	return reply, nil
}

func setClusterApiError(err error, cluster *hivev1.ClusterDeployment) {
	if err != nil {
		errorCondition := hivev1.ClusterDeploymentCondition{
			Type:               hivev1.UnreachableCondition,
			Status:             corev1.ConditionFalse,
			LastProbeTime:      metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
			Reason:             AgentPlatformError,
			Message:            err.Error(),
		}
		setCondition(errorCondition, &cluster.Status.Conditions)
	} else {
		if index := findConditionIndexByReason(AgentPlatformError, &cluster.Status.Conditions); index >= 0 {
			cluster.Status.Conditions = append(cluster.Status.Conditions[:index],
				cluster.Status.Conditions[index+1:]...)
		}
	}
}

func (r *ClusterDeploymentsReconciler) syncClusterState(cluster *hivev1.ClusterDeployment, c *common.Cluster) {
	if cluster.Status.Conditions == nil {
		cluster.Status.Conditions = []hivev1.ClusterDeploymentCondition{}
	}

	setStateAndStateInfo(cluster, c)

	cluster.Status.InstallStrategy = &hivev1.InstallStrategyStatus{Agent: &agent.InstallStrategyStatus{
		ConnectivityMajorityGroups: c.ConnectivityMajorityGroups,
	}}
	cluster.Status.InstallStrategy.Agent.ConnectivityMajorityGroups = c.ConnectivityMajorityGroups

	// TODO: count hosts in specific states
	//cluster.Status.InstallStrategy.Agent...

	setValidations(cluster, c)
}

func setStateAndStateInfo(cluster *hivev1.ClusterDeployment, c *common.Cluster) {
	// TODO: find proper way to set state and state info
	setCondition(hivev1.ClusterDeploymentCondition{
		Type:               hivev1.UnreachableCondition,
		Status:             corev1.ConditionUnknown,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             AgentPlatformState,
		Message:            swag.StringValue(c.Status),
	}, &cluster.Status.Conditions)
	setCondition(hivev1.ClusterDeploymentCondition{
		Type:               hivev1.UnreachableCondition,
		Status:             corev1.ConditionUnknown,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             AgentPlatformStateInfo,
		Message:            swag.StringValue(c.StatusInfo),
	}, &cluster.Status.Conditions)
}

func findConditionIndexByReason(reason string, conditions *[]hivev1.ClusterDeploymentCondition) int {
	if conditions == nil {
		return -1
	}
	for cIndex, c := range *conditions {
		if c.Reason == reason {
			return cIndex
		}
	}
	return -1
}

// Set existing or append condition by reason
func setCondition(condition hivev1.ClusterDeploymentCondition, conditions *[]hivev1.ClusterDeploymentCondition) {
	if index := findConditionIndexByReason(condition.Reason, conditions); index >= 0 {
		(*conditions)[index] = condition
	} else {
		*conditions = append(*conditions, condition)
	}
}

func setConditionIfNotExists(condition hivev1.ClusterDeploymentCondition, conditions *[]hivev1.ClusterDeploymentCondition) {
	if index := findConditionIndexByReason(condition.Reason, conditions); index >= 0 {
		return
	}
	*conditions = append(*conditions, condition)
}

func setValidations(cluster *hivev1.ClusterDeployment, c *common.Cluster) {
	// TODO: translate validations into conditions, currently put all the validations as string to unknown condition.
	validations := hivev1.ClusterDeploymentCondition{
		Type:               hivev1.UnreachableCondition,
		Status:             corev1.ConditionUnknown,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             AgentPlatformCondition,
		Message:            c.ValidationsInfo,
	}
	setCondition(validations, &cluster.Status.Conditions)
}

func (r *ClusterDeploymentsReconciler) deregisterClusterIfNeeded(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "failed to deregister cluster: %s", key.Name)
		r.Log.Error(err)
		return reply, err
	}

	c, err := r.Installer.GetClusterByKubeKey(key)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return if from any reason cluster is already deleted from db (or never existed)
		return buildReply(nil)
	}

	if err != nil {
		return buildReply(err)
	}

	if err = r.Installer.DeregisterClusterInternal(ctx, installer.DeregisterClusterParams{
		ClusterID: *c.ID,
	}); err != nil {
		return buildReply(err)
	}

	r.Log.Infof("Cluster resource deleted, Unregistered cluster: %s", c.ID.String())

	return buildReply(nil)
}

func (r *ClusterDeploymentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapSecretToClusterDeployment := func(a client.Object) []reconcile.Request {
		clusterDeployments := &hivev1.ClusterDeploymentList{}
		if err := r.List(context.Background(), clusterDeployments); err != nil {
			return []reconcile.Request{}
		}
		reply := make([]reconcile.Request, 0, len(clusterDeployments.Items))
		for _, clusterDeployment := range clusterDeployments.Items {
			if clusterDeployment.Spec.PullSecretRef.Name == a.GetName() {
				reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: clusterDeployment.Namespace,
					Name:      clusterDeployment.Name,
				}})
			}
		}
		return reply
	}

	clusterDeploymentUpdates := r.CRDEventsHandler.GetClusterDeploymentUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(mapSecretToClusterDeployment)).
		Watches(&source.Channel{Source: clusterDeploymentUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

func handleUpdateError(err error) (bool, ctrl.Result, error) {
	if IsHTTP4XXError(err) {
		return true, ctrl.Result{}, errors.Wrap(err, "failed to update clusterDeployment")
	}
	return true, ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterOnError},
		errors.Wrap(err, "failed to update clusterDeployment")
}

func hasFailedToProvision(clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) bool {
	conditions := clusterDeployment.Status.Conditions
	conditionIdx := findConditionIndexByReason(AgentPlatformState, &conditions)
	if conditionIdx == -1 {
		return false
	} else if !funk.Contains(installationStatuses, conditions[conditionIdx].Message) {
		return false
	}
	return swag.StringValue(cluster.Status) == models.ClusterStatusError
}

func setProvisionFailureError(clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) {
	// TODO: find proper way to set state and state info
	setConditionIfNotExists(hivev1.ClusterDeploymentCondition{
		Type:               hivev1.ProvisionFailedCondition,
		Status:             corev1.ConditionTrue,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             ProvisionFailedReason,
		Message:            swag.StringValue(cluster.StatusInfo),
	}, &clusterDeployment.Status.Conditions)
	setConditionIfNotExists(hivev1.ClusterDeploymentCondition{
		Type:               hivev1.ProvisionStoppedCondition,
		Status:             corev1.ConditionTrue,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             InstallAttemptsLimitReachedReason,
		Message:            "Install attempts limit reached (limit: 1)",
	}, &clusterDeployment.Status.Conditions)
}
