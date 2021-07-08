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
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	restclient "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	AgentFinalizerName = "agent." + aiv1beta1.Group + "/ai-deprovision"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	Scheme           *runtime.Scheme
	Installer        bminventory.InstallerInternals
	CRDEventsHandler CRDEventsHandler
	ServiceBaseURL   string
	AuthType         auth.AuthType
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/ai-deprovision,verbs=update

func (r *AgentReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"agent":           req.Name,
			"agent_namespace": req.Namespace,
		})

	defer func() {
		log.Info("Agent Reconcile ended")
	}()

	log.Info("Agent Reconcile started")

	agent := &aiv1beta1.Agent{}

	err := r.Get(ctx, req.NamespacedName, agent)
	if err != nil {
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if agent.Spec.ClusterDeploymentName == nil {
		log.Debugf("ClusterDeploymentName not set in Agent %s. Skipping Reconcile", agent.Name)
		return ctrl.Result{Requeue: false}, nil
	}

	if agent.ObjectMeta.DeletionTimestamp.IsZero() { // agent not being deleted
		// Register a finalizer if it is absent.
		if !funk.ContainsString(agent.GetFinalizers(), AgentFinalizerName) {
			controllerutil.AddFinalizer(agent, AgentFinalizerName)
			if err = r.Update(ctx, agent); err != nil {
				log.WithError(err).Errorf("failed to add finalizer %s to resource %s %s", AgentFinalizerName, agent.Name, agent.Namespace)
				return ctrl.Result{Requeue: true}, err
			}
		}
	} else { // agent is being deleted
		if funk.ContainsString(agent.GetFinalizers(), AgentFinalizerName) {
			// deletion finalizer found, deregister the backend host and delete the agent
			reply, cleanUpErr := r.deregisterHostIfNeeded(ctx, log, req.NamespacedName)
			if cleanUpErr != nil {
				log.WithError(cleanUpErr).Errorf("failed to run pre-deletion cleanup for finalizer %s on resource %s %s", AgentFinalizerName, agent.Name, agent.Namespace)
				return reply, err
			}
			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(agent, AgentFinalizerName)
			if err = r.Update(ctx, agent); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from resource %s %s", AgentFinalizerName, agent.Name, agent.Namespace)
				return ctrl.Result{Requeue: true}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	kubeKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}

	// Retrieve clusterDeployment
	if err = r.Get(ctx, kubeKey, clusterDeployment); err != nil {
		if k8serrors.IsNotFound(err) {
			// Delete the agent, using a finalizer with pre-delete to deregister the host.
			log.Infof("Cluster Deployment name: %s namespace: %s not found, deleting Agent",
				agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
			return r.deleteAgent(ctx, log, req.NamespacedName)
		}

		errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
			agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
		log.WithError(err).Error(errMsg)
		// Update that we failed to retrieve the clusterDeployment
		return r.updateStatus(ctx, log, agent, nil, nil, errors.Wrapf(err, errMsg), !k8serrors.IsNotFound(err))
	}

	// Retrieve cluster by ClusterDeploymentName from the database
	cluster, err := r.Installer.GetClusterByKubeKey(kubeKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Delete the agent, using a finalizer with pre-delete to deregister the host.
			log.Infof("Cluster name: %s namespace: %s not found in backend, deleting Agent",
				agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
			return r.deleteAgent(ctx, log, req.NamespacedName)
		}
		// Update that we failed to retrieve the cluster from the database
		return r.updateStatus(ctx, log, agent, nil, nil, err, !errors.Is(err, gorm.ErrRecordNotFound))
	}

	//Retrieve host from cluster
	clusterId := cluster.ID.String()
	host := getHostFromCluster(cluster, agent.Name)
	if host == nil {
		// Host is not a part of the cluster, which may happen with newly created day2 clusters.
		// Delete the agent, using a finalizer with pre-delete to deregister the host.
		log.Infof("Host not found in Cluster ID :%s deleting Agent", string(*cluster.ID))
		return r.deleteAgent(ctx, log, req.NamespacedName)
	}

	// check for updates from user, compare spec and update if needed
	err = r.updateIfNeeded(ctx, log, agent, cluster)
	if err != nil {
		return r.updateStatus(ctx, log, agent, host, &clusterId, err, !IsUserError(err))
	}

	err = r.updateInventory(log, host, agent)
	if err != nil {
		return r.updateStatus(ctx, log, agent, host, &clusterId, err, true)
	}

	err = r.updateNtpSources(log, host, agent)
	if err != nil {
		return r.updateStatus(ctx, log, agent, host, &clusterId, err, true)
	}

	return r.updateStatus(ctx, log, agent, host, &clusterId, nil, false)
}

func (r *AgentReconciler) deleteAgent(ctx context.Context, log logrus.FieldLogger, agent types.NamespacedName) (ctrl.Result, error) {
	agentToDelete := &aiv1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
		},
	}
	if delErr := r.Client.Delete(ctx, agentToDelete); delErr != nil {
		log.WithError(delErr).Errorf("Failed to delete resource %s %s", agent.Name, agent.Namespace)
		return ctrl.Result{Requeue: true}, delErr
	}
	return ctrl.Result{}, nil
}

