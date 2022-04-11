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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/openshift/assisted-service/api/common"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
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
	"gorm.io/gorm"
	certificatesv1 "k8s.io/api/certificates/v1"
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
	AgentFinalizerName   = "agent." + aiv1beta1.Group + "/ai-deprovision"
	InventoryLabelPrefix = "inventory." + aiv1beta1.Group + "/"
)

// AgentReconciler reconciles a Agent object
type AgentReconciler struct {
	client.Client
	APIReader                  client.Reader
	Log                        logrus.FieldLogger
	Scheme                     *runtime.Scheme
	Installer                  bminventory.InstallerInternals
	CRDEventsHandler           CRDEventsHandler
	ServiceBaseURL             string
	AuthType                   auth.AuthType
	SpokeK8sClientFactory      SpokeK8sClientFactory
	ApproveCsrsRequeueDuration time.Duration
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/ai-deprovision,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

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
	origAgent := agent.DeepCopy()
	if agent.ObjectMeta.Annotations == nil {
		agent.ObjectMeta.Annotations = make(map[string]string)
	}
	if agent.ObjectMeta.Labels == nil {
		agent.ObjectMeta.Labels = make(map[string]string)
	}

	if agent.ObjectMeta.DeletionTimestamp.IsZero() { // agent not being deleted
		// Register a finalizer if it is absent.
		if !funk.ContainsString(agent.GetFinalizers(), AgentFinalizerName) {
			controllerutil.AddFinalizer(agent, AgentFinalizerName)
			if err = r.Update(ctx, agent); err != nil {
				log.WithError(err).Errorf("failed to add finalizer %s to resource %s %s", AgentFinalizerName, agent.Name, agent.Namespace)
			}
			// After update there should not be any more changes on the object
			// Update will return a new object so the creation of maps like annotations or labels is not valid anymore
			return ctrl.Result{Requeue: true}, nil
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

	h, err := r.Installer.GetHostByKubeKey(req.NamespacedName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return r.deleteAgent(ctx, log, req.NamespacedName)
		} else {
			log.WithError(err).Errorf("failed to retrieve Host %s from backend", agent.Name)
			return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, err
		}
	}

	if agent.Spec.ClusterDeploymentName == nil && h.ClusterID != nil {
		log.Debugf("ClusterDeploymentName is unset in Agent %s. unbind", agent.Name)
		return r.unbindHost(ctx, log, agent, origAgent, h)
	}

	if agent.Spec.ClusterDeploymentName != nil {
		kubeKey := types.NamespacedName{
			Namespace: agent.Spec.ClusterDeploymentName.Namespace,
			Name:      agent.Spec.ClusterDeploymentName.Name,
		}
		clusterDeployment := &hivev1.ClusterDeployment{}

		// Retrieve clusterDeployment
		if err = r.Get(ctx, kubeKey, clusterDeployment); err != nil {
			errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
				agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
			log.WithError(err).Error(errMsg)
			// Update that we failed to retrieve the clusterDeployment
			//TODO MGMT-7844 add mapping CD-ACI to rnot requeue always
			return r.updateStatus(ctx, log, agent, origAgent, &h.Host, nil, errors.Wrapf(err, errMsg), true)
		}

		// Retrieve cluster by ClusterDeploymentName from the database
		cluster, err2 := r.Installer.GetClusterByKubeKey(kubeKey)
		if err2 != nil {
			log.WithError(err2).Errorf("Fail to get cluster name: %s namespace: %s in backend",
				agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
			// Update that we failed to retrieve the cluster from the database
			return r.updateStatus(ctx, log, agent, origAgent, &h.Host, nil, err2, true)
		}

		if h.ClusterID == nil {
			log.Infof("ClusterDeploymentName is set in Agent %s. bind", agent.Name)

			host, err2 := r.Installer.BindHostInternal(ctx, installer.BindHostParams{
				HostID:     *h.ID,
				InfraEnvID: h.InfraEnvID,
				BindHostParams: &models.BindHostParams{
					ClusterID: cluster.ID,
				},
			})
			if err2 != nil {
				return r.updateStatus(ctx, log, agent, origAgent, &h.Host, nil, err2, !IsUserError(err2))
			}
			return r.updateStatus(ctx, log, agent, origAgent, &host.Host, cluster.ID, nil, true)
		} else if *h.ClusterID != *cluster.ID {
			log.Infof("ClusterDeploymentName is changed in Agent %s. unbind first", agent.Name)
			return r.unbindHost(ctx, log, agent, origAgent, h)
		}
	}

	// check for updates from user, compare spec and update if needed
	h, err = r.updateIfNeeded(ctx, log, agent, h)
	if err != nil {
		return r.updateStatus(ctx, log, agent, origAgent, &h.Host, h.ClusterID, err, !IsUserError(err))
	}

	err = r.updateInventory(log, ctx, &h.Host, agent)
	if err != nil {
		return r.updateStatus(ctx, log, agent, origAgent, &h.Host, h.ClusterID, err, true)
	}

	err = r.updateNtpSources(log, &h.Host, agent)
	if err != nil {
		return r.updateStatus(ctx, log, agent, origAgent, &h.Host, h.ClusterID, err, true)
	}

	return r.updateStatus(ctx, log, agent, origAgent, &h.Host, h.ClusterID, nil, false)
}

func (r *AgentReconciler) shouldApproveMoreCSRs(node *corev1.Node) bool {
	return !isNodeReady(node)
}

// Validate that the CSR can be approved
func (r *AgentReconciler) shouldApproveCSR(csr *certificatesv1.CertificateSigningRequest, agent *aiv1beta1.Agent, validateNodeCsr nodeCsrValidator) (bool, error) {
	x509CSR, err := getX509ParsedRequest(csr)
	if err != nil {
		return false, err
	}
	if !isCsrAssociatedWithAgent(x509CSR, agent) {
		return false, nil
	}

	return validateNodeCsr(agent, csr, x509CSR)
}

func (r *AgentReconciler) approveAIHostsCSRs(clients SpokeK8sClient, agent *aiv1beta1.Agent, validateNodeCsr nodeCsrValidator) {
	csrList, err := clients.ListCsrs()
	if err != nil {
		r.Log.WithError(err).Errorf("Failed to get CSRs for agent %s/%s", agent.Namespace, agent.Name)
		return
	}
	for i := range csrList.Items {
		csr := &csrList.Items[i]
		if !isCsrApproved(csr) {
			shouldApprove, err := r.shouldApproveCSR(csr, agent, validateNodeCsr)
			if err != nil || !shouldApprove {
				if err != nil {
					r.Log.WithError(err).Errorf("Failed checking if CSR %s should be approved for agent %s/%s",
						csr.Name, agent.Namespace, agent.Name)
				}
				continue
			}
			if err = clients.ApproveCsr(csr); err != nil {
				r.Log.WithError(err).Errorf("Failed to approve CSR %s for agent %s/%s", csr.Name, agent.Namespace, agent.Name)
				continue
			}
		}
	}
}

// Attempt to approve CSRs for agent. If already approved then the node will be marked as done
// requeue means that approval will be attempted again
func (r *AgentReconciler) tryApproveDay2CSRs(ctx context.Context, agent *aiv1beta1.Agent) bool {
	r.Log.Infof("Approving CSRs for agent %s/%s", agent.Namespace, agent.Name)

	// Get adminKubeConfigSecretName from clusterDeployment or fallback to template
	adminKubeConfigSecretName := fmt.Sprintf(adminKubeConfigStringTemplate, agent.Spec.ClusterDeploymentName.Name)
	clusterDeployment := &hivev1.ClusterDeployment{}
	cdKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	err := r.Get(ctx, cdKey, clusterDeployment)
	if err != nil {
		adminKubeConfigSecretName = getClusterDeploymentAdminKubeConfigSecretName(clusterDeployment)
	}

	namespacedName := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      adminKubeConfigSecretName,
	}

	secret, err := getSecret(ctx, r.Client, r.APIReader, namespacedName)
	if err != nil {
		r.Log.WithError(err).Errorf("Agent %s/%s: Failed to get secret", agent.Namespace, agent.Name)
		return false
	}
	if err = ensureSecretIsLabelled(ctx, r.Client, secret, namespacedName); err != nil {
		r.Log.WithError(err).Errorf("Agent %s/%s: Failed to label secret", agent.Namespace, agent.Name)
		return false
	}
	clients, err := r.SpokeK8sClientFactory.Create(secret)
	if err != nil {
		r.Log.WithError(err).Errorf("Agent %s/%s: Failed to create spoke client", agent.Namespace, agent.Name)
		return false
	}
	hostname := getAgentHostname(agent)
	var (
		validateNodeCsr       nodeCsrValidator
		shouldApproveMoreCSRs bool
	)

	// TODO: Node name might be FQDN and not just host name if cluster is IPv6
	node, err := clients.GetNode(hostname)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			r.Log.WithError(err).Errorf("Agent %s/%s: Failed to get node %s", agent.Namespace, agent.Name, hostname)
			return false
		}
		validateNodeCsr = validateNodeClientCSR
		shouldApproveMoreCSRs = true
	} else {
		validateNodeCsr = createNodeServerCsrValidator(node)
		shouldApproveMoreCSRs = r.shouldApproveMoreCSRs(node)
	}

	// Even if node is already ready, we try approving last time
	r.approveAIHostsCSRs(clients, agent, validateNodeCsr)

	return !shouldApproveMoreCSRs
}

