package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var resetLogsField = []interface{}{"logs_info", "", "controller_logs_started_at", strfmt.DateTime(time.Time{}), "controller_logs_collected_at", strfmt.DateTime(time.Time{})}

type transitionHandler struct {
	log           logrus.FieldLogger
	db            *gorm.DB
	prepareConfig PrepareConfig
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

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), params.db, sCluster,
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

	//reset log fields and Openshift ClusterID when resetting the cluster
	extra := append(append(make([]interface{}, 0), "OpenshiftClusterID", ""), resetLogsField...)
	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), params.db, sCluster, params.reason, extra...)
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

	extra := append(append(make([]interface{}, 0), "install_started_at", strfmt.DateTime(time.Now())), resetLogsField...)
	err = th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		statusInfoPreparingForInstallation, extra...)
	if err != nil {
		th.log.WithError(err).Errorf("failed to reset fields in PostPrepareForInstallation on cluster %s", *sCluster.cluster.ID)
	}
	return err
}

////////////////////////////////////////////////////////////////////////////
// Update installation progress
////////////////////////////////////////////////////////////////////////////

type TransitionArgsUpdateInstallationProgress struct {
	ctx      context.Context
	progress string
}

func (th *transitionHandler) PostUpdateInstallationProgress(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)

	if !ok {
		return errors.New("PostUpdateInstallationProgress incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsUpdateInstallationProgress)
	if !ok {
		return errors.New("PostUpdateInstallationProgress invalid argument")
	}
	if cluster, err := updateClusterProgress(logutil.FromContext(params.ctx, th.log), th.db, *sCluster.cluster.ID, swag.StringValue(sCluster.cluster.Status),
		params.progress); err != nil {
		return err
	} else {
		sCluster.cluster = cluster
		return nil
	}
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
		true, createClusterCompletionStatusInfo(log, sCluster.cluster)); err != nil {
		return err
	} else {
		sCluster.cluster = cluster
		return nil
	}
}

func createClusterCompletionStatusInfo(log logrus.FieldLogger, cluster *common.Cluster) string {
	_, statuses := getClusterMonitoringOperatorsStatus(cluster)
	log.Infof("Cluster %s Monitoring status: %s", *cluster.ID, statuses)

	// Cluster status info is installed if no failed OLM operators
	if countOperatorsInAllStatuses(statuses[models.OperatorTypeOlm]) == 0 ||
		len(statuses[models.OperatorTypeOlm][models.OperatorStatusFailed]) == 0 {
		return statusInfoInstalled
	}

	statusInfo := StatusInfoDegraded
	statusInfo += ". Failed OLM operators: " + strings.Join(statuses[models.OperatorTypeOlm][models.OperatorStatusFailed], ", ")

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
		return false, err
	}
	if !exists {
		return false, nil
	}

	// TODO: MGMT-4458
	// Backward-compatible solution for clusters that don't have monitored operators data
	// Those clusters shouldn't finish until the controller would tell them.
	if len(sCluster.cluster.MonitoredOperators) == 0 {
		return true, nil
	}

	isComplete, _ := getClusterMonitoringOperatorsStatus(sCluster.cluster)
	return isComplete, nil
}

func getClusterMonitoringOperatorsStatus(cluster *common.Cluster) (bool, MonitoredOperatorStatuses) {
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
		operatorsStatuses[operator.OperatorType][operator.Status] = append(operatorsStatuses[operator.OperatorType][operator.Status], operator.Name)
	}

	return haveBuiltinOperatorsComplete(operatorsStatuses[models.OperatorTypeBuiltin]) &&
		haveOLMOperatorsComplete(operatorsStatuses[models.OperatorTypeOlm]), operatorsStatuses
}

func haveBuiltinOperatorsComplete(operatorsNamesByStatus OperatorsNamesByStatus) bool {
	// All the builtin operators are mandatory for a successfull installation
	return len(operatorsNamesByStatus[models.OperatorStatusAvailable]) == countOperatorsInAllStatuses(operatorsNamesByStatus)
}

func haveOLMOperatorsComplete(operatorsNamesByStatus OperatorsNamesByStatus) bool {
	// Need to wait for OLM operators to finish. Either available or failed. Failed operators would cause a degraded state.
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
	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		params.installErr.Error())
}

