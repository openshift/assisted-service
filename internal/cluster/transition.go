package cluster

import (
	"context"
	"time"

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
	return updateClusterStateWithParams(logutil.FromContext(params.ctx, th.log), sCluster.srcState,
		params.reason, sCluster.cluster, params.db)
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
	return updateClusterStateWithParams(logutil.FromContext(params.ctx, th.log), sCluster.srcState,
		params.reason, sCluster.cluster, params.db)
}

////////////////////////////////////////////////////////////////////////////
// Prepare for installation
////////////////////////////////////////////////////////////////////////////

type TransitionArgsPrepareForInstallation struct {
	ctx context.Context
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
	return updateClusterStateWithParams(logutil.FromContext(params.ctx, th.log), sCluster.srcState,
		statusInfoPreparingForInstallation, sCluster.cluster, th.db,
		"install_started_at", strfmt.DateTime(time.Now()))
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

	return updateClusterStateWithParams(logutil.FromContext(params.ctx, th.log), sCluster.srcState,
		params.reason, sCluster.cluster, th.db, "install_completed_at", strfmt.DateTime(time.Now()))
}

func (th *transitionHandler) isSuccess(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) (b bool, err error) {
	params, _ := args.(*TransitionArgsCompleteInstallation)
	return params.isSuccess, nil
}

func (th *transitionHandler) notSuccess(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) (b bool, err error) {
	params, _ := args.(*TransitionArgsCompleteInstallation)
	return !params.isSuccess, nil
}
