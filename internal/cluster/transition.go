package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/stream"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var resetLogsField = []interface{}{"logs_info", "", "controller_logs_started_at", strfmt.DateTime(time.Time{}), "controller_logs_collected_at", strfmt.DateTime(time.Time{})}
var resetProgressFields = []interface{}{"progress_finalizing_stage_percentage", 0, "progress_installing_stage_percentage", 0,
	"progress_preparing_for_installation_stage_percentage", 0, "progress_total_percentage", 0}

var resetFields = append(append(resetProgressFields, resetLogsField...), "openshift_cluster_id", "")

type transitionHandler struct {
	log                 logrus.FieldLogger
	db                  *gorm.DB
	stream              stream.EventStreamWriter
	prepareConfig       PrepareConfig
	installationTimeout time.Duration
	finalizingTimeout   time.Duration
	eventsHandler       eventsapi.Handler
}

//go:generate mockgen -source=transition.go -package=cluster -destination=mock_transition.go
type TransitionHandler interface {
	PostCancelInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostResetCluster(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostPrepareForInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostCompleteInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostHandlePreInstallationError(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	IsFinalizing(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	IsInstalling(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	IsInstallingPendingUserAction(sw stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error)
	WithAMSSubscriptions(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	PostUpdateFinalizingAMSConsoleUrl(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	IsInstallationTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	IsFinalizingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	IsPreparingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	PostPreparingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostRefreshCluster(reason string) stateswitch.PostTransition
	InstallCluster(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error
	PostRefreshLogsProgress(progress string) stateswitch.PostTransition
	IsLogCollectionTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
	areAllHostsDone(sw stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error)
	hasClusterCompleteInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error)
}

////////////////////////////////////////////////////////////////////////////
// CancelInstallation
////////////////////////////////////////////////////////////////////////////

type TransitionArgsCancelInstallation struct {
	ctx    context.Context
	reason string
	db     *gorm.DB
}

func (th *transitionHandler) PostCancelInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostCancelInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsCancelInstallation)
	if !ok {
		return errors.New("PostCancelInstallation invalid argument")
	}

	return th.updateTransitionCluster(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, sCluster,
		params.reason)
}

////////////////////////////////////////////////////////////////////////////
// ResetCluster
////////////////////////////////////////////////////////////////////////////

type TransitionArgsResetCluster struct {
	ctx    context.Context
	reason string
	db     *gorm.DB
}

func (th *transitionHandler) PostResetCluster(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostResetCluster incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsResetCluster)
	if !ok {
		return errors.New("PostResetCluster invalid argument")
	}

	extra := resetFields[:]
	// reset api_vip and ingress_vip in case of resetting the SNO cluster
	if common.IsSingleNodeCluster(sCluster.cluster) {
		extra = append(extra, "api_vip", "", "ingress_vip", "")
	}

	return th.updateTransitionCluster(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, sCluster, params.reason, extra...)
}

////////////////////////////////////////////////////////////////////////////
// Prepare for installation
////////////////////////////////////////////////////////////////////////////

type TransitionArgsPrepareForInstallation struct {
	ctx                context.Context
	db                 *gorm.DB
	manifestsGenerator network.ManifestsGeneratorAPI
	metricApi          metrics.API
}

func (th *transitionHandler) PostPrepareForInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	var err error
	if !ok {
		return errors.New("PostPrepareForInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsPrepareForInstallation)
	if !ok {
		return errors.New("PostPrepareForInstallation invalid argument")
	}
	extra := append(append(make([]interface{}, 0), "install_started_at", strfmt.DateTime(time.Now()), "installation_preparation_completion_status", ""), resetLogsField...)
	err = th.updateTransitionCluster(params.ctx, logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		statusInfoPreparingForInstallation, extra...)
	if err != nil {
		th.log.WithError(err).Errorf("failed to reset fields in PostPrepareForInstallation on cluster %s", *sCluster.cluster.ID)
	}
	return err
}

////////////////////////////////////////////////////////////////////////////
// Complete installation
////////////////////////////////////////////////////////////////////////////

type OperatorsNamesByStatus map[models.OperatorStatus][]string
type MonitoredOperatorStatuses map[models.OperatorType]OperatorsNamesByStatus

func (th *transitionHandler) PostCompleteInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostCompleteInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return errors.New("PostCompleteInstallation invalid argument")
	}

	log := logutil.FromContext(params.ctx, th.log)
	if cluster, err := params.clusterAPI.CompleteInstallation(params.ctx, params.db, sCluster.cluster,
		true, th.createClusterCompletionStatusInfo(params.ctx, log, sCluster.cluster, th.eventsHandler)); err != nil {
		return err
	} else {
		sCluster.cluster = cluster
		params.updatedCluster = cluster
		return nil
	}
}

func (th *transitionHandler) createClusterCompletionStatusInfo(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster, eventHandler eventsapi.Handler) string {
	statusInfo := statusInfoInstalled

	_, statuses := th.getClusterMonitoringOperatorsStatus(cluster)
	log.Infof("Cluster %s Monitoring status: %s", *cluster.ID, statuses)

	// Check if the cluster is degraded. A cluster is degraded if not all requested OLM operators
	// are installed successfully. Then, check if all workers are successfully installed.
	if !(countOperatorsInAllStatuses(statuses[models.OperatorTypeOlm]) == 0 ||
		len(statuses[models.OperatorTypeOlm][models.OperatorStatusFailed]) == 0) {

		failedOperators := ". Failed OLM operators: " + strings.Join(statuses[models.OperatorTypeOlm][models.OperatorStatusFailed], ", ")
		eventgen.SendClusterDegradedOLMOperatorsFailedEvent(ctx, eventHandler, *cluster.ID, failedOperators)

		statusInfo = StatusInfoDegraded
		statusInfo += ". Failed OLM operators: " + strings.Join(statuses[models.OperatorTypeOlm][models.OperatorStatusFailed], ", ")
	} else {
		_, installedWorkers := HostsInStatus(cluster, []string{models.HostStatusInstalled})
		if installedWorkers < NumberOfWorkers(cluster) {
			statusInfo = StatusInfoNotAllWorkersInstalled
		}
	}
	return statusInfo
}

func (th *transitionHandler) hasClusterCompleteInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("hasClusterCompleteInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return false, errors.New("hasClusterCompleteInstallation invalid argument")
	}

	objectName := fmt.Sprintf("%s/%s", sCluster.cluster.ID, constants.Kubeconfig)
	exists, err := params.objectHandler.DoesObjectExist(params.ctx, objectName)
	if err != nil {
		th.log.Debugf("cluster %s: hasClusterCompleteInstallation condition returns false due to kubeconfig DoesObjectExist error", sCluster.cluster.ID)
		return false, err
	}
	if !exists {
		th.log.Debugf("cluster %s: hasClusterCompleteInstallation condition returns false due to kubeconfig DoesObjectExist", sCluster.cluster.ID)
		return false, nil
	}

	// TODO: MGMT-4458
	// Backward-compatible solution for clusters that don't have monitored operators data
	// Those clusters shouldn't finish until the controller would tell them.
	if len(sCluster.cluster.MonitoredOperators) == 0 {
		return true, nil
	}

	isComplete, _ := th.getClusterMonitoringOperatorsStatus(sCluster.cluster)
	th.log.Debugf("cluster %s, hasClusterCompleteInstallation condition returns isComplete: %t", sCluster.cluster.ID, isComplete)
	return isComplete, nil
}

func (th *transitionHandler) getClusterMonitoringOperatorsStatus(cluster *common.Cluster) (bool, MonitoredOperatorStatuses) {
	operatorsStatuses := MonitoredOperatorStatuses{
		models.OperatorTypeOlm: {
			models.OperatorStatusAvailable:   {},
			models.OperatorStatusProgressing: {},
			models.OperatorStatusFailed:      {},
		},
		models.OperatorTypeBuiltin: {
			models.OperatorStatusAvailable:   {},
			models.OperatorStatusProgressing: {},
			models.OperatorStatusFailed:      {},
		},
	}

	for _, operator := range cluster.MonitoredOperators {
		th.log.Debugf("cluster: %s, %s operator %s status is: %s ", cluster.ID.String(), operator.OperatorType, operator.Name, operator.Status)
		operatorsStatuses[operator.OperatorType][operator.Status] = append(operatorsStatuses[operator.OperatorType][operator.Status], operator.Name)
	}
	th.log.Debugf("cluster: %s, progress: %+v ", cluster.ID.String(), cluster.Progress)
	return th.haveBuiltinOperatorsComplete(cluster.ID, operatorsStatuses[models.OperatorTypeBuiltin]) &&
		th.haveOLMOperatorsComplete(cluster.ID, operatorsStatuses[models.OperatorTypeOlm]), operatorsStatuses
}

func (th *transitionHandler) haveBuiltinOperatorsComplete(clusterID *strfmt.UUID, operatorsNamesByStatus OperatorsNamesByStatus) bool {
	// All the builtin operators are mandatory for a successful installation
	builtInOperatorsInAllStatusesCount := countOperatorsInAllStatuses(operatorsNamesByStatus)
	th.log.Debugf("cluster: %s, OperatorStatusAvailable: %d, builtInOperatorsInAllStatusesCount is: %d",
		clusterID.String(), len(operatorsNamesByStatus[models.OperatorStatusAvailable]), builtInOperatorsInAllStatusesCount)
	return len(operatorsNamesByStatus[models.OperatorStatusAvailable]) == builtInOperatorsInAllStatusesCount
}

func (th *transitionHandler) haveOLMOperatorsComplete(clusterID *strfmt.UUID, operatorsNamesByStatus OperatorsNamesByStatus) bool {
	// Need to wait for OLM operators to finish. Either available or failed. Failed operators would cause a degraded state.
	OlmOperatorsInAllStatusesCount := countOperatorsInAllStatuses(operatorsNamesByStatus)
	th.log.Debugf("cluster: %s, OperatorStatusAvailable + OperatorStatusFailed: %d, builtInOperatorsInAllStatusesCount is: %d",
		clusterID.String(), len(operatorsNamesByStatus[models.OperatorStatusAvailable])+len(operatorsNamesByStatus[models.OperatorStatusFailed]),
		OlmOperatorsInAllStatusesCount)
	return len(operatorsNamesByStatus[models.OperatorStatusAvailable])+len(operatorsNamesByStatus[models.OperatorStatusFailed]) ==
		countOperatorsInAllStatuses(operatorsNamesByStatus)
}

func countOperatorsInAllStatuses(operatorNamesByStatus OperatorsNamesByStatus) int {
	sum := 0
	for _, v := range operatorNamesByStatus {
		sum += len(v)
	}

	return sum
}

////////////////////////////////////////////////////////////////////////////
// Handle pre-installation error
////////////////////////////////////////////////////////////////////////////

type TransitionArgsHandlePreInstallationError struct {
	ctx        context.Context
	installErr error
}

func (th *transitionHandler) PostHandlePreInstallationError(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, _ := sw.(*stateCluster)
	params, _ := args.(*TransitionArgsHandlePreInstallationError)
	return th.updateTransitionCluster(params.ctx, logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		params.installErr.Error())
}

func (th *transitionHandler) updateTransitionCluster(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, state *stateCluster,
	statusInfo string, extra ...interface{}) error {
	if cluster, err := updateClusterStatus(ctx, log, db, th.stream, *state.cluster.ID, state.srcState,
		swag.StringValue(state.cluster.Status), statusInfo, th.eventsHandler, extra...); err != nil {
		return err
	} else {
		state.cluster = cluster
		return nil
	}
}

////////////////////////////////////////////////////////////////////////////
// Refresh Cluster
////////////////////////////////////////////////////////////////////////////

type TransitionArgsRefreshCluster struct {
	ctx               context.Context
	eventHandler      eventsapi.Handler
	metricApi         metrics.API
	hostApi           host.API
	conditions        map[string]bool
	validationResults map[string][]ValidationResult
	db                *gorm.DB
	objectHandler     s3wrapper.API
	clusterAPI        API
	ocmClient         *ocm.Client
	dnsApi            dns.DNSApi
	updatedCluster    *common.Cluster
}

func If(id stringer) stateswitch.Condition {
	ret := func(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
		params, ok := args.(*TransitionArgsRefreshCluster)
		if !ok {
			return false, errors.Errorf("If(%s) invalid argument", id.String())
		}
		b, ok := params.conditions[id.String()]
		if !ok {
			return false, errors.Errorf("If(%s) no such condition", id.String())
		}
		return b, nil
	}
	return ret
}

// check if we should move to finalizing state
func (th *transitionHandler) IsFinalizing(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	installedStatus := []string{models.HostStatusInstalled}

	// Move to finalizing state when 3 masters and 0 or 2 worker (if workers are given) moved to installed state
	if ok && th.enoughMastersAndWorkers(sCluster, installedStatus) {
		th.log.Infof("Cluster %s has at least required number of installed hosts, "+
			"cluster is finalizing.", sCluster.cluster.ID)
		return true, nil
	}
	return false, nil
}

// check if we should stay in installing state
func (th *transitionHandler) IsInstalling(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, _ := sw.(*stateCluster)
	installingStatuses := []string{models.HostStatusInstalling, models.HostStatusInstallingInProgress,
		models.HostStatusInstalled, models.HostStatusInstallingPendingUserAction, models.HostStatusPreparingSuccessful}
	return th.enoughMastersAndWorkers(sCluster, installingStatuses), nil
}

// check if we should stay in installing state
func (th *transitionHandler) areAllHostsDone(sw stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error) {
	sCluster, _ := sw.(*stateCluster)
	doneStatuses := []string{models.HostStatusInstalled, models.HostStatusError}
	for _, h := range sCluster.cluster.Hosts {
		if !funk.ContainsString(doneStatuses, swag.StringValue(h.Status)) {
			return false, nil
		}
	}
	return true, nil
}

// check if we should move to installing-pending-user-action state
func (th *transitionHandler) IsInstallingPendingUserAction(
	sw stateswitch.StateSwitch,
	_ stateswitch.TransitionArgs,
) (bool, error) {
	sCluster, _ := sw.(*stateCluster)
	for _, h := range sCluster.cluster.Hosts {
		if swag.StringValue(h.Status) == models.HostStatusInstallingPendingUserAction {
			return true, nil
		}
	}
	return false, nil
}

func (th *transitionHandler) WithAMSSubscriptions(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("WithAMSSubscriptions incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return false, errors.New("WithAMSSubscriptions invalid argument")
	}
	if params.ocmClient != nil &&
		!sCluster.cluster.IsAmsSubscriptionConsoleUrlSet && params.clusterAPI.IsOperatorAvailable(sCluster.cluster, operators.OperatorConsole.Name) {
		return true, nil
	}
	th.log.Debugf("cluster %s, WithAMSSubscriptions condition ends and return false", sCluster.cluster.ID)
	return false, nil
}

func (th *transitionHandler) PostUpdateFinalizingAMSConsoleUrl(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostUpdateFinalizingAMSConsoleUrl incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return errors.New("PostUpdateFinalizingAMSConsoleUrl invalid argument")
	}
	log := logutil.FromContext(params.ctx, th.log)
	subscriptionID := sCluster.cluster.AmsSubscriptionID
	consoleUrl := common.GetConsoleUrl(sCluster.cluster.Name, sCluster.cluster.BaseDNSDomain)
	if err := params.ocmClient.AccountsMgmt.UpdateSubscriptionConsoleUrl(params.ctx, subscriptionID, consoleUrl); err != nil {
		log.WithError(err).Error("Failed to updated console-url in OCM")
		return err
	}
	isAmsSubscriptionConsoleUrlSetField := "is_ams_subscription_console_url_set"
	var (
		updatedCluster *common.Cluster
		err            error
	)
	if updatedCluster, err = UpdateCluster(params.ctx, log, th.db, th.stream, *sCluster.cluster.ID, sCluster.srcState, isAmsSubscriptionConsoleUrlSetField, true); err != nil {
		log.WithError(err).Errorf("Failed to update %s in cluster DB", isAmsSubscriptionConsoleUrlSetField)
		return err
	}
	params.updatedCluster = updatedCluster
	log.Infof("Updated console-url in AMS subscription with id %s", subscriptionID)
	return nil
}