func (r *AgentReconciler) unbindHost(ctx context.Context, log logrus.FieldLogger, agent, origAgent *aiv1beta1.Agent, h *common.Host) (ctrl.Result, error) {
	host, err2 := r.Installer.UnbindHostInternal(ctx, installer.UnbindHostParams{
		HostID:     *h.ID,
		InfraEnvID: h.InfraEnvID,
	})
	if err2 != nil {
		return r.updateStatus(ctx, log, agent, origAgent, &h.Host, nil, err2, !IsUserError(err2))
	}
	return r.updateStatus(ctx, log, agent, origAgent, &host.Host, h.ClusterID, nil, true)
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

	err = r.Installer.V2DeregisterHostInternal(
		ctx, installer.V2DeregisterHostParams{
			InfraEnvID: h.InfraEnvID,
			HostID:     *h.ID,
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

func (r *AgentReconciler) isDay2NonePlatformHostRebooting(ctx context.Context, agent *aiv1beta1.Agent, h *models.Host) (bool, error) {
	if swag.StringValue(h.Status) == models.HostStatusAddedToExistingCluster &&
		h.Progress.CurrentStage == models.HostStageDone {
		if agent.Status.Progress.CurrentStage == models.HostStageDone {
			return false, nil
		} else {
			isNone, err := isAgentInNonePlatformCluster(ctx, r.Client, agent)
			if err != nil {
				return false, err
			}
			return isNone, nil
		}
	}
	return false, nil
}

// updateStatus is updating all the Agent Conditions.
// In case that an error has ocurred when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *AgentReconciler) updateStatus(ctx context.Context, log logrus.FieldLogger, agent, origAgent *aiv1beta1.Agent, h *models.Host, clusterId *strfmt.UUID, syncErr error, internal bool) (ctrl.Result, error) {

	var (
		err                 error
		isNoneDay2Rebooting bool
	)
	ret := ctrl.Result{}
	specSynced(agent, syncErr, internal)

	if h != nil && h.Status != nil {
		agent.Status.Bootstrap = h.Bootstrap
		agent.Status.Role = h.Role
		if h.SuggestedRole != "" && h.Role == models.HostRoleAutoAssign {
			agent.Status.Role = h.SuggestedRole
		}
		agent.Status.DebugInfo.State = swag.StringValue(h.Status)
		agent.Status.DebugInfo.StateInfo = swag.StringValue(h.StatusInfo)

		if h.ValidationsInfo != "" {
			newValidationsInfo := ValidationsStatus{}
			err = json.Unmarshal([]byte(h.ValidationsInfo), &newValidationsInfo)
			if err != nil {
				log.WithError(err).Error("failed to umarshed ValidationsInfo")
				return ctrl.Result{}, err
			}
			agent.Status.ValidationsInfo = newValidationsInfo
		}

		if h.Progress != nil && h.Progress.CurrentStage != "" {
			if isNoneDay2Rebooting, err = r.isDay2NonePlatformHostRebooting(ctx, agent, h); err != nil {
				log.WithError(err).Errorf("Failed to find if agent %s/%s belongs to none platform cluster and is rebooting", agent.Namespace, agent.Name)
				return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
			}

			if isNoneDay2Rebooting {
				agent.Status.Progress.CurrentStage = models.HostStageRebooting
			} else {
				agent.Status.Progress.CurrentStage = h.Progress.CurrentStage
			}
			agent.Status.Progress.ProgressInfo = h.Progress.ProgressInfo
			agent.Status.Progress.InstallationPercentage = h.Progress.InstallationPercentage
			stageStartTime := metav1.NewTime(time.Time(h.Progress.StageStartedAt))
			agent.Status.Progress.StageStartTime = &stageStartTime
			stageUpdateTime := metav1.NewTime(time.Time(h.Progress.StageUpdatedAt))
			agent.Status.Progress.StageUpdateTime = &stageUpdateTime
			agent.Status.Progress.ProgressStages = h.ProgressStages
		} else {
			agent.Status.Progress = aiv1beta1.HostProgressInfo{}
		}
		status := *h.Status
		if clusterId != nil {
			err := r.populateEventsURL(log, agent, h.InfraEnvID.String())
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
			if agent.Status.DebugInfo.LogsURL == "" && !time.Time(h.LogsCollectedAt).Equal(time.Time{}) { // logs collection time is updated means logs are available
				logsURL, err := generateControllerLogsDownloadURL(r.ServiceBaseURL, clusterId.String(), r.AuthType, agent.Name, "host")
				if err != nil {
					log.WithError(err).Error("failed to generate controller logs URL")
					return ctrl.Result{}, err
				}
				agent.Status.DebugInfo.LogsURL = logsURL
			}
		}
		connected(agent, status)
		requirementsMet(agent, status)
		validated(agent, status, h)
		installed(agent, status, swag.StringValue(h.StatusInfo))
		bound(agent, status, h)
	} else {
		setConditionsUnknown(agent)
	}
	if isNoneDay2Rebooting {
		alreadyApproved := r.tryApproveDay2CSRs(ctx, agent)
		if alreadyApproved {
			agent.Status.Progress.CurrentStage = models.HostStageDone
		} else {
			ret = ctrl.Result{RequeueAfter: r.ApproveCsrsRequeueDuration}
		}
	}
	if !reflect.DeepEqual(agent, origAgent) {
		if updateErr := r.Status().Update(ctx, agent); updateErr != nil {
			log.WithError(updateErr).Error("failed to update agent Status")
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		log.Debugf("Agent %s/%s: update skipped", agent.Namespace, agent.Name)
	}
	if syncErr != nil && internal {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ret, nil
}

func (r *AgentReconciler) populateEventsURL(log logrus.FieldLogger, agent *aiv1beta1.Agent, infraEnvId string) error {
	if agent.Status.DebugInfo.EventsURL == "" {
		tokenGen := gencrypto.CryptoPair{JWTKeyType: gencrypto.InfraEnvKey, JWTKeyValue: infraEnvId}
		eventUrl, err := generateEventsURL(r.ServiceBaseURL, r.AuthType, tokenGen, "host_id", agent.Name)
		if err != nil {
			log.WithError(err).Error("failed to generate Events URL")
			return err
		}
		agent.Status.DebugInfo.EventsURL = eventUrl
	}
	return nil
}

func generateControllerLogsDownloadURL(baseURL string, clusterID string, authType auth.AuthType, host string, logsType string) (string, error) {
	hostID := strfmt.UUID(host)
	builder := &installer.V2DownloadClusterLogsURL{
		ClusterID: strfmt.UUID(clusterID),
		HostID:    &hostID,
		LogsType:  &logsType,
	}
	u, err := builder.Build()
	if err != nil {
		return "", err
	}

	downloadURL := fmt.Sprintf("%s%s", baseURL, u.RequestURI())
	if authType != auth.TypeLocal {
		return downloadURL, nil
	}

	downloadURL, err = gencrypto.SignURL(downloadURL, clusterID, gencrypto.ClusterKey)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign %s controller logs URL for host %s", logsType, host)
	}

	return downloadURL, nil
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
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.BoundCondition,
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

func (r *AgentReconciler) updateInstallerArgs(ctx context.Context, log logrus.FieldLogger, host *common.Host, agent *aiv1beta1.Agent) error {

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
			msg := fmt.Sprintf("Fail to unmarshal installer args for host %s in infraEnv %s", agent.Name, host.InfraEnvID)
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

	params := installer.V2UpdateHostInstallerArgsParams{
		InfraEnvID:          host.InfraEnvID,
		HostID:              strfmt.UUID(agent.Name),
		InstallerArgsParams: &agentSpecInstallerArgs,
	}
	_, err := r.Installer.V2UpdateHostInstallerArgsInternal(ctx, params)
	log.Infof("Updated Agent InstallerArgs %s %s", agent.Name, agent.Namespace)
	return err
}

func installed(agent *aiv1beta1.Agent, status, statusInfo string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusInstalled, models.HostStatusAddedToExistingCluster:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.InstalledReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstalledMsg, statusInfo)
	case models.HostStatusError:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationFailedReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstallationFailedMsg, statusInfo)
	case models.HostStatusInsufficient, models.HostStatusInsufficientUnbound,
		models.HostStatusDisconnected, models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscovering, models.HostStatusDiscoveringUnbound,
		models.HostStatusKnown, models.HostStatusKnownUnbound,
		models.HostStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationNotStartedReason
		msg = aiv1beta1.InstallationNotStartedMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful,
		models.HostStatusInstalling, models.HostStatusInstallingInProgress,
		models.HostStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.InstallationInProgressReason
		msg = fmt.Sprintf("%s %s", aiv1beta1.InstallationInProgressMsg, statusInfo)
	case models.HostStatusBinding:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.BindingReason
		msg = aiv1beta1.BindingMsg
	case models.HostStatusUnbinding, models.HostStatusUnbindingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnbindingReason
		msg = aiv1beta1.UnbindingMsg
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
	case models.HostStatusBinding == status:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.BindingReason
		msg = aiv1beta1.BindingMsg
	case models.HostStatusUnbinding == status || models.HostStatusUnbindingPendingUserAction == status:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnbindingReason
		msg = aiv1beta1.UnbindingMsg
	case models.HostStatusInsufficient == status || models.HostStatusInsufficientUnbound == status:
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
	case models.HostStatusDisconnectedUnbound, models.HostStatusDisconnected:
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
	case models.HostStatusKnown, models.HostStatusKnownUnbound:
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
		models.HostStatusInsufficientUnbound, models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscoveringUnbound, models.HostStatusDiscovering,
		models.HostStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.AgentNotReadyReason
		msg = aiv1beta1.AgentNotReadyMsg
	case models.HostStatusPreparingForInstallation, models.HostStatusPreparingSuccessful, models.HostStatusInstalling,
		models.HostStatusInstallingInProgress, models.HostStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentAlreadyInstallingReason
		msg = aiv1beta1.AgentAlreadyInstallingMsg
	case models.HostStatusInstalled, models.HostStatusError, models.HostStatusAddedToExistingCluster:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.AgentInstallationStoppedReason
		msg = aiv1beta1.AgentInstallationStoppedMsg
	case models.HostStatusBinding:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.BindingReason
		msg = aiv1beta1.BindingMsg
	case models.HostStatusUnbinding, models.HostStatusUnbindingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnbindingReason
		msg = aiv1beta1.UnbindingMsg
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

func bound(agent *aiv1beta1.Agent, status string, h *models.Host) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.HostStatusBinding:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.BindingReason
		msg = aiv1beta1.BindingMsg
	case models.HostStatusUnbinding:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnbindingReason
		msg = aiv1beta1.UnbindingMsg
	case models.HostStatusUnbindingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnbindingPendingUserActionReason
		msg = aiv1beta1.UnbindingPendingUserActionMsg
	case models.HostStatusDisconnectedUnbound, models.HostStatusKnownUnbound, models.HostStatusInsufficientUnbound,
		models.HostStatusDisabledUnbound, models.HostStatusDiscoveringUnbound:
		condStatus = corev1.ConditionFalse
		reason = aiv1beta1.UnboundReason
		msg = aiv1beta1.UnboundMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = aiv1beta1.BoundReason
		msg = aiv1beta1.BoundMsg
	}
	conditionsv1.SetStatusConditionNoHeartbeat(&agent.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.BoundCondition,
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
			log.Debugf("Updating ntp source to %s/%s", ntp.SourceName, ntp.SourceState)
			ntps[i].SourceName = ntp.SourceName
			ntps[i].SourceState = ntp.SourceState
		}
	}
	return nil
}

