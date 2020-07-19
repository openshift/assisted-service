package host

import (
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/stateswitch"
)

const (
	TransitionTypeRegisterHost               = "RegisterHost"
	TransitionTypeHostInstallationFailed     = "HostInstallationFailed"
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeResetHost                  = "ResetHost"
	TransitionTypeInstallHost                = "InstallHost"
	TransitionTypeDisableHost                = "DisableHost"
	TransitionTypeEnableHost                 = "EnableHost"
	TransitionTypeResettingPendingUserAction = "ResettingPendingUserAction"
)

func NewHostStateMachine(th *transitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	// Register host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
			HostStatusDiscovering,
			HostStatusKnown,
			HostStatusDisconnected,
			HostStatusInsufficient,
			HostStatusResetting,
			stateswitch.State(models.HostStatusResettingPendingUserAction),
		},
		DestinationState: HostStatusDiscovering,
		PostTransition:   th.PostRegisterHost,
	})

	// Register host after reboot
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRegisterHost,
		Condition:        th.IsHostInReboot,
		SourceStates:     []stateswitch.State{HostStatusInstallingInProgress},
		DestinationState: HostStatusInstallingPendingUserAction,
		PostTransition:   th.PostRegisterDuringReboot,
	})

	// Register host during installation
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

	// Cancel installation - disabled host (do nothing)
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisabled),
		},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
	})

	// Cancel installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeCancelInstallation,
		SourceStates:     []stateswitch.State{HostStatusInstalling, HostStatusInstallingInProgress, HostStatusError},
		DestinationState: HostStatusError,
		PostTransition:   th.PostCancelInstallation,
	})

	// Reset disabled host (do nothing)
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisabled),
		},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
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

	// Install insufficient host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeInstallHost,
		SourceStates:     []stateswitch.State{HostStatusInsufficient},
		DestinationState: HostStatusInstalling,
		PostTransition:   th.PostInstallHost,
	})

	// Install disabled host will not do anything
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeInstallHost,
		SourceStates:     []stateswitch.State{HostStatusDisabled},
		DestinationState: HostStatusDisabled,
	})

	// Disable host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeDisableHost,
		SourceStates: []stateswitch.State{
			HostStatusDisconnected,
			HostStatusDiscovering,
			HostStatusInsufficient,
			HostStatusKnown,
		},
		DestinationState: HostStatusDisabled,
		PostTransition:   th.PostDisableHost,
	})

	// Enable host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeEnableHost,
		SourceStates: []stateswitch.State{
			HostStatusDisabled,
		},
		DestinationState: HostStatusDiscovering,
		PostTransition:   th.PostEnableHost,
	})

	// Resetting pending user action
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResettingPendingUserAction,
		SourceStates: []stateswitch.State{
			HostStatusResetting,
		},
		DestinationState: stateswitch.State(models.HostStatusResettingPendingUserAction),
		PostTransition:   th.PostResettingPendingUserAction,
	})
	return sm
}
