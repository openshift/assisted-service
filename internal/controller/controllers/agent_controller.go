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
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/host"
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

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Infof("Reconcile has been called for Agent name=%s namespace=%s", req.Name, req.Namespace)

	agent := &aiv1beta1.Agent{}

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
		// Update that we failed to retrieve the clusterDeployment
		return r.updateStatus(ctx, agent, nil, errors.Wrapf(err, errMsg), !k8serrors.IsNotFound(err))
	}

	// Retrieve cluster for ClusterDeploymentName from the database
	cluster, err := r.Installer.GetClusterByKubeKey(kubeKey)
	if err != nil {
		// Update that we failed to retrieve the cluster from the database
		return r.updateStatus(ctx, agent, nil, err, !errors.Is(err, gorm.ErrRecordNotFound))
	}

	//Retrieve host from cluster
	host := getHostFromCluster(cluster, agent.Name)
	if host == nil {
		return r.updateStatus(ctx, agent, nil, errors.New("host not found in cluster"), false)
	}

	// check for updates from user, compare spec and update if needed
	err = r.updateIfNeeded(ctx, agent, cluster)
	if err != nil {
		return r.updateStatus(ctx, agent, host, err, !IsHTTP4XXError(err))
	}

	err = r.updateInventory(host, agent)
	if err != nil {
		return r.updateStatus(ctx, agent, host, err, true)
	}

	return r.updateStatus(ctx, agent, host, nil, false)
}

// updateStatus is updating all the Agent Conditions.
// In case that an error has occured when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *AgentReconciler) updateStatus(ctx context.Context, agent *aiv1beta1.Agent, h *models.Host, syncErr error, internal bool) (ctrl.Result, error) {

	specSynced(agent, syncErr, internal)

	if h != nil && h.Status != nil {
		status := *h.Status
		connected(agent, status)
		readyForInstallation(agent, status)
		validated(agent, status, h)
		installed(agent, status, swag.StringValue(h.StatusInfo))
	} else {
		setConditionsUnknown(agent)
	}
	if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update agent Status")
		return ctrl.Result{Requeue: true}, nil
	}
	if internal {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ctrl.Result{}, nil
}

func setConditionsUnknown(agent *aiv1beta1.Agent) {
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.InstalledCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  aiv1beta1.NotAvailableReason,
		Message: aiv1beta1.NotAvailableMsg,
	})
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConnectedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  aiv1beta1.NotAvailableReason,
		Message: aiv1beta1.NotAvailableMsg,
	})
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ReadyForInstallationCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  aiv1beta1.NotAvailableReason,
		Message: aiv1beta1.NotAvailableMsg,
	})
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ValidatedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  aiv1beta1.NotAvailableReason,
		Message: aiv1beta1.NotAvailableMsg,
	})
}

// specSynced is updating the Agent SpecSynced Condition.
//Internal bool differentiate between the reason BackendErrorReason/InputErrorReason.
//if true then it is a backend server error (internal HTTP 5XX) otherwise an user input error (HTTP 4XXX)
func specSynced(agent *aiv1beta1.Agent, syncErr error, internal bool) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	if syncErr == nil {
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.SyncedOkReason
		msg = aiv1beta1.SyncedOkMsg
	} else {
		condStatus = corev1.ConditionFalse
		if internal {
			reason = aiv1beta1.BackendErrorReason
			msg = aiv1beta1.BackendErrorMsg + " " + syncErr.Error()
		} else {
			reason = aiv1beta1.InputErrorReason
			msg = aiv1beta1.InputErrorMsg + " " + syncErr.Error()
		}
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.SpecSyncedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func (r *AgentReconciler) updateInstallerArgs(ctx context.Context, c *common.Cluster, host *common.Host, agent *aiv1beta1.Agent) error {

	if agent.Spec.InstallerArgs == host.InstallerArgs {
		r.Log.Debugf("Nothing to update, installer args were already set")
		return nil
	}

	// InstallerArgs are saved in DB as string after unmarshalling of []string
	// that operation removes all whitespaces between words
	// in order to be able to validate that field didn't changed
	// doing reverse operation
	// If agent.Spec.InstallerArgs was not set but host.InstallerArgs was, we need to delete InstallerArgs
	agentSpecInstallerArgs := models.InstallerArgsParams{Args: []string{}}
	if agent.Spec.InstallerArgs != "" {
		err := json.Unmarshal([]byte(agent.Spec.InstallerArgs), &agentSpecInstallerArgs.Args)
		if err != nil {
			msg := fmt.Sprintf("Fail to unmarshal installer args for host %s in cluster %s", agent.Name, c.Name)
			r.Log.WithError(err).Errorf(msg)
			return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, msg))
		}
	}

	// as we marshalling same var or []string, there is no point to verify error on marshalling it
	argsBytes, _ := json.Marshal(agentSpecInstallerArgs.Args)
	// we need to validate if the equal one more after marshalling
	if string(argsBytes) == host.InstallerArgs {
		r.Log.Debugf("Nothing to update, installer args were already set")
		return nil
	}

	params := installer.UpdateHostInstallerArgsParams{
		ClusterID:           *c.ID,
		HostID:              strfmt.UUID(agent.Name),
		InstallerArgsParams: &agentSpecInstallerArgs,
	}
	_, err := r.Installer.UpdateHostInstallerArgsInternal(ctx, params)

	return err
}

