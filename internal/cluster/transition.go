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
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type transitionHandler struct {
	log logrus.FieldLogger
	db  *gorm.DB
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
	if sCluster.srcState == clusterStatusError {
		return nil
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

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), params.db, sCluster,
		params.reason)
}

////////////////////////////////////////////////////////////////////////////
// Prepare for installation
////////////////////////////////////////////////////////////////////////////

type TransitionArgsPrepareForInstallation struct {
	ctx context.Context
	db  *gorm.DB
}

func (th *transitionHandler) PostPrepareForInstallation(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
	sCluster, ok := sw.(*stateCluster)
	if !ok {
		return errors.New("PostResetCluster incompatible type of StateSwitch")
	}
	params, ok := args.(*TransitionArgsPrepareForInstallation)
	if !ok {
		return errors.New("PostResetCluster invalid argument")
	}

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		statusInfoPreparingForInstallation, "install_started_at", strfmt.DateTime(time.Now()))
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

	return th.updateTransitionCluster(logutil.FromContext(params.ctx, th.log), th.db, sCluster,
		params.reason, "install_completed_at", strfmt.DateTime(time.Now()))
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
	conditions        map[validationID]bool
	validationResults map[string][]validationResult
	db                *gorm.DB
}

func If(id validationID) stateswitch.Condition {
	ret := func(sw stateswitch.StateSwitch, args stateswitch.TransitionArgs) (bool, error) {
		params, ok := args.(*TransitionArgsRefreshCluster)
		if !ok {
			return false, errors.Errorf("If(%s) invalid argument", id.String())
		}
		b, ok := params.conditions[id]
		if !ok {
			return false, errors.Errorf("If(%s) no such condition", id.String())
		}
		return b, nil
	}
	return ret
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
		//if status was changed - we need to send event and metrics
		if err == nil && updatedCluster != nil && sCluster.srcState != swag.StringValue(updatedCluster.Status) {
			msg := fmt.Sprintf("Updated status of cluster %s to %s", updatedCluster.Name, *updatedCluster.Status)
			params.eventHandler.AddEvent(params.ctx, updatedCluster.ID.String(), models.EventSeverityInfo, msg, time.Now(), updatedCluster.ID.String())
			//report installation finished metric if needed
			reportInstallationCompleteStatuses := []string{models.ClusterStatusInstalled, models.ClusterStatusError}
			if sCluster.srcState == models.ClusterStatusInstalling &&
				funk.ContainsString(reportInstallationCompleteStatuses, swag.StringValue(updatedCluster.Status)) {
				params.metricApi.ClusterInstallationFinished(logutil.FromContext(params.ctx, th.log), swag.StringValue(updatedCluster.Status),
					updatedCluster.OpenshiftVersion, updatedCluster.InstallStartedAt)
			}
			return nil
		}
		return err
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