func (r *AgentReconciler) updateInventory(log logrus.FieldLogger, ctx context.Context, host *models.Host, agent *aiv1beta1.Agent) error {
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

	return r.updateInventoryLabels(log, ctx, agent)
}

func (r *AgentReconciler) updateInventoryLabels(log logrus.FieldLogger, ctx context.Context, agent *aiv1beta1.Agent) error {
	inventory := agent.Status.Inventory
	hasSSD := false
	for _, d := range inventory.Disks {
		if d.DriveType == "SSD" {
			hasSSD = true
			break
		}
	}
	hasVirt := funk.Contains(inventory.Cpu.Flags, "vmx") || funk.Contains(inventory.Cpu.Flags, "svm")

	changed := false
	changed = setAgentAnnotation(log, agent, InventoryLabelPrefix+"version", "0.1") || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"storage-hasnonrotationaldisk", strconv.FormatBool(hasSSD)) || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"cpu-architecture", inventory.Cpu.Architecture) || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"cpu-virtenabled", strconv.FormatBool(hasVirt)) || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"host-manufacturer", inventory.SystemVendor.Manufacturer) || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"host-productname", inventory.SystemVendor.ProductName) || changed
	changed = setAgentLabel(log, agent, InventoryLabelPrefix+"host-isvirtual", strconv.FormatBool(inventory.SystemVendor.Virtual)) || changed

	if changed {
		if err := r.Update(ctx, agent); err != nil {
			log.WithError(err).Errorf("failed to add labels to agent %s/%s", agent.Namespace, agent.Name)
			return err
		}
		agentRef := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Name}
		err := r.Get(ctx, agentRef, agent)
		if err != nil {
			log.WithError(err).Errorf("Failed to get agent %s", agentRef)
			return err
		}
	}

	return nil
}

