package host

import "github.com/filanov/stateswitch"

const (
	TransitionTypeRegisterHost           = "RegisterHost"
	TransitionTypeHostInstallationFailed = "HostInstallationFailed"
	TransitionTypeCancelInstallation     = "CancelInstallation"
	TransitionTypeResetHost              = "ResetHost"
	TransitionTypeInstallHost            = "InstallHost"
)

func NewHostStateMachine(th *transitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
			HostStatusDiscovering,
			HostStatusKnown,
			HostStatusDisconnected,
			HostStatusInsufficient,
			HostStatusResetting,
		},
		DestinationState: HostStatusDiscovering,
		PostTransition:   th.PostRegisterHost,
	})

	// Register host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRegisterHost,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress},
		DestinationState: HostStatusError,
		PostTransition:   th.PostRegisterDuringInstallation,
	})

	// Installation failure
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeHostInstallationFailed,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress},
		DestinationState: HostStatusError,
		PostTransition:   th.PostHostInstallationFailed,
	})

	// Cancel installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeCancelInstallation,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress, HostStatusError},
		DestinationState: HostStatusError,
		PostTransition:   th.PostCancelInstallation,
	})

	// Reset host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeResetHost,
		SourceStates:     []stateswitch.State{HostStatusError},
		DestinationState: HostStatusResetting,
		PostTransition:   th.PostResetHost,
	})

	// Install host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeInstallHost,
		Condition:        th.IsValidRoleForInstallation,
		SourceStates:     []stateswitch.State{HostStatusKnown},
		DestinationState: HostStatusInstalling,
		PostTransition:   th.PostInstallHost,
	})

	// Install disabled host will not do anything
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeInstallHost,
		SourceStates:     []stateswitch.State{HostStatusDisabled},
		DestinationState: HostStatusDisabled,
	})

	return sm
}
