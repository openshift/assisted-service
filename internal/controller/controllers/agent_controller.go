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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	Scheme           *runtime.Scheme
	Installer        bminventory.InstallerInternals
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch

func (r *AgentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	agent := &aiv1beta1.Agent{}
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

	err = r.updateInventory(cluster, agent)
	if err != nil {
		r.updateFailure(ctx, agent, err)
		return ctrl.Result{Requeue: true}, nil
	}

	conditionsv1.SetStatusCondition(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.AgentSyncedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.AgentSyncedReason,
		Message: aiv1beta1.AgentStateSynced,
	})
	if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update agent status")
		return ctrl.Result{Requeue: true}, nil
	}
	return result, nil
}

func (r *AgentReconciler) updateInventory(c *common.Cluster, agent *aiv1beta1.Agent) error {

	host := getHostFromCluster(c, agent.Name)
	if host == nil {
		r.Log.Errorf("Fail to update inventory: Host %s not found in cluster %s", agent.Name, c.Name)
		return errors.New("Fail to update inventory: Host not found in cluster")
	}
	if host.Inventory == "" {
		r.Log.Debugf("Skip update inventory: Host %s cluster %s inventory not set", agent.Name, c.Name)
		return nil
	}
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return err
	}
	agent.Status.Inventory.Hostname = inventory.Hostname
	agent.Status.Inventory.BmcAddress = inventory.BmcAddress
	agent.Status.Inventory.BmcV6address = inventory.BmcV6address
	if inventory.Memory != nil {
		agent.Status.Inventory.Memory = aiv1beta1.HostMemory{
			PhysicalBytes: inventory.Memory.PhysicalBytes,
			UsableBytes:   inventory.Memory.UsableBytes,
		}
	}
	if inventory.CPU != nil {
		agent.Status.Inventory.Cpu = aiv1beta1.HostCPU{
			Count:          inventory.CPU.Count,
			ClockMegahertz: int64(inventory.CPU.Frequency),
			Flags:          inventory.CPU.Flags,
			ModelName:      inventory.CPU.ModelName,
			Architecture:   inventory.CPU.Architecture,
		}
	}
	if inventory.Boot != nil {
		agent.Status.Inventory.Boot = aiv1beta1.HostBoot{
			CurrentBootMode: inventory.Boot.CurrentBootMode,
			PxeInterface:    inventory.Boot.PxeInterface,
		}
	}
	if inventory.SystemVendor != nil {
		agent.Status.Inventory.SystemVendor = aiv1beta1.HostSystemVendor{
			SerialNumber: inventory.SystemVendor.SerialNumber,
			ProductName:  inventory.SystemVendor.ProductName,
			Manufacturer: inventory.SystemVendor.Manufacturer,
			Virtual:      inventory.SystemVendor.Virtual,
		}
	}
	if inventory.Interfaces != nil {
		ifcs := make([]aiv1beta1.HostInterface, len(inventory.Interfaces))
		agent.Status.Inventory.Interfaces = ifcs
		for i, inf := range inventory.Interfaces {
			if inf.IPV6Addresses != nil {
				ifcs[i].IPV6Addresses = inf.IPV6Addresses
			} else {
				ifcs[i].IPV6Addresses = make([]string, 0)
			}
			if inf.IPV4Addresses != nil {
				ifcs[i].IPV4Addresses = inf.IPV4Addresses
			} else {
				ifcs[i].IPV4Addresses = make([]string, 0)
			}
			if inf.Flags != nil {
				ifcs[i].Flags = inf.Flags
			} else {
				ifcs[i].Flags = make([]string, 0)
			}
			ifcs[i].Vendor = inf.Vendor
			ifcs[i].Name = inf.Name
			ifcs[i].HasCarrier = inf.HasCarrier
			ifcs[i].Product = inf.Product
			ifcs[i].Mtu = inf.Mtu
			ifcs[i].Biosdevname = inf.Biosdevname
			ifcs[i].ClientId = inf.ClientID
			ifcs[i].MacAddress = inf.MacAddress
			ifcs[i].SpeedMbps = inf.SpeedMbps
		}
	}
	if inventory.Disks != nil {
		disks := make([]aiv1beta1.HostDisk, len(inventory.Disks))
		agent.Status.Inventory.Disks = disks
		for i, d := range inventory.Disks {
			disks[i].ID = d.ID
			disks[i].ByID = d.ByID
			disks[i].DriveType = d.DriveType
			disks[i].Vendor = d.Vendor
			disks[i].Name = d.Name
			disks[i].Path = d.Path
			disks[i].Hctl = d.Hctl
			disks[i].ByPath = d.ByPath
			disks[i].Model = d.Model
			disks[i].Wwn = d.Wwn
			disks[i].Serial = d.Serial
			disks[i].SizeBytes = d.SizeBytes
			disks[i].Bootable = d.Bootable
			disks[i].Smart = d.Smart
			disks[i].InstallationEligibility = aiv1beta1.HostInstallationEligibility{
				Eligible:           d.InstallationEligibility.Eligible,
				NotEligibleReasons: d.InstallationEligibility.NotEligibleReasons,
			}
			if d.InstallationEligibility.NotEligibleReasons == nil {
				disks[i].InstallationEligibility.NotEligibleReasons = make([]string, 0)
			}
			if d.IoPerf != nil {
				disks[i].IoPerf = aiv1beta1.HostIOPerf{
					SyncDurationMilliseconds: d.IoPerf.SyncDuration,
				}
			}
		}
	}
	return nil
}

