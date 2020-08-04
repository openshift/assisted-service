package cluster

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeResetCluster               = "ResetCluster"
	TransitionTypePrepareForInstallation     = "PrepareForInstallation"
	TransitionTypeCompleteInstallation       = "CompleteInstallation"
	TransitionTypeHandlePreInstallationError = "Handle pre-installation-error"
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
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusError),
		},
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
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

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeHandlePreInstallationError,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
			stateswitch.State(models.ClusterStatusError),
		},
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostHandlePreInstallationError,
	})

	return sm
}
