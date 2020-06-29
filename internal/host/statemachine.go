package host

import "github.com/filanov/stateswitch"

const (
	TransitionTypeRegisterHost           = "RegisterHost"
	TransitionTypeHostInstallationFailed = "HostInstallationFailed"
	TransitionTypeCancelInstallation     = "CancelInstallation"
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
		},
		DestinationState: HostStatusDiscovering,
		PostTransition:   th.PostRegisterHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRegisterHost,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress},
		DestinationState: HostStatusError,
		PostTransition:   th.PostRegisterDuringInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeHostInstallationFailed,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress},
		DestinationState: HostStatusError,
		PostTransition:   th.PostHostInstallationFailed,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeCancelInstallation,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress, HostStatusError},
		DestinationState: HostStatusError,
		PostTransition:   th.PostCancelInstallation,
	})

	return sm
}
