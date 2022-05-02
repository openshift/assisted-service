package host

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

func NewPoolHostStateMachine(sm stateswitch.StateMachine, th *transitionHandler) stateswitch.StateMachine {

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
		},
		Condition:        th.IsUnboundHost,
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRegisterHost,
	})

	// Register host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
			stateswitch.State(models.HostStatusUnbinding),
			stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRegisterHost,
	})

	// Bind host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeBindHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnownUnbound),
		},
		DestinationState: stateswitch.State(models.HostStatusBinding),
		PostTransition:   th.PostBindHost,
	})

	// Refresh host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusUnbinding),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusDisconnectedUnbound),
		PostTransition:   th.PostRefreshHost(statusInfoDisconnected),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeMediaDisconnect,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusUnbinding),
		},
		DestinationState: stateswitch.State(models.HostStatusDisconnectedUnbound),
		PostTransition:   th.PostHostMediaDisconnected,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusDiscoveringUnbound),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
	})

	var hasMinRequiredHardware = stateswitch.And(If(HasMinValidDisks), If(HasMinCPUCores), If(HasMinMemory))
	sufficientToBeBound := stateswitch.And(hasMinRequiredHardware, If(IsHostnameValid), If(IsNTPSynced))

	// In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail.
	// This transition handles the case that a host does not pass minimum hardware requirements for any of the roles
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
		},
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
			stateswitch.Not(sufficientToBeBound)),
		DestinationState: stateswitch.State(models.HostStatusInsufficientUnbound),
		PostTransition:   th.PostRefreshHost(statusInfoInsufficientHardware),
	})

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusBinding),
		stateswitch.State(models.HostStatusUnbinding),
	} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
		},
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
			sufficientToBeBound),
		DestinationState: stateswitch.State(models.HostStatusKnownUnbound),
		PostTransition:   th.PostRefreshHost(statusInfoHostReadyToBeBound),
	})

	return sm
}