func (r *AgentReconciler) deregisterHostIfNeeded(ctx context.Context, log logrus.FieldLogger, key types.NamespacedName) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "failed to deregister host: %s", key.Name)
		log.Error(err)
		return reply, err
	}

	h, err := r.Installer.GetHostByKubeKey(key)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// return if from any reason host is already deleted from db (or never existed)
			return buildReply(nil)
		} else {
			return buildReply(err)
		}
	}

	err = r.Installer.DeregisterHostInternal(
		ctx, installer.DeregisterHostParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
		})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// return if from any reason host is already deleted from db
			return buildReply(nil)
		} else {
			return buildReply(err)
		}
	}

	log.Infof("Host resource deleted, Unregistered host: %s", h.ID.String())

	return buildReply(nil)
}

// updateStatus is updating all the Agent Conditions.
// In case that an error has occured when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *AgentReconciler) updateStatus(ctx context.Context, log logrus.FieldLogger, agent *aiv1beta1.Agent, h *models.Host, clusterId *string, syncErr error, internal bool) (ctrl.Result, error) {

	specSynced(agent, syncErr, internal)

	if h != nil && h.Status != nil {
		agent.Status.Bootstrap = h.Bootstrap
		agent.Status.Role = h.Role
		agent.Status.DebugInfo.State = swag.StringValue(h.Status)
		agent.Status.DebugInfo.StateInfo = swag.StringValue(h.StatusInfo)
		if h.Progress != nil && h.Progress.CurrentStage != "" {
			agent.Status.Progress.CurrentStage = h.Progress.CurrentStage
			agent.Status.Progress.ProgressInfo = h.Progress.ProgressInfo
			stageStartTime := metav1.NewTime(time.Time(h.Progress.StageStartedAt))
			agent.Status.Progress.StageStartTime = &stageStartTime
			stageUpdateTime := metav1.NewTime(time.Time(h.Progress.StageUpdatedAt))
			agent.Status.Progress.StageUpdateTime = &stageUpdateTime
		}
		status := *h.Status
		if clusterId != nil {
			err := r.populateEventsURL(log, agent, *clusterId)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
		}
		connected(agent, status)
		requirementsMet(agent, status)
		validated(agent, status, h)
		installed(agent, status, swag.StringValue(h.StatusInfo))
	} else {
		setConditionsUnknown(agent)
	}
	if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
		log.WithError(updateErr).Error("failed to update agent Status")
		return ctrl.Result{Requeue: true}, nil
	}
	if internal {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ctrl.Result{}, nil
}