func (th *transitionHandler) enoughMastersAndWorkers(sCluster *stateCluster, statuses []string) bool {
	mastersInSomeInstallingStatus, workersInSomeInstallingStatus := HostsInStatus(sCluster.cluster, statuses)

	minRequiredMasterNodes := MinMastersNeededForInstallation
	if swag.StringValue(sCluster.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		minRequiredMasterNodes = 1
	}

	numberOfExpectedWorkers := NumberOfWorkers(sCluster.cluster)
	minWorkersNeededForInstallation := 0
	if numberOfExpectedWorkers > 1 {
		minWorkersNeededForInstallation = 2
	}

	// to be installed cluster need 3 master
	// As for the workers, we need at least 2 workers when a cluster with 5 or more hosts is created
	// otherwise no minimum workers are required. This is because in the case of 4 or less hosts the
	// masters are set as schedulable and the workload can be shared across the available hosts. In the
	// case of a 5 nodes cluster, masters are not schedulable so we depend on the workers to run the
	// workload.
	if mastersInSomeInstallingStatus >= minRequiredMasterNodes &&
		(numberOfExpectedWorkers == 0 || workersInSomeInstallingStatus >= minWorkersNeededForInstallation) {
		return true
	}
	return false
}

// check if installation reach to timeout
func (th *transitionHandler) IsInstallationTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("IsInstallationTimedOut incompatible type of StateSwitch")
	}
	if time.Since(time.Time(sCluster.cluster.InstallStartedAt)) > th.installationTimeout {
		return true, nil
	}
	return false, nil
}

