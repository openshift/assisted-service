package host

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
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
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusResettingPendingUserAction),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
	})

	// Do nothing when host in reboot tries to register from resetting state.
	// On such cases cluster monitor is responsible to set the host state to
	// resetting-pending-user-action.
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusResetting),
		},
		Condition:        th.IsHostInReboot,
		DestinationState: stateswitch.State(models.HostStatusResetting),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusResetting),
		},
		Condition:        stateswitch.Not(th.IsHostInReboot),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
	})

	// Register host after reboot
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		Condition:      th.IsHostInReboot,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusInstallingPendingUserAction),
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
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRegisterDuringInstallation,
	})

	// Installation failure
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeHostInstallationFailed,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
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
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusCancelled),
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
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusResetting),
		PostTransition:   th.PostResetHost,
	})

	// Install host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeInstallHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostInstallHost,
	})

	// Install disabled host will not do anything
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeInstallHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisabled),
		},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
	})

	// Install day2 host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeInstallHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		Condition:        th.IsHostAddingToExistingCluster,
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostInstallHost,
	})

	// Disable host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeDisableHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
		},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
		PostTransition:   th.PostDisableHost,
	})

	// Enable host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeEnableHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisabled),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostEnableHost,
	})

	// Resetting pending user action
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResettingPendingUserAction,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusResetting),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusResettingPendingUserAction),
		PostTransition:   th.PostResettingPendingUserAction,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResettingPendingUserAction,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisabled),
		},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
	})

	// Prepare for installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypePrepareForInstallation,
		Condition:      th.IsValidRoleForInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		PostTransition:   th.PostPrepareForInstallation,
	})

	// Refresh host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDisconnected),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
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

	// Time out while host installationd
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling)},
		Condition:        stateswitch.And(th.HasInstallationTimedOut),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationTimedOut),
	})

	// Time out while host installationInProgress
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition:        stateswitch.And(th.HasInstallationInProgressTimedOut),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationInProgressTimedOut),
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
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
		},
		Condition:        stateswitch.And(If(IsConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
	})

	var hasMinRequiredHardware = stateswitch.And(If(HasMinValidDisks), If(HasMinCPUCores), If(HasMinMemory))

	var requiredInputFieldsExist = stateswitch.And(If(IsMachineCidrDefined))

	var isSufficientForInstall = stateswitch.And(If(HasMemoryForRole), If(HasCPUCoresForRole), If(BelongsToMachineCidr),
		If(IsHostnameUnique), If(IsHostnameValid), If(IsAPIVipConnected), If(BelongsToMajorityGroup))

	// In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail.
	// This transition handles the case that a host does not pass minimum hardware requirements for any of the roles
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
		},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			stateswitch.Not(hasMinRequiredHardware)),
		DestinationState: stateswitch.State(models.HostStatusInsufficient),
		PostTransition:   th.PostRefreshHost(statusInfoInsufficientHardware),
	})

	// In order for this transition to be fired at least one of the validations in sufficientInputValidations must fail.
	// This transition handles the case that there is missing input that has to be provided from a user or other external means
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
		},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			stateswitch.Not(requiredInputFieldsExist)),
		DestinationState: stateswitch.State(models.HostStatusPendingForInput),
		PostTransition:   th.PostRefreshHost(statusInfoPendingForInput),
	})

	// In order for this transition to be fired at least one of the validations in sufficientForInstallValidations must fail.
	// This transition handles the case that one of the required validations that are required in order for the host
	// to be in known state (ready for installation) has failed
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusKnown),
		},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			stateswitch.Not(isSufficientForInstall)),
		DestinationState: stateswitch.State(models.HostStatusInsufficient),
		PostTransition:   th.PostRefreshHost(statusInfoNotReadyForInstall),
	})

	// This transition is fired when all validations pass
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusKnown),
		},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			isSufficientForInstall),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        th.IsPreparingTimedOut,
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoPreparingTimedOut),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.Not(th.IsPreparingTimedOut),
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
	})

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusDisabled),
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusResetting),
		stateswitch.State(models.HostStatusInstallingPendingUserAction),
		stateswitch.State(models.HostStatusResettingPendingUserAction),
	} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	return sm
}
