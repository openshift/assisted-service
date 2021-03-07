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
	"time"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	AgentPlatformError     = "AgentPlatformError"
	AgentPlatformCondition = "AgentPlatformCondition"
	AgentPlatformState     = "AgentPlatformState"
	AgentPlatformStateInfo = "AgentPlatformStateInfo"
)

const HighAvailabilityModeNone = "None"
const defaultRequeueAfterOnError = 10 * time.Second

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

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch

func (r *ClusterDeploymentsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
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
	if gorm.IsRecordNotFoundError(err) {
		return r.createNewCluster(ctx, req.NamespacedName, clusterDeployment)
	}
	if err != nil {
		return r.updateState(ctx, clusterDeployment, nil, err)
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

	ready, err := r.isReadyForInstallation(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateState(ctx, clusterDeployment, cluster, err)
	}
	if ready {
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
	notifyInstallEnv := false

	params := &models.ClusterUpdateParams{}

	spec := clusterDeployment.Spec

	updateString := func(new, old string, target **string, isInstallEnvUpdate bool) {
		if new != old {
			*target = swag.String(new)
			update = true
			if isInstallEnvUpdate {
				notifyInstallEnv = true
			}
		}
	}

	updateString(spec.ClusterName, cluster.Name, &params.Name, false)
	updateString(spec.BaseDomain, cluster.BaseDNSDomain, &params.BaseDNSDomain, false)

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR, cluster.ClusterNetworkCidr, &params.ClusterNetworkCidr, false)
		hostPrefix := int64(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
		if hostPrefix > 0 && hostPrefix != cluster.ClusterNetworkHostPrefix {
			params.ClusterNetworkHostPrefix = swag.Int64(hostPrefix)
			update = true
		}
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork[0], cluster.ServiceNetworkCidr, &params.ServiceNetworkCidr, false)
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork[0].CIDR, cluster.MachineNetworkCidr, &params.MachineNetworkCidr, false)
	}

	updateString(spec.Platform.AgentBareMetal.APIVIP, cluster.APIVip, &params.APIVip, false)
	updateString(spec.Platform.AgentBareMetal.APIVIPDNSName, swag.StringValue(cluster.APIVipDNSName), &params.APIVipDNSName, false)
	updateString(spec.Platform.AgentBareMetal.IngressVIP, cluster.IngressVip, &params.IngressVip, false)
	updateString(spec.Provisioning.InstallStrategy.Agent.SSHPublicKey, cluster.SSHPublicKey, &params.SSHPublicKey, false)

	installEnv, err := getInstallEnvByClusterDeployment(ctx, r.Client, clusterDeployment)

	// It is possible that the clusterDeployment doesn't have an installEnv. ClusterDeploymentsReconciler should not fail for that reason.
	if err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, fmt.Sprintf("failed to search for installEnv for clusterDeployment %s", clusterDeployment.Name))
	}
	if installEnv != nil {
		if len(installEnv.Spec.AdditionalNTPSources) > 0 {
			updateString(installEnv.Spec.AdditionalNTPSources[0], cluster.AdditionalNtpSource, &params.AdditionalNtpSource, true)
		}
		updateString(installEnv.Spec.Proxy.NoProxy, cluster.NoProxy, &params.NoProxy, true)
		updateString(installEnv.Spec.Proxy.HTTPProxy, cluster.HTTPProxy, &params.HTTPProxy, true)
		updateString(installEnv.Spec.Proxy.HTTPSProxy, cluster.HTTPSProxy, &params.HTTPSProxy, true)
	}

	if userManagedNetwork := isUserManagedNetwork(clusterDeployment); userManagedNetwork != swag.BoolValue(cluster.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(userManagedNetwork)
	}

	// TODO: handle InstallConfigOverrides

	pullSecretData, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, clusterDeployment.Namespace)
	if err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, "failed to get pull secret for update")
	}
	// TODO: change isInstallEnvUpdate to false, once clusterDeployment pull-secret can differ from installEnv
	updateString(pullSecretData, cluster.PullSecret, &params.PullSecret, true)

	if !update {
		return update, ctrl.Result{}, nil
	}

	updatedCluster, err := r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *cluster.ID,
	})
	if err != nil && IsHTTP4XXError(err) {
		return update, ctrl.Result{}, errors.Wrap(err, "failed to update clusterDeployment")
	}
	if err != nil {
		return update, ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterOnError},
			errors.Wrap(err, "failed to update clusterDeployment")
	}

	r.Log.Infof("Updated clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	reply, err := r.updateState(ctx, clusterDeployment, updatedCluster, nil)
	if err == nil && notifyInstallEnv && installEnv != nil {
		r.Log.Infof("Notify that installEnv %s should re-generate the image for clusterDeployment %s", installEnv.Name, clusterDeployment.ClusterName)
		r.CRDEventsHandler.NotifyInstallEnvUpdates(installEnv.Name, installEnv.Namespace)
	}
	return update, reply, err
}