// check if finalizing reach to timeout
func (th *transitionHandler) IsFinalizingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("IsFinalizingTimedOut incompatible type of StateSwitch")
	}
	if time.Since(time.Time(sCluster.cluster.StatusUpdatedAt)) > th.finalizingTimeout {
		return true, nil
	}

	return false, nil
}

// check if prepare for installation reach to timeout
func (th *transitionHandler) IsPreparingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("IsPreparingTimedOut incompatible type of StateSwitch")
	}
	// can happen if the service was rebooted or somehow the async part crashed.
	if time.Since(time.Time(sCluster.cluster.StatusUpdatedAt)) > th.prepareConfig.PrepareForInstallationTimeout {
		return true, nil
	}
	return false, nil
}

func (th *transitionHandler) PostPreparingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostRefreshCluster incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return errors.New("PostRefreshCluster invalid argument")
	}

	var (
		err            error
		updatedCluster *common.Cluster
	)

	reason := statusInfoPreparingForInstallationTimeout
	if sCluster.srcState != swag.StringValue(sCluster.cluster.Status) || reason != swag.StringValue(sCluster.cluster.StatusInfo) {
		updatedCluster, err = updateClusterStatus(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, th.stream, *sCluster.cluster.ID, sCluster.srcState, *sCluster.cluster.Status,
			reason, params.eventHandler)
	}

	//update hosts status to models.HostStatusResettingPendingUserAction if needed
	cluster := sCluster.cluster
	if updatedCluster != nil {
		cluster = updatedCluster
		params.updatedCluster = updatedCluster
	}
	setPendingUserResetIfNeeded(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, params.hostApi, cluster)

	if err != nil {
		return err
	}

	eventgen.SendInstallationPreparingTimedOutEvent(params.ctx, params.eventHandler, *cluster.ID)

	return nil
}

