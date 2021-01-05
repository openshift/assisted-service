package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

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

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), params.db, sCluster, params.reason,
		"ControllerLogsCollectedAt", strfmt.DateTime(time.Time{}),
		"OpenshiftClusterID", "", // reset Openshift cluster ID when resetting
	)
}

////////////////////////////////////////////////////////////////////////////
// Prepare for installation
////////////////////////////////////////////////////////////////////////////

type TransitionArgsPrepareForInstallation struct {
	ctx      context.Context
	db       *gorm.DB
	ntpUtils network.NtpUtilsAPI
}

func (th *transitionHandler) PostPrepareForInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostPrepareForInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsPrepareForInstallation)
	if !ok {
		return errors.New("PostPrepareForInstallation invalid argument")
	}

	if err := params.ntpUtils.AddChronyManifest(params.ctx, logutil.FromContext(params.ctx, th.log), sCluster.cluster); err != nil {
		return errors.Wrap(err, "PostPrepareForInstallation failed to add chrony manifest")
	}

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		statusInfoPreparingForInstallation, "install_started_at", strfmt.DateTime(time.Now()),
		"controller_logs_collected_at", strfmt.DateTime(time.Time{}))
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

type TransitionArgsCompleteInstallation struct {
	ctx       context.Context
	isSuccess bool
	reason    string
}

func (th *transitionHandler) PostCompleteInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostCompleteInstallation incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsCompleteInstallation)
	if !ok {
		return errors.New("PostCompleteInstallation invalid argument")
	}

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster, params.reason)
}

func (th *transitionHandler) isSuccess(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) (b bool, err error) {
	params, _ := args.(*TransitionArgsCompleteInstallation)
	return params.isSuccess, nil
}

func (th *transitionHandler) notSuccess(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) (b bool, err error) {
	params, _ := args.(*TransitionArgsCompleteInstallation)
	return !params.isSuccess, nil
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
	validationResults map[string][]validationResult
	db                *gorm.DB
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

	// to be installed cluster need 3 master and at least 1 worker(if workers were given)
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
			//report installation finished metric if needed
			reportInstallationCompleteStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError}
			if sCluster.srcState == models.ClusterStatusInstalling &&
				funk.ContainsString(reportInstallationCompleteStatuses, swag.StringValue(updatedCluster.Status)) {
				params.metricApi.ClusterInstallationFinished(logutil.FromContext(params.ctx, th.log), swag.StringValue(updatedCluster.Status),
					updatedCluster.OpenshiftVersion, *updatedCluster.ID, updatedCluster.EmailDomain, updatedCluster.InstallStartedAt)
			}
		}

		return nil
	}
	return ret
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
