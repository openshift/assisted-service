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
	TransitionTypePrepareForInstallation     = "Prepare for installation"
	TransitionTypeRefresh                    = "RefreshHost"
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
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
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
		TransitionType: TransitionTypeResetHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusResetting),
		PostTransition:   th.PostResetHost,
	})

	// Install host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeInstallHost,
		SourceStates:     []stateswitch.State{stateswitch.State(models.HostStatusPreparingForInstallation)},
		DestinationState: stateswitch.State(models.HostStatusInstalling),
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

	// Prepare for installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypePrepareForInstallation,
		Condition:        th.IsValidRoleForInstallation,
		SourceStates:     []stateswitch.State{stateswitch.State(models.HostStatusKnown)},
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		PostTransition:   th.PostPrepareForInstallation,
	})

	// Refresh host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{HostStatusDiscovering, HostStatusInsufficient, HostStatusKnown,
			HostStatusPendingForInput, HostStatusDisconnected},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: HostStatusDisconnected,
		PostTransition:   th.PostRefreshHost(statusInfoDisconnected),
	})

	// Abort host if cluster has errors
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
		},
		Condition:        th.HasClusterError,
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoAbortingDueClusterErrors),
	})

	// Noop transitions for cluster error
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusInstalling),
		stateswitch.State(models.HostStatusInstallingInProgress),
		stateswitch.State(models.HostStatusInstalled),
	} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			Condition:        stateswitch.Not(th.HasClusterError),
			DestinationState: state,
		})
	}

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefresh,
		SourceStates:     []stateswitch.State{HostStatusDisconnected, HostStatusDiscovering},
		Condition:        stateswitch.And(If(IsConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: HostStatusDiscovering,
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
	})

	var hasMinRequiredHardware = stateswitch.And(If(HasMinValidDisks), If(HasMinCPUCores), If(HasMinMemory))

	var requiredInputFieldsExist = stateswitch.And(If(IsMachineCidrDefined), If(IsRoleDefined))

	var isSufficientForInstall = stateswitch.And(If(HasMemoryForRole), If(HasCPUCoresForRole), If(BelongsToMachineCidr),
		If(IsHostnameUnique), If(IsHostnameValid))

	// In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail.
	// This transition handles the case that a host does not pass minimum hardware requirements for any of the roles
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates:   []stateswitch.State{HostStatusDisconnected, HostStatusDiscovering, HostStatusInsufficient},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			stateswitch.Not(hasMinRequiredHardware)),
		DestinationState: HostStatusInsufficient,
		PostTransition:   th.PostRefreshHost(statusInfoInsufficientHardware),
	})

	// In order for this transition to be fired at least one of the validations in sufficientInputValidations must fail.
	// This transition handles the case that there is missing input that has to be provided from a user or other external means
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{HostStatusDisconnected, HostStatusDiscovering,
			HostStatusInsufficient, HostStatusKnown, HostStatusPendingForInput},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			stateswitch.Not(requiredInputFieldsExist)),
		DestinationState: HostStatusPendingForInput,
		PostTransition:   th.PostRefreshHost(statusInfoPendingForInput),
	})

	// In order for this transition to be fired at least one of the validations in sufficientForInstallValidations must fail.
	// This transition handles the case that one of the required validations that are required in order for the host
	// to be in known state (ready for installation) has failed
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{HostStatusDisconnected, HostStatusInsufficient, HostStatusPendingForInput,
			HostStatusDiscovering, HostStatusKnown},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			stateswitch.Not(isSufficientForInstall)),
		DestinationState: HostStatusInsufficient,
		PostTransition:   th.PostRefreshHost(statusInfoNotReadyForInstall),
	})

	// This transition is fired when all validations pass
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{HostStatusDisconnected, HostStatusInsufficient, HostStatusPendingForInput,
			HostStatusDiscovering, HostStatusKnown},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			isSufficientForInstall),
		DestinationState: HostStatusKnown,
		PostTransition:   th.PostRefreshHost(""),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefresh,
		SourceStates:     []stateswitch.State{stateswitch.State(models.HostStatusPreparingForInstallation)},
		Condition:        th.IsPreparingTimedOut,
		DestinationState: HostStatusError,
		PostTransition:   th.PostRefreshHost(statusInfoPreparingTimedOut),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefresh,
		SourceStates:     []stateswitch.State{stateswitch.State(models.HostStatusPreparingForInstallation)},
		Condition:        stateswitch.Not(th.IsPreparingTimedOut),
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
	})

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusDisabled),
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusResetting),
		stateswitch.State(models.HostStatusInstallingPendingUserAction),
		stateswitch.State(models.HostStatusResettingPendingUserAction)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	return sm
}