// Return a post transition function with a constant reason
func (th *transitionHandler) PostRefreshCluster(reason string) stateswitch.PostTransition {
	ret := func(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
		sCluster, ok := sw.(*stateCluster)
		if !ok {
			return errors.New("PostRefreshCluster incompatible type of StateSwitch")
		}
		params, ok := args.(*TransitionArgsRefreshCluster)
		if !ok {
			return errors.New("PostRefreshCluster invalid argument")
		}

		var (
			err            error
			updatedCluster *common.Cluster
		)
		//update cluster record if the state or the reason has changed
		if sCluster.srcState != swag.StringValue(sCluster.cluster.Status) || reason != swag.StringValue(sCluster.cluster.StatusInfo) {
			var extra []interface{}
			var log = logutil.FromContext(params.ctx, th.log)
			extra, err = addExtraParams(log, sCluster.cluster, sCluster.srcState)
			if err != nil {
				return err
			}
			updatedCluster, err = updateClusterStatus(params.ctx, log, params.db, th.stream, *sCluster.cluster.ID, sCluster.srcState, *sCluster.cluster.Status,
				reason, params.eventHandler, extra...)
			if err != nil {
				return err
			}
		}

		//update hosts status to models.HostStatusResettingPendingUserAction if needed
		cluster := sCluster.cluster
		if updatedCluster != nil {
			cluster = updatedCluster
			params.updatedCluster = updatedCluster
		}
		setPendingUserResetIfNeeded(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, params.hostApi, cluster)

		if err != nil {
			return err
		}

		//report cluster install duration metrics in case of an installation halt. Cancel and Installed cases are
		//treated separately in CancelInstallation and CompleteInstallation respectively
		if sCluster.srcState != swag.StringValue(sCluster.cluster.Status) &&
			sCluster.srcState != models.ClusterStatusInstallingPendingUserAction &&
			funk.ContainsString([]string{models.ClusterStatusError, models.ClusterStatusInstallingPendingUserAction}, swag.StringValue(sCluster.cluster.Status)) {

			params.metricApi.ClusterInstallationFinished(params.ctx, *sCluster.cluster.Status, sCluster.srcState,
				sCluster.cluster.OpenshiftVersion, *sCluster.cluster.ID, sCluster.cluster.EmailDomain,
				sCluster.cluster.InstallStartedAt)
		}
		return nil
	}
	return ret
}