func setAgentAnnotation(log logrus.FieldLogger, agent *aiv1beta1.Agent, key string, value string) bool {
	annotations := agent.GetAnnotations()

	// If we already have an annotation with the same value no change is needed.
	if val, ok := annotations[key]; ok {
		if val == value {
			return false
		}
	}

	log.Infof("Setting annotation %s=%s on agent %s/%s", key, value, agent.Namespace, agent.Name)
	annotations[key] = value
	agent.SetAnnotations(annotations)
	return true
}

func setAgentLabel(log logrus.FieldLogger, agent *aiv1beta1.Agent, key string, value string) bool {
	labels := agent.GetLabels()

	// Label values can only have alphanumeric characters, '-', '_' or '.'
	re := regexp.MustCompile("[^-A-Za-z0-9_.]+")
	value = re.ReplaceAllString(value, "")

	// If the value still doesn't match the regex, skip it because it will cause the update to fail
	re = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
	if !re.MatchString(value) {
		log.Info("Skipping setting of label %s=%s because the value contains illegal characters", key, value)
		return false
	}

	// If we already have a label with the same value no change is needed.
	if val, ok := labels[key]; ok {
		if val == value {
			return false
		}
	}

	log.Infof("Setting label %s=%s on agent %s/%s", key, value, agent.Namespace, agent.Name)
	labels[key] = value
	agent.SetLabels(labels)
	return true
}