func (r *AgentReconciler) updateIfNeeded(ctx context.Context, agent *aiv1beta1.Agent, c *common.Cluster) (ctrl.Result, error) {
	spec := agent.Spec
	var Requeue bool
	var inventoryErr error
	host := getHostFromCluster(c, agent.Name)
	if host == nil {
		r.Log.Errorf("Host %s not found in cluster %s", agent.Name, c.Name)
		return ctrl.Result{}, errors.New("Host not found in cluster")
	}

	internalHost, err := r.Installer.GetCommonHostInternal(ctx, string(*c.ID), agent.Name)
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			Requeue = true
			inventoryErr = common.NewApiError(http.StatusNotFound, err)
		} else {
			Requeue = false
			inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
		}
		return ctrl.Result{Requeue: Requeue}, inventoryErr
	}

	if internalHost.Approved != spec.Approved {
		err = r.Installer.UpdateHostApprovedInternal(ctx, string(*c.ID), agent.Name, spec.Approved)
		if err != nil {
			if gorm.IsRecordNotFoundError(err) {
				Requeue = true
				inventoryErr = common.NewApiError(http.StatusNotFound, err)
			} else {
				Requeue = false
				inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
			}
			return ctrl.Result{Requeue: Requeue}, inventoryErr
		}
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

	if spec.InstallationDiskID != "" && spec.InstallationDiskID != host.InstallationDiskID {
		clusterUpdate = true
		params.DisksSelectedConfig = []*models.ClusterUpdateParamsDisksSelectedConfigItems0{
			{
				DisksConfig: []*models.DiskConfigParams{
					{ID: &spec.InstallationDiskID, Role: models.DiskRoleInstall},
				},
				ID: strfmt.UUID(agent.Name),
			},
		}
	}

	if !clusterUpdate {
		return ctrl.Result{}, nil
	}

	_, err = r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
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

func (r *AgentReconciler) updateFailure(ctx context.Context, agent *aiv1beta1.Agent, err error) {
	conditionsv1.SetStatusCondition(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.AgentSyncedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  aiv1beta1.AgentSyncErrorReason,
		Message: aiv1beta1.AgentStateFailedToSync + ": " + err.Error(),
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
		For(&aiv1beta1.Agent{}).
		Watches(&source.Channel{Source: r.CRDEventsHandler.GetAgentUpdates()},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