func installed(agent *aiv1beta1.Agent, status, statusInfo string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusInstalled:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentInstalledReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentInstalledMsg, statusInfo)
	case models.HostStatusError:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentInstallationFailedReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentInstallationFailedMsg, statusInfo)
	case models.HostStatusInsufficient, models.HostStatusDisconnected, models.HostStatusDiscovering,
		models.HostStatusPendingForInput, models.HostStatusKnown:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentInstallationNotStartedReason
		msg = aiv1beta1.AgentInstallationNotStartedMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful,
		models.HostStatusInstalling, models.HostStatusInstallingInProgress,
		models.HostStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentInstallationInProgressReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentInstallationInProgressMsg, statusInfo)
	default:
		condStatus = corev1.ConditionUnknown
		reason = aiv1beta1.UnknownStatusReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.UnknownStatusMsg, status)
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.InstalledCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func validated(agent *aiv1beta1.Agent, status string, h *models.Host) {
	failedValidationInfo := ""
	validationRes, err := host.GetValidations(h)
	var failures []string
	if err == nil {
		for _, vRes := range validationRes {
			for _, v := range vRes {
				if v.Status == host.ValidationFailure {
					failures = append(failures, v.Message)
				}
			}
		}
		failedValidationInfo = strings.Join(failures[:], ",")
	}
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch {
	case models.HostStatusInsufficient == status:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentValidationsFailingReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentValidationsFailingMsg, failedValidationInfo)
	case h.ValidationsInfo == "":
		condStatus = corev1.ConditionUnknown
		reason = aiv1beta1.AgentValidationsUnknownReason
		msg = aiv1beta1.AgentValidationsUnknownMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentValidationsPassingReason
		msg = aiv1beta1.AgentValidationsPassingMsg
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ValidatedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func connected(agent *aiv1beta1.Agent, status string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusDisconnected:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentDisconnectedReason
		msg = aiv1beta1.AgentDisonnectedMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentConnectedReason
		msg = aiv1beta1.AgentConnectedMsg
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConnectedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func readyForInstallation(agent *aiv1beta1.Agent, status string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusKnown:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentReadyReason
		msg = aiv1beta1.AgentReadyMsg
	case models.HostStatusInsufficient, models.HostStatusDisconnected,
		models.HostStatusDiscovering, models.HostStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentNotReadyReason
		msg = aiv1beta1.AgentNotReadyMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful, models.HostStatusInstalled,
		models.HostStatusInstalling, models.HostStatusInstallingInProgress, models.HostStatusInstallingPendingUserAction,
		models.HostStatusError:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentAlreadyInstallingReason
		msg = aiv1beta1.AgentAlreadyInstallingMsg
	default:
		condStatus = corev1.ConditionUnknown
		reason = aiv1beta1.UnknownStatusReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.UnknownStatusMsg, status)
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ReadyForInstallationCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func (r *AgentReconciler) updateInventory(host *models.Host, agent *aiv1beta1.Agent) error {
	if host.Inventory == "" {
		r.Log.Debugf("Skip update inventory: Host %s inventory not set", agent.Name)
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

func (r *AgentReconciler) updateIfNeeded(ctx context.Context, agent *aiv1beta1.Agent, c *common.Cluster) error {
	spec := agent.Spec
	host := getHostFromCluster(c, agent.Name)
	if host == nil {
		r.Log.Errorf("Host %s not found in cluster %s", agent.Name, c.Name)
		return errors.New("Host not found in cluster")
	}

	internalHost, err := r.Installer.GetCommonHostInternal(ctx, string(*c.ID), agent.Name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		return err
	}

	if internalHost.Approved != spec.Approved {
		err = r.Installer.UpdateHostApprovedInternal(ctx, string(*c.ID), agent.Name, spec.Approved)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = common.NewApiError(http.StatusNotFound, err)
			}
			return err
		}
	}

	err = r.updateInstallerArgs(ctx, c, internalHost, agent)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		return err
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
		return nil
	}

	_, err = r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *c.ID,
	})
	if err != nil {
		return err
	}

	r.Log.Infof("Updated Agent spec %s %s", agent.Name, agent.Namespace)

	return nil
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