func (th *transitionHandler) InstallCluster(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("InstallCluster incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsRefreshCluster)
	if !ok {
		return errors.New("InstallCluster invalid argument")
	}
	cluster := sCluster.cluster
	ctx := params.ctx
	if err := params.dnsApi.CreateDNSRecordSets(ctx, cluster); err != nil {
		return err
	}
	// send metric and event that installation process has been started
	params.metricApi.InstallationStarted()
	return nil
}

func (th *transitionHandler) PostRefreshLogsProgress(progress string) stateswitch.PostTransition {
	return func(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
		sCluster, _ := sw.(*stateCluster)
		return updateLogsProgress(th.log, th.db, sCluster.cluster, progress)
	}
}

// check if log collection on cluster level reached timeout
func (th *transitionHandler) IsLogCollectionTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		th.log.Error("IsLogCollectionTimedOut incompatible type of StateSwitch")
		return false, errors.New("Cluster IsLogCollectionTimedOut incompatible type of StateSwitch")
	}

	// if we transitioned to the state before the logs were collected, check the timeout
	// from the time the state machine entered the state
	if sCluster.cluster.LogsInfo == models.LogsStateEmpty && time.Time(sCluster.cluster.ControllerLogsStartedAt).IsZero() {
		return time.Since(time.Time(sCluster.cluster.StatusUpdatedAt)) > th.prepareConfig.LogPendingTimeout, nil
	}

	// if logs are requested, but not collected at all (e.g. controller failed to start or could not send back the
	// logs due to networking error) then check the timeout from the time logs were expected
	if sCluster.cluster.LogsInfo == models.LogsStateRequested && time.Time(sCluster.cluster.ControllerLogsCollectedAt).IsZero() {
		return time.Since(time.Time(sCluster.cluster.ControllerLogsStartedAt)) > th.prepareConfig.LogCollectionTimeout, nil
	}

	// if logs are requested, and some logs were collected (e.g. must-gather takes too long to collect)
	// check the timeout from the time logs were last collected
	if sCluster.cluster.LogsInfo == models.LogsStateRequested && !time.Time(sCluster.cluster.ControllerLogsCollectedAt).IsZero() {
		return time.Since(time.Time(sCluster.cluster.ControllerLogsCollectedAt)) > th.prepareConfig.LogCollectionTimeout, nil
	}

	// if logs are uploaded but not completed (e.g. controller was crashed mid action or request was lost)
	// check the timeout from the last time the log were collected
	if sCluster.cluster.LogsInfo == models.LogsStateCollecting {
		return time.Since(time.Time(sCluster.cluster.ControllerLogsCollectedAt)) > th.prepareConfig.LogCollectionTimeout, nil
	}

	return false, nil
}

