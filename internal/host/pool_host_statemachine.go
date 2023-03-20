package host

import (
	"fmt"

	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

func NewPoolHostStateMachine(sm stateswitch.StateMachine, th TransitionHandler) stateswitch.StateMachine {
	documentPoolStates(sm)
	documentPoolTransitionTypes(sm)

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
		},
		Condition:        th.IsUnboundHost,
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRegisterHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Initial registration",
			Description: "When new host is first registered. This transition is executed only on bound hosts because <unknown, TODO>",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscoveringUnbound),
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusInsufficientUnbound),
			stateswitch.State(models.HostStatusKnownUnbound),
			stateswitch.State(models.HostStatusUnbinding),
			stateswitch.State(models.HostStatusUnbindingPendingUserAction),
			stateswitch.State(models.HostStatusReclaimingRebooting),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRegisterHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Initial registration",
			Description: "When the host attempts to register while it's in one of the non-installation states. We move the host back to the discovering state instead of keeping it in its current state because we consider it a new host with potentially different hardware. See PostRegisterHost function",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeBindHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnownUnbound),
		},
		DestinationState: stateswitch.State(models.HostStatusBinding),
		PostTransition:   th.PostBindHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Bind host",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move host to disconnected when connected times out",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move to disconnected when virtual media disconnects pre-installation",
			Description: "TODO: Document this transition rule.",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnectedUnbound),
			stateswitch.State(models.HostStatusDiscoveringUnbound),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: stateswitch.State(models.HostStatusDiscoveringUnbound),
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host reconnected without inventory",
			Description: "TODO: Document this transition rule. Why is Discovering in the source states?",
		},
	})

	var hasMinRequiredHardware = stateswitch.And(If(HasMinValidDisks), If(HasMinCPUCores), If(HasMinMemory))
	sufficientToBeBound := stateswitch.And(hasMinRequiredHardware, If(IsHostnameValid))

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host has insufficient hardware",
			Description: "In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail. This transition handles the case that a host does not pass minimum hardware requirements for any of the roles",
		},
	})

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusBinding),
		stateswitch.State(models.HostStatusUnbinding),
	} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Refresh during %s should stay in %s", state, state),
				Description: "TODO: Document this transition rule.",
			},
		})
	}

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host ready",
			Description: "This transition is fired when all validations pass.",
		},
	})

	return sm
}

func documentPoolStates(sm stateswitch.StateMachine) {
	sm.DescribeState(stateswitch.State(models.HostStatusBinding), stateswitch.StateDoc{
		Name:        "Binding",
		Description: "Host is being bound to a cluster",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusDisconnectedUnbound), stateswitch.StateDoc{
		Name:        "Unbound Disconnected",
		Description: "Host is disconnected and not bound to a cluster",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusDiscoveringUnbound), stateswitch.StateDoc{
		Name:        "Unbound Discovering",
		Description: "Host is discovering and not bound to a cluster",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusInsufficientUnbound), stateswitch.StateDoc{
		Name:        "Unbound Insufficient",
		Description: "Host is unbound and insufficient. Hosts in this state do not meet all the requirements required for installation to start. In other words, hosts for which some of the validations which we deem required for installation have a negative status",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusKnownUnbound), stateswitch.StateDoc{
		Name:        "Unbound Unknown",
		Description: "TODO: Document this state",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusReclaimingRebooting), stateswitch.StateDoc{
		Name:        "Unbound Reclaiming Rebooting",
		Description: "TODO: Document this state",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusUnbinding), stateswitch.StateDoc{
		Name:        "Unbinding",
		Description: "TODO: Document this state",
	})

	sm.DescribeState(stateswitch.State(models.HostStatusUnbindingPendingUserAction), stateswitch.StateDoc{
		Name:        "Unbinding, Pending user action",
		Description: "TODO: Document this state",
	})
}

func documentPoolTransitionTypes(sm stateswitch.StateMachine) {
	sm.DescribeTransitionType(TransitionTypeBindHost, stateswitch.TransitionTypeDoc{
		Name:        "Bind Host",
		Description: "Triggered when a previously unbound host is bound to a cluster",
	})
	sm.DescribeTransitionType(TransitionTypeRefresh, stateswitch.TransitionTypeDoc{
		Name:        "Refresh",
		Description: "Triggered on some hosts periodically by the background host monitor goroutine that runs on the leader instance of the Assisted Service. Responsible for driving transitions between states that require re-evaluation of all the validation results and potential timeout conditions",
	})
	sm.DescribeTransitionType(TransitionTypeMediaDisconnect, stateswitch.TransitionTypeDoc{
		Name:        "Media Disconnect",
		Description: "Triggered when the a step response returned by the agent indicates that a virtual media disconnection has occurred",
	})
	sm.DescribeTransitionType(TransitionTypeRegisterHost, stateswitch.TransitionTypeDoc{
		Name:        "Register Host",
		Description: "Triggered when a host boots the discovery ISO and calls the Register API",
	})
}