func (r *AgentReconciler) updateHostIgnition(ctx context.Context, log logrus.FieldLogger, host *common.Host, agent *aiv1beta1.Agent) error {
	if agent.Spec.IgnitionConfigOverrides == host.IgnitionConfigOverrides {
		log.Debugf("Nothing to update, ignition config override was already set")
		return nil
	}
	agentHostIgnitionParams := models.HostIgnitionParams{Config: ""}
	if agent.Spec.IgnitionConfigOverrides != "" {
		agentHostIgnitionParams.Config = agent.Spec.IgnitionConfigOverrides
	}
	params := installer.V2UpdateHostIgnitionParams{
		InfraEnvID:         host.InfraEnvID,
		HostID:             strfmt.UUID(agent.Name),
		HostIgnitionParams: &agentHostIgnitionParams,
	}
	_, err := r.Installer.V2UpdateHostIgnitionInternal(ctx, params)

	log.Infof("Updated Agent Ignition %s %s", agent.Name, agent.Namespace)

	return err
}

func (r *AgentReconciler) updateIfNeeded(ctx context.Context, log logrus.FieldLogger, agent *aiv1beta1.Agent, internalHost *common.Host) (*common.Host, error) {
	spec := agent.Spec
	var err error
	returnedHost := internalHost

	err = r.updateInstallerArgs(ctx, log, internalHost, agent)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("Failed to update installer args")
		return internalHost, err
	}

	err = r.updateHostIgnition(ctx, log, internalHost, agent)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("Failed to update host ignition")
		return internalHost, err
	}

	hostUpdate := false
	params := &installer.V2UpdateHostParams{
		HostID:           *internalHost.ID,
		InfraEnvID:       internalHost.InfraEnvID,
		HostUpdateParams: &models.HostUpdateParams{},
	}
	if spec.Hostname != "" && spec.Hostname != internalHost.RequestedHostname {
		hostUpdate = true
		params.HostUpdateParams.HostName = &spec.Hostname
	}

	if spec.MachineConfigPool != "" && spec.MachineConfigPool != internalHost.MachineConfigPoolName {
		hostUpdate = true
		params.HostUpdateParams.MachineConfigPoolName = &spec.MachineConfigPool
	}

	if spec.Role != "" && spec.Role != internalHost.Role {
		hostUpdate = true
		role := string(spec.Role)
		params.HostUpdateParams.HostRole = &role
	}

	if spec.InstallationDiskID != "" && spec.InstallationDiskID != internalHost.InstallationDiskID {
		hostUpdate = true
		params.HostUpdateParams.DisksSelectedConfig = []*models.DiskConfigParams{
			{ID: &spec.InstallationDiskID, Role: models.DiskRoleInstall},
		}
	}

	if spec.IgnitionEndpointTokenReference != nil {
		var token string
		token, err = r.getIgnitionToken(ctx, agent.Spec.IgnitionEndpointTokenReference)
		if err != nil {
			log.WithError(err).Errorf("Failed to get ignition token")
			return internalHost, err
		}

		if token != internalHost.IgnitionEndpointToken {
			hostUpdate = true
			params.HostUpdateParams.IgnitionEndpointToken = &token
		}
	} else {
		if internalHost.IgnitionEndpointToken != "" {
			hostUpdate = true
			params.HostUpdateParams.IgnitionEndpointToken = swag.String("")
		}
	}

	if hostUpdate {
		var hostStatusesBeforeInstallationOrUnbound = []string{
			models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
			models.HostStatusInsufficient, models.HostStatusPendingForInput,
			models.HostStatusDisconnectedUnbound, models.HostStatusInsufficientUnbound, models.HostStatusDiscoveringUnbound,
			models.HostStatusKnownUnbound,
			models.HostStatusBinding,
		}
		if funk.ContainsString(hostStatusesBeforeInstallationOrUnbound, swag.StringValue(internalHost.Status)) {
			returnedHost, err = r.Installer.V2UpdateHostInternal(ctx, *params)

			if err != nil {
				log.WithError(err).Errorf("Failed to update host params %s %s", agent.Name, agent.Namespace)
				return internalHost, err
			}
			log.Infof("Updated host parameters for agent %s %s", agent.Name, agent.Namespace)
		}
	}
	if internalHost.Approved != spec.Approved {
		err = r.Installer.UpdateHostApprovedInternal(ctx, internalHost.InfraEnvID.String(), agent.Name, spec.Approved)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = common.NewApiError(http.StatusNotFound, err)
			}
			log.WithError(err).Errorf("Failed to approve Agent")
			return returnedHost, err
		}
		log.Infof("Updated Agent Approve %s %s", agent.Name, agent.Namespace)
	}
	log.Debugf("Updated Agent spec %s %s", agent.Name, agent.Namespace)

	return returnedHost, nil
}

func (r *AgentReconciler) getIgnitionToken(ctx context.Context, ignitionEndpointTokenReference *aiv1beta1.IgnitionEndpointTokenReference) (string, error) {
	secretRef := types.NamespacedName{Namespace: ignitionEndpointTokenReference.Namespace, Name: ignitionEndpointTokenReference.Name}
	secret, err := getSecret(ctx, r.Client, r.APIReader, secretRef)
	if err != nil {
		return "", errors.Wrap(err, "Failed to get user-data secret")
	}
	if err := ensureSecretIsLabelled(ctx, r.Client, secret, secretRef); err != nil {
		return "", errors.Wrap(err, "Failed to label user-data secret")
	}

	token, ok := secret.Data[common.IgnitionTokenKeyInSecret]
	if !ok {
		return "", errors.Errorf("secret %s did not contain key value", secretRef.Name)
	}
	return string(token), nil
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.Agent{}).
		Watches(&source.Channel{Source: r.CRDEventsHandler.GetAgentUpdates()},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
