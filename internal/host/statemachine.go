package host

import (
	"strings"

	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
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
	TransitionTypeRefresh                    = "RefreshHost"
	TransitionTypeRegisterInstalledHost      = "RegisterInstalledHost"
)

func NewHostStateMachine(th *transitionHandler, log logrus.FieldLogger, disabledHostValidations []string) stateswitch.StateMachine {
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
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
	})

	// Disabled host can register if it was booted, no change in the state.
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRegisterHost,
		SourceStates:     []stateswitch.State{stateswitch.State(models.HostStatusDisabled)},
		DestinationState: stateswitch.State(models.HostStatusDisabled),
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
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRegisterDuringInstallation,
	})

	// Host in error should be able to register without changes.
	// if the registration return conflict or error then we have infinite number of events.
	// if the registration is blocked (403) it will break auto-reset feature.
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
	})

	// Register host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterInstalledHost,
		SourceStates: []stateswitch.State{
			"",
		},
		DestinationState: stateswitch.State(models.HostStatusInstalled),
		PostTransition:   th.PostRegisterInstalledHost,
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
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusCancelled),
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
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
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusResetting),
		PostTransition:   th.PostResetHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostResetHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
	})

	// Install host

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
		Condition:        th.IsDay2Host,
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
			stateswitch.State(models.HostStatusPreparingSuccessful),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
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

	// Refresh host

	// Prepare for installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		Condition:      stateswitch.And(If(ValidRoleForInstallation), If(IsConnected), If(ClusterPreparingForInstallation)),
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		PostTransition:   th.PostRefreshHost(statusInfoPreparingForInstallation),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(InstallationDiskSpeedCheckSuccessful), If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
		PostTransition:   th.PostRefreshHost(statusInfoHostPreparationSuccessful),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
	})

	// Install host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(ClusterInstalling)),
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostRefreshHost(statusInfoInstalling),
	})

	installationDiskSpeedCheckFailed := stateswitch.Not(If(SufficientOrUnknownInstallationDiskSpeed))
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), installationDiskSpeedCheckFailed),
		DestinationState: stateswitch.State(models.HostStatusInsufficient),
		PostTransition:   th.PostRefreshHost(statusInfoNotReadyForInstall),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(SufficientOrUnknownInstallationDiskSpeed), stateswitch.Not(If(ClusterPreparingForInstallation))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoClusterIsNotPreparing),
	})

	installationDiskSpeedUnknown := stateswitch.Not(stateswitch.Or(If(InstallationDiskSpeedCheckSuccessful), installationDiskSpeedCheckFailed))
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), installationDiskSpeedUnknown, If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), stateswitch.Not(stateswitch.Or(If(ClusterPreparingForInstallation), If(ClusterInstalling)))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoClusterIsNotPreparing),
	})

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
			stateswitch.State(models.HostStatusResettingPendingUserAction),
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
		},
		Condition:        stateswitch.And(th.HasClusterError, stateswitch.Not(th.IsDay2Host)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoAbortingDueClusterErrors),
	})

	// Time out while host installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling)},
		Condition:        th.HasInstallationTimedOut,
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationTimedOut),
	})

	// Connection time out while host preparing installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoConnectionTimedOut),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
		PostTransition:   th.PostRefreshHost(statusInfoConnectionTimedOut),
	})

	// Connection time out while host installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		Condition:        th.HostNotResponsiveWhileInstallation,
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoConnectionTimedOut),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition:        th.ShouldIgnoreInstallingInProgressTimeout,
		DestinationState: stateswitch.State(models.HostStatusInstallingInProgress),
		PostTransition:   th.PostRefreshHostRefreshStageUpdateTime,
	})

	// Time out while host installationInProgress
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition: stateswitch.And(
			th.HasInstallationInProgressTimedOut,
			stateswitch.Not(th.ShouldIgnoreInstallingInProgressTimeout)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationInProgressTimedOut),
	})

	// Noop transitions for cluster error
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusInstalling),
		stateswitch.State(models.HostStatusInstallingInProgress),
		stateswitch.State(models.HostStatusInstalled),
		stateswitch.State(models.HostStatusInstallingPendingUserAction),
		stateswitch.State(models.HostStatusResettingPendingUserAction),
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

	var hasMinRequiredHardware = stateswitch.And(filterHostStateMachine(disabledHostValidations, log, HasMinValidDisks, HasMinCPUCores, HasMinMemory, IsPlatformValid)...)

	var requiredInputFieldsExist = stateswitch.And(filterHostStateMachine(disabledHostValidations, log, IsMachineCidrDefined)...)

	var isSufficientForInstall = stateswitch.And(filterHostStateMachine(disabledHostValidations, log, HasMemoryForRole, HasCPUCoresForRole, BelongsToMachineCidr, IsHostnameUnique, IsHostnameValid, IsAPIVipConnected, BelongsToMajorityGroup,
		AreOcsRequirementsSatisfied, AreLsoRequirementsSatisfied, AreCnvRequirementsSatisfied, SufficientOrUnknownInstallationDiskSpeed)...)

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
			stateswitch.State(models.HostStatusKnown),
		},
		Condition: stateswitch.And(If(IsConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			isSufficientForInstall,
			stateswitch.Not(stateswitch.And(If(ClusterPreparingForInstallation), If(ValidRoleForInstallation)))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	// check timeout of log collection
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusCancelled)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Condition:        th.IsLogCollectionTimedOut,
			PostTransition:   th.PostRefreshLogsProgress(string(models.LogsStateTimeout)),
		})
	}

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusDisabled),
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusCancelled),
		stateswitch.State(models.HostStatusResetting),
	} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	// Noop transaction fro installed day2 on cloud hosts
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		Condition:        th.IsDay2Host,
		DestinationState: stateswitch.State(models.HostStatusAddedToExistingCluster),
	})

	return sm
}

func filterHostStateMachine(disabledValidations []string, log logrus.FieldLogger, validationIDs ...validationID) []stateswitch.Condition {
	f := funk.Filter(validationIDs, func(sid validationID) bool {
		for _, id := range disabledValidations {
			if string(sid) == id {
				return false
			}
		}
		return true
	})
	filtered := f.([]validationID)
	if len(disabledValidations) > 0 && len(filtered) == len(validationIDs) {
		d := funk.FilterString(disabledValidations, func(id string) bool {
			for _, vid := range validationIDs {
				if string(vid) == id {
					return false
				}
			}
			return true
		})
		log.Warnf("Unable to find host validation IDs: %s", strings.Join(d, ","))
	}
	ret := []stateswitch.Condition{}
	for _, sid := range filtered {
		ret = append(ret, If(sid))
	}
	return ret
}
