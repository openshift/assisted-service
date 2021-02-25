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
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	Log       logrus.FieldLogger
	Scheme    *runtime.Scheme
	Installer bminventory.InstallerInternals
}

// +kubebuilder:rbac:groups=adi.io.my.domain,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=agents/status,verbs=get;update;patch

func (r *AgentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	agent := &adiiov1alpha1.Agent{}
	var Requeue bool
	var inventoryErr error

	err := r.Get(ctx, req.NamespacedName, agent)
	if err != nil {
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if agent.Spec.ClusterDeploymentName == nil {
		r.Log.Debugf("ClusterDeploymentName not set in Agent %s. Skipping Reconcile", agent.Name)
		return ctrl.Result{Requeue: false}, nil
	}
	kubeKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}

	// Retrieve clusterDeployment
	if err = r.Get(ctx, kubeKey, clusterDeployment); err != nil {
		errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
			agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
		Requeue = false
		clientError := true
		if !k8serrors.IsNotFound(err) {
			Requeue = true
			clientError = false
		}
		clusterDeploymentRefErr := newKubeAPIError(errors.Wrapf(err, errMsg), clientError)
		// Update that we failed to retrieve the clusterDeployment
		r.updateFailure(ctx, agent, clusterDeploymentRefErr)
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Retrieve cluster for ClusterDeploymentName from the database
	cluster, err := r.Installer.GetClusterByKubeKey(kubeKey)
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			Requeue = true
			inventoryErr = common.NewApiError(http.StatusNotFound, err)
		} else {
			Requeue = false
			inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
		}
		// Update that we failed to retrieve the cluster from the database
		r.updateFailure(ctx, agent, inventoryErr)
		return ctrl.Result{Requeue: Requeue}, nil
	}

	var result ctrl.Result
	// check for updates from user, compare spec and update if needed
	result, err = r.updateIfNeeded(ctx, agent, cluster)
	if err != nil {
		r.updateFailure(ctx, agent, err)
		return result, nil
	}

	conditionsv1.SetStatusCondition(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    adiiov1alpha1.AgentSyncedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  adiiov1alpha1.AgentSyncedReason,
		Message: adiiov1alpha1.AgentStateSynced,
	})
	if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update agent status")
		return ctrl.Result{Requeue: true}, nil
	}
	return result, nil
}

func (r *AgentReconciler) updateIfNeeded(ctx context.Context, agent *adiiov1alpha1.Agent, c *common.Cluster) (ctrl.Result, error) {
	spec := agent.Spec
	host := getHostFromCluster(c, agent.Name)
	if host == nil {
		r.Log.Errorf("Host %s not found in cluster %s", agent.Name, c.Name)
		return ctrl.Result{}, errors.New("Host not found in cluster")
	}

	clusterUpdate := false
	params := &models.ClusterUpdateParams{}
	if spec.Hostname != "" && spec.Hostname != host.RequestedHostname {
		clusterUpdate = true
		params.HostsNames = []*models.ClusterUpdateParamsHostsNamesItems0{
			{
				Hostname: spec.Hostname,
				ID:       strfmt.UUID(agent.Name),
			},
		}
	}

	if spec.MachineConfigPool != "" && spec.MachineConfigPool != host.MachineConfigPoolName {
		clusterUpdate = true
		params.HostsMachineConfigPoolNames = []*models.ClusterUpdateParamsHostsMachineConfigPoolNamesItems0{
			{
				MachineConfigPoolName: spec.MachineConfigPool,
				ID:                    strfmt.UUID(agent.Name),
			},
		}
	}

	if spec.Role != "" && spec.Role != host.Role {
		clusterUpdate = true
		params.HostsRoles = []*models.ClusterUpdateParamsHostsRolesItems0{
			{
				Role: models.HostRoleUpdateParams(spec.Role),
				ID:   strfmt.UUID(agent.Name),
			},
		}
	}

	if !clusterUpdate {
		return ctrl.Result{}, nil
	}

	_, err := r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *c.ID,
	})
	if err != nil && IsHTTP4XXError(err) {
		return ctrl.Result{}, err
	}
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterOnError}, err
	}

	r.Log.Infof("Updated Agent spec %s %s", agent.Name, agent.Namespace)

	return ctrl.Result{}, nil
}

func (r *AgentReconciler) updateFailure(ctx context.Context, agent *adiiov1alpha1.Agent, err error) {
	conditionsv1.SetStatusCondition(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    adiiov1alpha1.AgentSyncedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  adiiov1alpha1.AgentSyncErrorReason,
		Message: adiiov1alpha1.AgentStateFailedToSync + ": " + err.Error(),
	})
	if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update agent status")
	}
}

func getHostFromCluster(c *common.Cluster, agentId string) *models.Host {
	var host *models.Host
	for _, h := range c.Hosts {
		if (*h.ID).String() == agentId {
			host = h
			break
		}
	}
	return host
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.Agent{}).
		Complete(r)
}
