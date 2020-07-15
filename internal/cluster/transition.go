package cluster

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
	return updateClusterStatus(logutil.FromContext(params.ctx, th.log), params.db, *sCluster.cluster.ID, sCluster.srcState,
		*sCluster.cluster.Status, params.reason)
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
	return updateClusterStatus(logutil.FromContext(params.ctx, th.log), params.db, *sCluster.cluster.ID, sCluster.srcState,
		*sCluster.cluster.Status, params.reason)
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

	return updateClusterStatus(logutil.FromContext(params.ctx, th.log), th.db, *sCluster.cluster.ID, sCluster.srcState,
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

	return updateClusterStatus(logutil.FromContext(params.ctx, th.log), th.db, *sCluster.cluster.ID, sCluster.srcState,
		*sCluster.cluster.Status, params.reason,
		"install_completed_at", strfmt.DateTime(time.Now()))
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
	return updateCluster(logutil.FromContext(params.ctx, th.log), sCluster.srcState,
		params.installErr.Error(), sCluster.cluster, th.db)
}

// Updates the status according to the cluster where the status equals the srcStatus
func updateCluster(log logrus.FieldLogger, srcStatus, statusInfo string, c *common.Cluster, db *gorm.DB,
	extra ...interface{}) error {
	newStatus := c.Status
	c.Status = &srcStatus
	return updateClusterStatus(log, db, *c.ID, *c.Status, *newStatus, statusInfo, extra...)
}
