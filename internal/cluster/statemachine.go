package cluster

import (
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/stateswitch"
)

const (
	TransitionTypeCancelInstallation     = "CancelInstallation"
	TransitionTypeResetCluster           = "ResetCluster"
	TransitionTypePrepareForInstallation = "PrepareForInstallation"
	TransitionTypeCompleteInstallation   = "CompleteInstallation"
)

func NewClusterStateMachine(th *transitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			clusterStatusInstalling,
			clusterStatusError,
		},
		DestinationState: clusterStatusError,
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetCluster,
		SourceStates: []stateswitch.State{
			clusterStatusError,
		},
		DestinationState: clusterStatusInsufficient,
		PostTransition:   th.PostResetCluster,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypePrepareForInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusReady),
		},
		DestinationState: stateswitch.State(models.ClusterStatusPreparingForInstallation),
		PostTransition:   th.PostPrepareForInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCompleteInstallation,
		Condition:      th.isSuccess,
		Transition: func(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
			params, _ := args.(*TransitionArgsCompleteInstallation)
			params.reason = statusInfoInstalled
			return nil
		},
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: clusterStatusInstalled,
		PostTransition:   th.PostCompleteInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCompleteInstallation,
		Condition:      th.notSuccess,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: clusterStatusError,
		PostTransition:   th.PostCompleteInstallation,
	})

	return sm
}