func (r *AgentReconciler) populateEventsURL(log logrus.FieldLogger, agent *aiv1beta1.Agent, clusterId string) error {
	if agent.Status.DebugInfo.EventsURL == "" || !strings.Contains(agent.Status.DebugInfo.EventsURL, clusterId) {
		eventUrl, err := r.eventsURL(log, clusterId, agent.Name)
		if err != nil {
			log.WithError(err).Error("failed to generate Events URL")
			return err
		}
		agent.Status.DebugInfo.EventsURL = eventUrl
	}
	return nil
}

func (r *AgentReconciler) eventsURL(log logrus.FieldLogger, clusterId, agentId string) (string, error) {
	eventsURL := fmt.Sprintf("%s%s/clusters/%s/events?host_id=%s", r.ServiceBaseURL, restclient.DefaultBasePath, clusterId, agentId)
	if r.AuthType != auth.TypeLocal {
		return eventsURL, nil
	}
	eventsURL, err := gencrypto.SignURL(eventsURL, clusterId)
	if err != nil {
		log.WithError(err).Error("failed to get Events URL")
		return "", err
	}
	return eventsURL, nil
}

func setConditionsUnknown(agent *aiv1beta1.Agent) {
	agent.Status.DebugInfo.State = ""
	agent.Status.DebugInfo.StateInfo = ""
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
		Type:    aiv1beta1.RequirementsMetCondition,
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

func (r *AgentReconciler) updateInstallerArgs(ctx context.Context, log logrus.FieldLogger, c *common.Cluster, host *common.Host, agent *aiv1beta1.Agent) error {

	if agent.Spec.InstallerArgs == host.InstallerArgs {
		log.Debugf("Nothing to update, installer args were already set")
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
			log.WithError(err).Errorf(msg)
			return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, msg))
		}
	}

	// as we marshalling same var or []string, there is no point to verify error on marshalling it
	argsBytes, _ := json.Marshal(agentSpecInstallerArgs.Args)
	// we need to validate if the equal one more after marshalling
	if string(argsBytes) == host.InstallerArgs {
		log.Debugf("Nothing to update, installer args were already set")
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
		reason = aiv1beta1.InstalledReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstalledMsg, statusInfo)
	case models.HostStatusError:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationFailedReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstallationFailedMsg, statusInfo)
	case models.HostStatusInsufficient, models.HostStatusDisconnected, models.HostStatusDiscovering,
		models.HostStatusPendingForInput, models.HostStatusKnown:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationNotStartedReason
		msg = aiv1beta1.InstallationNotStartedMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful,
		models.HostStatusInstalling, models.HostStatusInstallingInProgress,
		models.HostStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationInProgressReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstallationInProgressMsg, statusInfo)
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
				if v.Status != host.ValidationSuccess && v.Status != host.ValidationDisabled {
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
		reason = aiv1beta1.ValidationsFailingReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentValidationsFailingMsg, failedValidationInfo)
	case models.HostStatusPendingForInput == status:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.ValidationsUserPendingReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.AgentValidationsUserPendingMsg, failedValidationInfo)
	case h.ValidationsInfo == "":
		condStatus = corev1.ConditionUnknown
		reason = aiv1beta1.ValidationsUnknownReason
		msg = aiv1beta1.AgentValidationsUnknownMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.ValidationsPassingReason
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

