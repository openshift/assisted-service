package cluster

import (
	"github.com/filanov/stateswitch"
)

const (
	TransitionTypeCancelInstallation = "CancelInstallation"
	TransitionTypeResetCluster       = "ResetCluster"
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

	return sm
}