func setPendingUserResetIfNeeded(ctx context.Context, log logrus.FieldLogger, db *gorm.DB, hostApi host.API, c *common.Cluster) {
	if swag.StringValue(c.Status) == models.ClusterStatusInsufficient {
		if isPendingUserResetRequired(hostApi, c) {
			log.Infof("Setting cluster: %s hosts to status: %s",
				c.ID, models.HostStatusInstallingPendingUserAction)
			if err := setPendingUserReset(ctx, c, db, hostApi); err != nil {
				log.Errorf("failed setting cluster: %s hosts to status: %s",
					c.ID, models.HostStatusInstallingPendingUserAction)
			}
		}
	}
}

func isPendingUserResetRequired(hostAPI host.API, c *common.Cluster) bool {
	for _, h := range c.Hosts {
		if hostAPI.IsRequireUserActionReset(h) {
			return true
		}
	}
	return false
}

func setPendingUserReset(ctx context.Context, c *common.Cluster, db *gorm.DB, hostAPI host.API) error {
	txSuccess := false
	tx := db.Begin()
	defer func() {
		if !txSuccess {
			tx.Rollback()
		}
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, h := range c.Hosts {
		if err := hostAPI.ResetPendingUserAction(ctx, h, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	txSuccess = true
	return nil
}

// This function initialize the progress bar in the transition between prepare for installing to installing
// all other progress settings should be initiated from the cluster status API
func initProgressParamsInstallingStage() []interface{} {
	preparingForInstallationStagePercentage := int64(100)
	totalPercentage := int64(common.ProgressWeightPreparingForInstallationStage * float64(preparingForInstallationStagePercentage))
	return []interface{}{"progress_preparing_for_installation_stage_percentage", preparingForInstallationStagePercentage,
		"progress_total_percentage", totalPercentage}
}

func addExtraParams(log logrus.FieldLogger, cluster *common.Cluster, srcState string) ([]interface{}, error) {
	extra := []interface{}{}
	switch swag.StringValue(cluster.Status) {
	case models.ClusterStatusInstalling:
		// In case of SNO cluster, set api_vip and ingress_vip with host ip
		if common.IsSingleNodeCluster(cluster) {
			hostIP, err := network.GetIpForSingleNodeInstallation(cluster, log)
			if err != nil {
				log.WithError(err).Errorf("Failed to find host ip for single node installation")
				return nil, err
			}
			extra = append(make([]interface{}, 0), "api_vip", hostIP, "ingress_vip", hostIP)
		}
		if srcState == models.ClusterStatusPreparingForInstallation {
			extra = append(extra, initProgressParamsInstallingStage()...)
		}
	}
	return extra, nil
}