func (r *ClusterDeploymentsReconciler) getOCPVersion(cluster *hivev1.ClusterDeployment) string {
	// TODO: fix when HIVE-1383 is resolved, As for now single node supported only with 4.8, default version is 4.7
	if cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		return "4.8"
	}
	return "4.7"
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	notifyInstallEnv := false

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
		OpenshiftVersion:      swag.String(r.getOCPVersion(clusterDeployment)),
		OlmOperators:          nil, // TODO: handle operators
		PullSecret:            swag.String(pullSecret),
		VipDhcpAllocation:     swag.Bool(false),
		IngressVip:            spec.Platform.AgentBareMetal.IngressVIP,
		SSHPublicKey:          spec.Provisioning.InstallStrategy.Agent.SSHPublicKey,
		UserManagedNetworking: swag.Bool(isUserManagedNetwork(clusterDeployment)),
	}

	installEnv, err := getInstallEnvByClusterDeployment(ctx, r.Client, clusterDeployment)
	if err == nil && installEnv != nil {
		notifyInstallEnv = true
		if len(installEnv.Spec.AdditionalNTPSources) > 0 {
			clusterParams.AdditionalNtpSource = &installEnv.Spec.AdditionalNTPSources[0]
		}
		clusterParams.NoProxy = &installEnv.Spec.Proxy.NoProxy
		clusterParams.HTTPProxy = &installEnv.Spec.Proxy.HTTPProxy
		clusterParams.HTTPSProxy = &installEnv.Spec.Proxy.HTTPSProxy
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
	reply, updateErr := r.updateState(ctx, clusterDeployment, c, err)
	if updateErr != nil && notifyInstallEnv && installEnv != nil {
		r.Log.Infof("Notify that installEnv %s should re-generate the image for clusterDeployment %s", installEnv.Name, clusterDeployment.ClusterName)
		r.CRDEventsHandler.NotifyInstallEnvUpdates(installEnv.Name, installEnv.Namespace)
	}
	return reply, updateErr
}

func (r *ClusterDeploymentsReconciler) updateState(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster,
	err error) (ctrl.Result, error) {

	reply := ctrl.Result{}
	if cluster != nil {
		r.syncClusterState(clusterDeployment, cluster)
	}

	if err != nil {
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

	if gorm.IsRecordNotFoundError(err) {
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
	mapSecretToClusterDeployment := handler.ToRequestsFunc(
		func(a handler.MapObject) []reconcile.Request {
			clusterDeployments := &hivev1.ClusterDeploymentList{}
			if err := r.List(context.Background(), clusterDeployments); err != nil {
				return []reconcile.Request{}
			}
			reply := make([]reconcile.Request, 0, len(clusterDeployments.Items))
			for _, clusterDeployment := range clusterDeployments.Items {
				if clusterDeployment.Spec.PullSecretRef.Name == a.Meta.GetName() {
					reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: clusterDeployment.Namespace,
						Name:      clusterDeployment.Name,
					}})
				}
			}
			return reply
		})

	clusterDeploymentUpdates := r.CRDEventsHandler.GetClusterDeploymentUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapSecretToClusterDeployment}).
		Watches(&source.Channel{Source: clusterDeploymentUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
