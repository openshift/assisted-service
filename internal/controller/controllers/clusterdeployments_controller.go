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
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/openshift/hive/pkg/apis/hive/v1/agent"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	AgentPlatformError     = "AgentPlatformError"
	AgentPlatformCondition = "AgentPlatformCondition"
	AgentPlatformState     = "AgentPlatformState"
	AgentPlatformStateInfo = "AgnetPlatformStateInfo"
)

// ClusterDeploymentsReconciler reconciles a Cluster object
type ClusterDeploymentsReconciler struct {
	client.Client
	Log                      logrus.FieldLogger
	Scheme                   *runtime.Scheme
	Installer                bminventory.InstallerInternals
	ClusterApi               cluster.API
	HostApi                  host.API
	PullSecretUpdatesChannel chan event.GenericEvent
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch

func (r *ClusterDeploymentsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	cluster := &hivev1.ClusterDeployment{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return r.deregisterClusterIfNeeded(ctx, req.NamespacedName)
		}
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{Requeue: true}, nil
	}

	// ignore unsupported platforms
	if !isSupportedPlatform(cluster) {
		return ctrl.Result{}, nil
	}

	c, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
	if gorm.IsRecordNotFoundError(err) {
		return r.createNewCluster(ctx, req.NamespacedName, cluster)
	}
	if err != nil {
		return r.updateState(ctx, cluster, nil, err)
	}

	return r.updateState(ctx, cluster, c, nil)
}

func isSupportedPlatform(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.Platform.AgentBareMetal == nil ||
		cluster.Spec.InstallStrategy == nil ||
		cluster.Spec.InstallStrategy.Agent == nil {
		return false
	}
	return true
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	cluster *hivev1.ClusterDeployment) (ctrl.Result, error) {

	r.Log.Infof("Creating a new cluster %s %s", cluster.Name, cluster.Namespace)
	spec := cluster.Spec

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return ctrl.Result{}, nil
	}

	clusterParams := &models.ClusterCreateParams{
		//AdditionalNtpSource:      swag.String(spec.AdditionalNtpSource), // TODO: get from AgentEnvSpec
		//HTTPProxy:                swag.String(spec.HTTPProxy), // TODO: get from AgentEnvSpec
		//HTTPSProxy:               swag.String(spec.HTTPSProxy), // TODO: get from AgentEnvSpec
		//NoProxy:                  swag.String(spec.NoProxy), // TODO: get from AgentEnvSpec
		//SSHPublicKey:          spec.SSHPublicKey, // TODO: get from AgentEnvSpec
		BaseDNSDomain:     spec.BaseDomain,
		Name:              swag.String(spec.ClusterName),
		OpenshiftVersion:  swag.String("4.7"), // TODO: check how to set openshift version
		Operators:         nil,                // TODO: handle operators
		PullSecret:        swag.String(pullSecret),
		VipDhcpAllocation: swag.Bool(spec.Platform.AgentBareMetal.VIPDHCPAllocation),
		IngressVip:        spec.Platform.AgentBareMetal.IngressVIP,
		SSHPublicKey:      spec.InstallStrategy.Agent.SSHPublicKey,
	}

	if len(spec.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		clusterParams.ClusterNetworkCidr = swag.String(spec.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR)
		clusterParams.ClusterNetworkHostPrefix = int64(spec.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
	}

	if len(spec.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		clusterParams.ServiceNetworkCidr = swag.String(spec.InstallStrategy.Agent.Networking.ServiceNetwork[0])
	}

	if cluster.Spec.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		clusterParams.UserManagedNetworking = swag.Bool(true)
	}

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: clusterParams,
	})

	// TODO: handle specific errors, 5XX retry, 4XX update status with the error
	return r.updateState(ctx, cluster, c, err)
}

func (r *ClusterDeploymentsReconciler) updateState(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster,
	err error) (ctrl.Result, error) {

	reply := ctrl.Result{}
	if c != nil {
		r.syncClusterState(cluster, c)
	}

	if err != nil {
		setClusterApiError(err, cluster)
		reply.RequeueAfter = defaultRequeueAfterOnError
	}

	if err := r.Status().Update(ctx, cluster); err != nil {
		r.Log.WithError(err).Errorf("failed set state for %s %s", cluster.Name, cluster.Namespace)
		return ctrl.Result{Requeue: true}, nil
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

func getAgentRefsFromIDs(uuidArr []strfmt.UUID) []agent.AgentRef {
	agentsRef := make([]agent.AgentRef, len(uuidArr))
	for i := range uuidArr {
		agentsRef[i] = agent.AgentRef{
			Namespace: "",                  // TODO: get namespace once agent CRD is implemented
			Name:      uuidArr[i].String(), // TODO: get name once agent CRD is implemented
		}
	}
	return agentsRef
}

func (r *ClusterDeploymentsReconciler) syncClusterState(cluster *hivev1.ClusterDeployment, c *common.Cluster) {
	if cluster.Status.Conditions == nil {
		cluster.Status.Conditions = []hivev1.ClusterDeploymentCondition{}
	}

	setStateAndStateInfo(cluster, c)

	cluster.Status.InstallStrategy = &hivev1.InstallStrategyStatus{Agent: &agent.InstallStrategyStatus{}}
	if len(c.HostNetworks) > 0 {
		cluster.Status.InstallStrategy.Agent.AgentNetworks = make([]agent.AgentNetwork, len(c.HostNetworks))
		for i, hn := range c.HostNetworks {
			cluster.Status.InstallStrategy.Agent.AgentNetworks[i] = agent.AgentNetwork{
				CIDR:      hn.Cidr,
				AgentRefs: getAgentRefsFromIDs(c.HostNetworks[i].HostIds),
			}
		}
	}
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Complete(r)
}