func (th *transitionHandler) updateTransitionCluster(log logrus.FieldLogger, db *gorm.DB, state *stateCluster,
	statusInfo string, extra ...interface{}) error {
	if cluster, err := updateClusterStatus(log, db, *state.cluster.ID, state.srcState,
		swag.StringValue(state.cluster.Status), statusInfo, extra...); err != nil {
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
	eventHandler      events.Handler
	metricApi         metrics.API
	hostApi           host.API
	conditions        map[string]bool
	validationResults map[string][]ValidationResult
	db                *gorm.DB
	objectHandler     s3wrapper.API
	clusterAPI        API
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

//check if we should move to finalizing state
func (th *transitionHandler) IsFinalizing(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	installedStatus := []string{models.HostStatusInstalled}

	// Move to finalizing state when 3 masters and at least 1 worker (if workers are given) moved to installed state
	if ok && th.enoughMastersAndWorkers(sCluster, installedStatus) {
		th.log.Infof("Cluster %s has at least required number of installed hosts, "+
			"cluster is finalizing.", sCluster.cluster.ID)
		return true, nil
	}
	return false, nil
}

//check if we should stay in installing state
func (th *transitionHandler) IsInstalling(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, _ := sw.(*stateCluster)
	installingStatuses := []string{models.HostStatusInstalling, models.HostStatusInstallingInProgress,
		models.HostStatusInstalled, models.HostStatusInstallingPendingUserAction}
	return th.enoughMastersAndWorkers(sCluster, installingStatuses), nil
}

//check if we should move to installing-pending-user-action state
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

func (th *transitionHandler) enoughMastersAndWorkers(sCluster *stateCluster, statuses []string) bool {
	mappedMastersByRole := MapMasterHostsByStatus(sCluster.cluster)
	mappedWorkersByRole := MapWorkersHostsByStatus(sCluster.cluster)
	mastersInSomeInstallingStatus := 0
	workersInSomeInstallingStatus := 0

	for _, status := range statuses {
		mastersInSomeInstallingStatus += len(mappedMastersByRole[status])
		workersInSomeInstallingStatus += len(mappedWorkersByRole[status])
	}

	numberOfExpectedWorkers := NumberOfWorkers(sCluster.cluster)
	minRequiredMasterNodes := MinMastersNeededForInstallation
	if swag.StringValue(sCluster.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		minRequiredMasterNodes = 1
	}

	// to be installed cluster need 3 master and at least 2 worker (if workers were given)
	if mastersInSomeInstallingStatus >= minRequiredMasterNodes &&
		(numberOfExpectedWorkers == 0 || workersInSomeInstallingStatus >= MinWorkersNeededForInstallation) {
		return true
	}
	return false
}

//check if prepare for installation reach to timeout
func (th *transitionHandler) IsPreparingTimedOut(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return false, errors.New("IsPreparingTimedOut incompatible type of StateSwitch")
	}
	// can happen if the service was rebooted or somehow the async part crashed.
	if time.Since(time.Time(sCluster.cluster.StatusUpdatedAt)) > th.prepareConfig.InstallationTimeout {
		return true, nil
	}
	return false, nil
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
			b              []byte
			err            error
			updatedCluster *common.Cluster
		)
		b, err = json.Marshal(&params.validationResults)
		if err != nil {
			return err
		}
		updatedCluster, err = updateClusterStatus(logutil.FromContext(params.ctx, th.log), params.db, *sCluster.cluster.ID, sCluster.srcState, *sCluster.cluster.Status,
			reason, "validations_info", string(b))
		//update hosts status to models.HostStatusResettingPendingUserAction if needed
		cluster := sCluster.cluster
		if updatedCluster != nil {
			cluster = updatedCluster
		}
		setPendingUserResetIfNeeded(params.ctx, logutil.FromContext(params.ctx, th.log), params.db, params.hostApi, cluster)

		if err != nil {
			return err
		}

		//if status was changed - we need to send event and metrics
		if updatedCluster != nil && sCluster.srcState != swag.StringValue(updatedCluster.Status) {
			msg := fmt.Sprintf("Updated status of cluster %s to %s", updatedCluster.Name, *updatedCluster.Status)
			params.eventHandler.AddEvent(params.ctx, *updatedCluster.ID, nil, models.EventSeverityInfo, msg, time.Now())
		}

		return nil
	}
	return ret
}

func (th *transitionHandler) PostRefreshLogsProgress(progress string) stateswitch.PostTransition {
	return func(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
		sCluster, _ := sw.(*stateCluster)
		return updateLogsProgress(th.log, th.db, sCluster.cluster, swag.StringValue(sCluster.cluster.Status), progress)
	}
}

//check if log collection on cluster level reached timeout
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