func requirementsMet(agent *aiv1beta1.Agent, status string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusKnown:
		if agent.Spec.Approved {
			condStatus = corev1.ConditionTrue
			reason = aiv1beta1.AgentReadyReason
			msg = aiv1beta1.AgentReadyMsg
		} else {
			condStatus = corev1.ConditionFalse
			reason = aiv1beta1.AgentIsNotApprovedReason
			msg = aiv1beta1.AgentIsNotApprovedMsg
		}
	case models.HostStatusInsufficient, models.HostStatusDisconnected,
		models.HostStatusDiscovering, models.HostStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentNotReadyReason
		msg = aiv1beta1.AgentNotReadyMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful, models.HostStatusInstalling,
		models.HostStatusInstallingInProgress, models.HostStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentAlreadyInstallingReason
		msg = aiv1beta1.AgentAlreadyInstallingMsg
	case models.HostStatusInstalled, models.HostStatusError:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentInstallationStoppedReason
		msg = aiv1beta1.AgentInstallationStoppedMsg
	default:
		condStatus = corev1.ConditionUnknown
		reason = aiv1beta1.UnknownStatusReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.UnknownStatusMsg, status)
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.RequirementsMetCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func (r *AgentReconciler) updateNtpSources(log logrus.FieldLogger, host *models.Host, agent *aiv1beta1.Agent) error {
	if host.NtpSources == "" {
		log.Debugf("Skip update NTP Sources: Host %s NTP sources not set", agent.Name)
		return nil
	}
	var ntpSources []*models.NtpSource
	if err := json.Unmarshal([]byte(host.NtpSources), &ntpSources); err != nil {
		log.WithError(err).Errorf("Failed to unmarshal NTP Sources %s:", host.NtpSources)
		return err
	}
	if ntpSources != nil {
		ntps := make([]aiv1beta1.HostNTPSources, len(ntpSources))
		agent.Status.NtpSources = ntps
		for i, ntp := range ntpSources {
			ntps[i].SourceName = ntp.SourceName
			ntps[i].SourceState = ntp.SourceState
		}
	}
	return nil
}

func (r *AgentReconciler) updateInventory(log logrus.FieldLogger, host *models.Host, agent *aiv1beta1.Agent) error {
	if host.Inventory == "" {
		log.Debugf("Skip update inventory: Host %s inventory not set", agent.Name)
		return nil
	}
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		log.WithError(err).Errorf("Failed to unmarshal host inventory")
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

func (r *AgentReconciler) updateHostIgnition(ctx context.Context, log logrus.FieldLogger, c *common.Cluster, host *common.Host, agent *aiv1beta1.Agent) error {
	if agent.Spec.IgnitionConfigOverrides == host.IgnitionConfigOverrides {
		log.Debugf("Nothing to update, ignition config override was already set")
		return nil
	}
	agentHostIgnitionParams := models.HostIgnitionParams{Config: ""}
	if agent.Spec.IgnitionConfigOverrides != "" {
		agentHostIgnitionParams.Config = agent.Spec.IgnitionConfigOverrides
	}
	params := installer.UpdateHostIgnitionParams{
		ClusterID:          *c.ID,
		HostID:             strfmt.UUID(agent.Name),
		HostIgnitionParams: &agentHostIgnitionParams,
	}
	_, err := r.Installer.UpdateHostIgnitionInternal(ctx, params)

	return err
}

func (r *AgentReconciler) updateIfNeeded(ctx context.Context, log logrus.FieldLogger, agent *aiv1beta1.Agent, c *common.Cluster) error {
	spec := agent.Spec
	host := getHostFromCluster(c, agent.Name)
	if host == nil {
		log.Errorf("Host %s not found in cluster %s", agent.Name, c.Name)
		return errors.New("Host not found in cluster")
	}

	internalHost, err := r.Installer.GetCommonHostInternal(ctx, string(*c.ID), agent.Name)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("Failed to get common host from cluster %s", string(*c.ID))
		return err
	}

	if internalHost.Approved != spec.Approved {
		err = r.Installer.UpdateHostApprovedInternal(ctx, string(*c.ID), agent.Name, spec.Approved)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = common.NewApiError(http.StatusNotFound, err)
			}
			log.WithError(err).Errorf("Failed to approve Agent")
			return err
		}
	}

	err = r.updateInstallerArgs(ctx, log, c, internalHost, agent)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("Failed to update installer args")
		return err
	}

	err = r.updateHostIgnition(ctx, log, c, internalHost, agent)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("Failed to update host ignition")
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

	_, err = r.Installer.UpdateClusterNonInteractive(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *c.ID,
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to update host params in cluster %s", string(*c.ID))
		return err
	}

	log.Infof("Updated Agent spec %s %s", agent.Name, agent.Namespace)

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
