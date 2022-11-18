package host

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeRegisterHost               = "RegisterHost"
	TransitionTypeHostInstallationFailed     = "HostInstallationFailed"
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeInstallHost                = "InstallHost"
	TransitionTypeResettingPendingUserAction = "ResettingPendingUserAction"
	TransitionTypeRefresh                    = "RefreshHost"
	TransitionTypeMediaDisconnect            = "MediaDisconnect"
	TransitionTypeBindHost                   = "BindHost"
	TransitionTypeUnbindHost                 = "UnbindHost"
	TransitionTypeReclaimHost                = "ReclaimHost"
	TransitionTypeRebootingForReclaim        = "RebootingForReclaim"
	TransitionTypeReclaimFailed              = "ReclaimHostFailed"
)

// func NewHostStateMachine(th *transitionHandler) stateswitch.StateMachine {
func NewHostStateMachine(sm stateswitch.StateMachine, th TransitionHandler) stateswitch.StateMachine {

	// Register host by late binding
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
		},
		Condition:        stateswitch.Not(th.IsUnboundHost),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
	})

	// Register host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusResettingPendingUserAction),
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
			stateswitch.State(models.HostStatusBinding),
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
		Condition:      stateswitch.Or(th.IsHostInReboot, stateswitch.And(th.IsDay2Host, th.IsHostInDone)),
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

	// Install host

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
			stateswitch.State(models.HostStatusPreparingFailed),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusResettingPendingUserAction),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusResettingPendingUserAction),
		PostTransition:   th.PostResettingPendingUserAction,
	})

	// Unbind host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeUnbindHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusPendingForInput),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbinding),
		PostTransition:   th.PostUnbindHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeUnbindHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
	})

	// ReclaimHost when installed moves to Reclaiming
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusReclaiming),
		PostTransition:   th.PostUnbindHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRebootingForReclaim,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
		},
		DestinationState: stateswitch.State(models.HostStatusReclaimingRebooting),
		PostTransition:   th.PostReclaim,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimFailed,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
			stateswitch.State(models.HostStatusReclaimingRebooting),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
			stateswitch.State(models.HostStatusReclaimingRebooting),
		},
		Condition:        th.HasStatusTimedOut(ReclaimTimeout),
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostRefreshReclaimTimeout,
	})

	// ReclaimHost in other states acts like Unbind
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusPendingForInput),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbinding),
		PostTransition:   th.PostUnbindHost,
	})
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
	})

	// Refresh host

	// Prepare for installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		Condition:      stateswitch.And(If(ValidRoleForInstallation), If(IsConnected), If(IsMediaConnected), If(ClusterPreparingForInstallation)),
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		PostTransition:   th.PostPreparingForInstallationHost,
	})

	// Unknown validations
	installationDiskSpeedUnknown := stateswitch.And(stateswitch.Not(If(InstallationDiskSpeedCheckSuccessful)), If(SufficientOrUnknownInstallationDiskSpeed))
	imagesAvailabilityUnknown := stateswitch.And(stateswitch.Not(If(SuccessfulContainerImageAvailability)), If(SucessfullOrUnknownContainerImagesAvailability))

	// All validations are successful
	allConditionsSuccessful := stateswitch.And(If(InstallationDiskSpeedCheckSuccessful), If(SuccessfulContainerImageAvailability))

	// All validations are successful, or were not evaluated
	allConditionsSuccessfulOrUnknown := stateswitch.And(If(SufficientOrUnknownInstallationDiskSpeed), If(SucessfullOrUnknownContainerImagesAvailability))

	// At least one of the validations has not been evaluated and there are no failed validations
	atLeastOneConditionUnknown := stateswitch.And(stateswitch.Or(installationDiskSpeedUnknown, imagesAvailabilityUnknown), allConditionsSuccessfulOrUnknown)

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), allConditionsSuccessful, If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
		PostTransition:   th.PostRefreshHost(statusInfoHostPreparationSuccessful),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
	})

	// Install host
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), If(ClusterInstalling)),
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostRefreshHost(statusInfoInstalling),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), allConditionsSuccessfulOrUnknown, stateswitch.Not(If(ClusterPreparingForInstallation))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingFailed),
			stateswitch.State(models.HostStatusKnown),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(SufficientOrUnknownInstallationDiskSpeed))),
		DestinationState: stateswitch.State(models.HostStatusInsufficient),
		PostTransition:   th.PostRefreshHost(statusInfoNotReadyForInstall),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(SucessfullOrUnknownContainerImagesAvailability))),
		DestinationState: stateswitch.State(models.HostStatusPreparingFailed),
		PostTransition:   th.PostRefreshHost(statusInfoHostPreparationFailure),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), atLeastOneConditionUnknown, If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingFailed),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(ClusterPreparingForInstallation))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(stateswitch.Or(If(ClusterPreparingForInstallation), If(ClusterInstalling)))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusPreparingFailed),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
		PostTransition:   th.PostRefreshHost(statusInfoDisconnected),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeMediaDisconnect,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusBinding),
		},
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
		PostTransition:   th.PostHostMediaDisconnected,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeMediaDisconnect,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingFailed),
			stateswitch.State(models.HostStatusPreparingSuccessful),
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostHostMediaDisconnected,
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
		Condition:        stateswitch.And(If(ClusterInError), stateswitch.Not(th.IsDay2Host)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoAbortingDueClusterErrors),
	})

	// Time out while host installation
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling)},
		Condition:        th.HasStatusTimedOut(InstallationTimeout),
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
	shouldIgnoreInstallationProgressTimeout := stateswitch.And(If(StageInWrongBootStages), If(ClusterPendingUserAction))

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition:        shouldIgnoreInstallationProgressTimeout,
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
			stateswitch.Not(th.IsHostInReboot),
			stateswitch.Not(shouldIgnoreInstallationProgressTimeout)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationInProgressTimedOut),
	})

	// Time out while host is rebooting
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition: stateswitch.And(
			th.HasInstallationInProgressTimedOut,
			th.IsHostInReboot),
		DestinationState: stateswitch.State(models.HostStatusInstallingPendingUserAction),
		PostTransition:   th.PostRefreshHost(statusRebootTimeout),
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
			Condition:        stateswitch.Not(If(ClusterInError)),
			DestinationState: state,
		})
	}

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
	})

	var hasMinRequiredHardware = stateswitch.And(
		If(HasMinValidDisks),
		If(HasMinCPUCores),
		If(HasMinMemory),
		If(CompatibleWithClusterPlatform),
		If(DiskEncryptionRequirementsSatisfied),
	)

	var requiredInputFieldsExist = stateswitch.And(
		If(IsMachineCidrDefined),
	)

	var isSufficientForInstall = stateswitch.And(
		If(HasMemoryForRole),
		If(HasCPUCoresForRole),
		If(BelongsToMachineCidr),
		If(IsHostnameUnique),
		If(IsHostnameValid),
		If(IsIgnitionDownloadable),
		If(BelongsToMajorityGroup),
		If(AreOdfRequirementsSatisfied),
		If(AreLsoRequirementsSatisfied),
		If(AreCnvRequirementsSatisfied),
		If(AreLvmRequirementsSatisfied),
		If(AreMetalLBRequirementsSatisfied),
		If(HasSufficientNetworkLatencyRequirementForRole),
		If(HasSufficientPacketLossRequirementForRole),
		If(HasDefaultRoute),
		If(IsAPIDomainNameResolvedCorrectly),
		If(IsAPIInternalDomainNameResolvedCorrectly),
		If(IsAppsDomainNameResolvedCorrectly),
		If(IsDNSWildcardNotConfigured),
		If(IsPlatformNetworkSettingsValid),
		If(SufficientOrUnknownInstallationDiskSpeed),
		If(NonOverlappingSubnets),
		If(CompatibleAgent),
		If(IsTimeSyncedBetweenHostAndService),
		If(NoSkipInstallationDisk),
		If(NoSkipMissingDisk),
	)

	// In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail.
	// This transition handles the case that a host does not pass minimum hardware requirements for any of the roles
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
		},
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
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
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory), If(VSphereHostUUIDEnabled),
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
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
			hasMinRequiredHardware,
			stateswitch.Or(stateswitch.Not(If(VSphereHostUUIDEnabled)),
				stateswitch.And(requiredInputFieldsExist, stateswitch.Not(isSufficientForInstall)),
			)),
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
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			isSufficientForInstall,
			If(VSphereHostUUIDEnabled),
		),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		Condition: stateswitch.And(If(IsConnected), If(IsMediaConnected), If(HasInventory),
			hasMinRequiredHardware,
			requiredInputFieldsExist,
			isSufficientForInstall,
			If(VSphereHostUUIDEnabled),
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

	// Noop transitions for refresh while reclaim has not timed out
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusReclaiming),
		stateswitch.State(models.HostStatusReclaimingRebooting),
	} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			Condition:        stateswitch.Not(th.HasStatusTimedOut(ReclaimTimeout)),
			DestinationState: state,
		})
	}

	return sm
}
