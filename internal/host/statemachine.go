package host

import (
	"fmt"

	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

// See documentTransitionTypes for documentation of each transition type
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
	documentStates(sm)
	documentTransitionTypes(sm)

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			"",
		},
		Condition:        stateswitch.Not(th.IsUnboundHost),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Initial registration",
			Description: "When new host is first registered. This transition is not executed on unbound hosts because <unknown, TODO>",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
			stateswitch.State(models.HostStatusPendingForInput),
		},
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Re-registration",
			Description: "When the host attempts to register while it's in one of the non-installation states. We move the host back to the discovering state instead of keeping it in its current state because we consider it a new host with potentially different hardware. See PostRegisterHost function",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusResetting),
		},
		Condition:        th.IsHostInReboot,
		DestinationState: stateswitch.State(models.HostStatusResetting),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Ignore register while rebooting host in resetting",
			Description: "On such cases cluster monitor is responsible to set the host state to resetting-pending-user-action. There are some edge cases on installation where user tries to reset installation on the same time reboot is called. On some cases the agent will get to reset itself and register again just before the reboot and the cluster monitor will not get to set the status in resetting-pending-user-action on time. Created to prevent OCPBUGSM-13597",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusResetting),
		},
		Condition:        stateswitch.Not(th.IsHostInReboot),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRegisterHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Register non-rebooting host in resetting",
			Description: "The opposite of the 'Ignore register while rebooting host in resetting' transition rule, move host to discovering",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		Condition:      stateswitch.Or(th.IsHostInReboot, stateswitch.And(th.IsDay2Host, th.IsHostInDone)),
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress),
			stateswitch.State(models.HostStatusInstallingPendingUserAction),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusInstallingPendingUserAction),
		PostTransition:   th.PostRegisterDuringReboot,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Wrong boot order detection",
			Description: "A day-1 host trying to register while it's in the rebooting stage or a day-2 host trying to register while it's in the done stage indicate that the host, after installing the operating system to disk and then rebooting, booted from the discovery ISO again instead of booting the installed operating system as it should've done (the first thing the discovery ISO live OS tries to do is register). This indicates that the user has a wrong boot order that they should fix. This transition makes sure to let the user know about what happened and what they should do to fix that",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRegisterDuringInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Register during installation",
			Description: "Any host registering during installation but doesn't match the 'Wrong boot order detection' transition is performing an invalid operation and thus should move to the error state",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusError),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Register during error",
			Description: "Host in error should be able to register without changes. If the registration return conflict or error then we have infinite number of events. If the registration is blocked (403) it will break auto-reset feature. It can happen that user rebooted the host manually after installation failure without changes in the cluster. So the best option is just accept the registration without changes in the DB",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRegisterHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalled),
		},
		DestinationState: stateswitch.State(models.HostStatusInstalled),
		PostTransition:   th.PostRegisterAfterInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Register post-installation",
			Description: "A host may boot from the installation ISO after the cluster has been installed. In that case we want to ask the host to go away, as otherwise it will flood the log and the events",
		},
	})

	// Installation failure
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeHostInstallationFailed,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostHostInstallationFailed,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Installation failed while host is installing",
			Description: "When the installation fails while a host is installing, the host should be moved to the error state because it is no longer actually installing",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Installation canceled while host is installing",
			Description: "When the installation is canceled while the host is installing or finished installing, the host needs to move to the cancelled state",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostCancelInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Cancel while preparing",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusKnown),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Cancel while known",
			Description: "TODO: Document this transition rule",
		},
	})

	// Install day2 host
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeInstallHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		Condition:        th.IsDay2Host,
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostInstallHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Install known host",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reset pending user action all states",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Unbind pre-installation",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeUnbindHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Unbind during or after installation",
			Description: "TODO: Document this transition rule",
		},
	})

	// ReclaimHost when installed moves to Reclaiming
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalled),
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		DestinationState: stateswitch.State(models.HostStatusReclaiming),
		PostTransition:   th.PostUnbindHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reclaim successful host",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRebootingForReclaim,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
		},
		DestinationState: stateswitch.State(models.HostStatusReclaimingRebooting),
		PostTransition:   th.PostReclaim,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Rebooting for reclaim reclaiming host",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimFailed,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
			stateswitch.State(models.HostStatusReclaimingRebooting),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reclaim failure for reclaiming host",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusReclaiming),
			stateswitch.State(models.HostStatusReclaimingRebooting),
		},
		Condition:        th.HasStatusTimedOut(ReclaimTimeout),
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostRefreshReclaimTimeout,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh reclaiming host",
			Description: "TODO: Document this transition rule",
		},
	})

	// ReclaimHost in other states acts like Unbind
	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reclaim pre-installation",
			Description: "TODO: Document this transition rule",
		},
	})
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeReclaimHost,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusError),
			stateswitch.State(models.HostStatusCancelled),
		},
		DestinationState: stateswitch.State(models.HostStatusUnbindingPendingUserAction),
		PostTransition:   th.PostUnbindHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reclaim failed host",
			Description: "TODO: Document this transition rule",
		},
	})

	// Refresh host

	// Prepare for installation
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		Condition:      stateswitch.And(If(ValidRoleForInstallation), If(IsConnected), If(IsMediaConnected), If(ClusterPreparingForInstallation)),
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusKnown),
		},
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		PostTransition:   th.PostPreparingForInstallationHost,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh known host in preparing cluster",
			Description: "TODO: Document this transition rule",
		},
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

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), allConditionsSuccessful, If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
		PostTransition:   th.PostRefreshHost(statusInfoHostPreparationSuccessful),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh successfully preparing host",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingSuccessful),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Stay in preparing successful",
			Description: "TODO: Document this transition rule",
		},
	})

	// Install host
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), If(ClusterInstalling)),
		DestinationState: stateswitch.State(models.HostStatusInstalling),
		PostTransition:   th.PostRefreshHost(statusInfoInstalling),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move successfully prepared host to installing",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), allConditionsSuccessfulOrUnknown, stateswitch.Not(If(ClusterPreparingForInstallation))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move preparing host to known when cluster stops preparing",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingFailed),
			stateswitch.State(models.HostStatusKnown),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(SufficientOrUnknownInstallationDiskSpeed))),
		DestinationState: stateswitch.State(models.HostStatusInsufficient),
		PostTransition:   th.PostRefreshHost(statusInfoNotReadyForInstall),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Preparing failed disk speed host move to insufficient",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(SucessfullOrUnknownContainerImagesAvailability))),
		DestinationState: stateswitch.State(models.HostStatusPreparingFailed),
		PostTransition:   th.PostRefreshHost(statusInfoHostPreparationFailure),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Preparing failed image pull host move to preparing failed",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), atLeastOneConditionUnknown, If(ClusterPreparingForInstallation)),
		DestinationState: stateswitch.State(models.HostStatusPreparingForInstallation),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Stay in preparing for installation",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingFailed),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(ClusterPreparingForInstallation))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Failed preparing to known when cluster is no longer preparing",
			Description: "TODO: Document this transition rule",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(stateswitch.Or(If(ClusterPreparingForInstallation), If(ClusterInstalling)))),
		DestinationState: stateswitch.State(models.HostStatusKnown),
		PostTransition:   th.PostRefreshHost(statusInfoKnown),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Successful preparing to known when cluster is no longer preparing",
			Description: "TODO: Document this transition rule. Why is ClusterInstalling relevant here?",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDiscovering),
			stateswitch.State(models.HostStatusInsufficient),
			stateswitch.State(models.HostStatusKnown),
			stateswitch.State(models.HostStatusPendingForInput),
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusPreparingFailed),
		},
		Condition: stateswitch.Or(stateswitch.Not(If(IsConnected)),
			stateswitch.Not(If(IsMediaConnected))),
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
		PostTransition:   th.PostRefreshHost(statusInfoDisconnected),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move host to disconnected when connected times out",
			Description: "This transition occurs when no requests are detected from the agent or when the discovery media gets disconnected during pre-installation phases",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move to disconnected when virtual media disconnects pre-installation",
			Description: "TODO: Document this transition rule.",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move to error when virtual media disconnects post-installation",
			Description: "TODO: Document this transition rule.",
		},
	})

	// Abort host if cluster has errors
	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move host to error when cluster is in error",
			Description: "TODO: Document this transition rule. Why not day 2?",
		},
	})

	// Time out while host installation
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling)},
		Condition:        th.HasStatusTimedOut(InstallationTimeout),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationTimedOut),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move host to error when installation times out",
			Description: "TODO: Document this transition rule.",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusPreparingForInstallation),
			stateswitch.State(models.HostStatusPreparingSuccessful),
		},
		Condition: stateswitch.Or(stateswitch.Not(If(IsConnected)),
			stateswitch.Not(If(IsMediaConnected))),
		DestinationState: stateswitch.State(models.HostStatusDisconnected),
		PostTransition:   th.PostRefreshHost(statusInfoConnectionTimedOutPreparing),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move preparing host to disconnected when connection times out",
			Description: "This transition occurs when no requests are detected from the agent or when the discovery media gets disconnected during prepare for installation phases",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstalling),
			stateswitch.State(models.HostStatusInstallingInProgress),
		},
		Condition:        stateswitch.Not(If(IsConnected)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoConnectionTimedOutInstalling),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move installing host to error when connection times out",
			Description: "TODO: Document this transition rule.",
		},
	})
	shouldIgnoreInstallationProgressTimeout := stateswitch.And(If(StageInWrongBootStages), If(ClusterPendingUserAction))

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition:        shouldIgnoreInstallationProgressTimeout,
		DestinationState: stateswitch.State(models.HostStatusInstallingInProgress),
		PostTransition:   th.PostRefreshHostRefreshStageUpdateTime,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Ignore timeout if host is in particular installation in progress stages",
			Description: "TODO: Document this transition rule.",
		},
	})

	// Time out while host installationInProgress
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition: stateswitch.And(
			th.HasInstallationInProgressTimedOut,
			stateswitch.Not(th.IsHostInReboot),
			stateswitch.Not(shouldIgnoreInstallationProgressTimeout)),
		DestinationState: stateswitch.State(models.HostStatusError),
		PostTransition:   th.PostRefreshHost(statusInfoInstallationInProgressTimedOut),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move to error on timeout if host is in particular installation in progress stages",
			Description: "TODO: Document this transition rule.",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusInstallingInProgress)},
		Condition: stateswitch.And(
			th.HasInstallationInProgressTimedOut,
			th.IsHostInReboot),
		DestinationState: stateswitch.State(models.HostStatusInstallingPendingUserAction),
		PostTransition:   th.PostRefreshHost(statusRebootTimeout),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Tell user about boot order wen reboot takes too long",
			Description: "TODO: Document this transition rule.",
		},
	})

	// Noop transitions for cluster error
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusInstalling),
		stateswitch.State(models.HostStatusInstallingInProgress),
		stateswitch.State(models.HostStatusInstalled),
		stateswitch.State(models.HostStatusInstallingPendingUserAction),
		stateswitch.State(models.HostStatusResettingPendingUserAction),
	} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			Condition:        stateswitch.Not(If(ClusterInError)),
			DestinationState: state,
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Refresh during %s state without cluster error should stay in %s state", state, state),
				Description: "TODO: Document this transition rule. Is this necessary?",
			},
		})
	}

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusDisconnected),
			stateswitch.State(models.HostStatusDiscovering),
		},
		Condition:        stateswitch.And(If(IsConnected), If(IsMediaConnected), stateswitch.Not(If(HasInventory))),
		DestinationState: stateswitch.State(models.HostStatusDiscovering),
		PostTransition:   th.PostRefreshHost(statusInfoDiscovering),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host reconnected without inventory",
			Description: "TODO: Document this transition rule. Why is Discovering in the source states?",
		},
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
		If(NoIPCollisionsInNetwork),
	)

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host has insufficient hardware",
			Description: "In order for this transition to be fired at least one of the validations in minRequiredHardwareValidations must fail. This transition handles the case that a host does not pass minimum hardware requirements for any of the roles",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host pending input",
			Description: "In order for this transition to be fired at least one of the validations in sufficientInputValidations must fail. This transition handles the case that there is missing input that has to be provided from a user or other external means",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host not ready",
			Description: "In order for this transition to be fired at least one of the validations in sufficientForInstallValidations must fail. This transition handles the case that one of the required validations that are required in order for the host to be in known state (ready for installation) has failed",
		},
	})

	// This transition is fired when all validations pass
	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host ready",
			Description: "This transition is fired when all validations pass. TODO: Why is the vSphere validation given special treatment here?",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
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
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Host stay ready",
			Description: "TODO: Document this transition rule.",
		},
	})

	// check timeout of log collection
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusCancelled)} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Condition:        th.IsLogCollectionTimedOut,
			PostTransition:   th.PostRefreshLogsProgress(string(models.LogsStateTimeout)),
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Log collection timed out during %s should stay in %s", state, state),
				Description: "TODO: Document this transition rule.",
			},
		})
	}

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusError),
		stateswitch.State(models.HostStatusCancelled),
		stateswitch.State(models.HostStatusResetting),
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

	// Noop transaction fro installed day2 on cloud hosts
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefresh,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.HostStatusAddedToExistingCluster),
		},
		Condition:        th.IsDay2Host,
		DestinationState: stateswitch.State(models.HostStatusAddedToExistingCluster),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Day 2 hosts should stay in added state",
			Description: "TODO: Document this transition rule.",
		},
	})

	// Noop transitions for refresh while reclaim has not timed out
	for _, state := range []stateswitch.State{
		stateswitch.State(models.HostStatusReclaiming),
		stateswitch.State(models.HostStatusReclaimingRebooting),
	} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefresh,
			SourceStates:     []stateswitch.State{state},
			Condition:        stateswitch.Not(th.HasStatusTimedOut(ReclaimTimeout)),
			DestinationState: state,
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Refresh without timeout during %s should stay in %s", state, state),
				Description: "TODO: Document this transition rule.",
			},
		})
	}

	return sm
}

func documentStates(sm stateswitch.StateMachine) {
	sm.DescribeState(stateswitch.State(models.HostStatusDiscovering), stateswitch.StateDoc{
		Name:        "Discovering",
		Description: "This is the first state that the host is in after it has been registered. We usually don't know much about the host at this point, unless it reached this state through other circumstances",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusKnown), stateswitch.StateDoc{
		Name:        "Known",
		Description: "Hosts in this state meet all the requirements and are ready for installation to start. All hosts must reach this state before cluster installation can begin",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusDisconnected), stateswitch.StateDoc{
		Name:        "Disconnected",
		Description: "Hosts reach this state when the agent stops communicating with the service for a period of time. This can happen if the host is rebooted, if the agent is stopped for some reason, or if the host lost connectivity. Hosts can also reach this state if the agent that runs them detects and reports that the virtual media serving the live ISO doesn't seem to be responding",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusInsufficient), stateswitch.StateDoc{
		Name:        "Insufficient",
		Description: "Hosts in this state do not meet all the requirements required for installation to start. In other words, hosts for which some of the validations which we deem required for installation have a negative status",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusDisabled), stateswitch.StateDoc{
		Name:        "Disabled",
		Description: "TODO: Describe this state. This seems like an obsolete state that is no longer being used",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusPreparingForInstallation), stateswitch.StateDoc{
		Name:        "Preparing for Installation",
		Description: "A transient state which occurs after the user triggers installation and before installation actually begins. This state was made for performing destructive validations such as disk speed check. We don't perform those validations in prior states because before the user clicks install, we don't have their consent to perform disk writes. If those validations fail, we do not continue with the installation process",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusPreparingFailed), stateswitch.StateDoc{
		Name:        "Preparing Failed",
		Description: "A state reached after the 'Preparing for Installation' state validations fail. This state is transient and the host automatically moves to and from it, it exists mostly to set the correct host status message to help the user understand what went wrong",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusPreparingSuccessful), stateswitch.StateDoc{
		Name:        "Preparing Successful",
		Description: "A state reached after the 'Preparing for Installation' state validations succeed. This state is transient and the host automatically moves to and from it, it exists mostly to set the correct host status message",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusPendingForInput), stateswitch.StateDoc{
		Name:        "Pending for Input",
		Description: "Similar to the 'Insufficient' state, except for validations which the user can resolve by providing some input, such as the machine CIDR for the cluster",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusInstalling), stateswitch.StateDoc{
		Name:        "Installing",
		Description: "The host installation has just begun. Hosts usually quickly move from this state to the 'Installing in Progress' state once they begin executing the install step",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusInstallingInProgress), stateswitch.StateDoc{
		Name:        "Installing in Progress",
		Description: "Hosts stay in this state for a long time while they're being installed. The actual host installation progress is tracked via the host's progress stages, percentage and messages rather than moving the hosts to different states",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusInstallingPendingUserAction), stateswitch.StateDoc{
		Name:        "Installing, Pending User Action",
		Description: "Hosts in this state are waiting for the user to perform some action before the installation can continue. For example, when the host boots into the discovery ISO after it has been rebooted by the Assisted Installer - the user must manually reboot the host into the installation disk",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusResettingPendingUserAction), stateswitch.StateDoc{
		Name:        "Resetting, Pending User Action",
		Description: "This is the true resetting state when ENABLE_AUTO_RESET is set to false (which it always is). In this state we wait for and tell the user to reboot the host into the live ISO in order to proceed",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusInstalled), stateswitch.StateDoc{
		Name:        "Installed",
		Description: "Hosts reach this state after they have been successfully installed. This state does not indicate that the cluster has successfully finished installing and initializing, only that this particular host seems to have successfuly joined and become an active member of the cluster",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusError), stateswitch.StateDoc{
		Name:        "Error",
		Description: "Hosts can reach this state in many ways when something goes wrong and there's nothing the service or the user can do to remedy the situation. For example, when the cluster state machine goes into error, all hosts within the cluster will also go into error. The only way to get a host out of this state is by resetting the cluster installation. It is possible that a cluster installation would be considered successful even when some of the hosts reach this state, for example when the host that reached this state is a worker and there are other workers that are sufficient for healthy cluster operation",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusResetting), stateswitch.StateDoc{
		Name:        "Resetting",
		Description: "Hosts reach this state when the user triggers a reset of the cluster installation. When ENABLE_AUTO_RESET is set to false (which it always is), this is a very short lived state and the host immediately proceeds to 'Resetting, Pending User Action' from it. This is a legacy state and it should eventually be merged with 'Resetting, Pending User Action'",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusAddedToExistingCluster), stateswitch.StateDoc{
		Name:        "Added to Existing Cluster",
		Description: "This is the final, successful state day-2 hosts reach when the Assisted Installer has done everything it can to help them join the target cluster",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusCancelled), stateswitch.StateDoc{
		Name:        "Cancelled",
		Description: "TODO: Describe this state",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusBinding), stateswitch.StateDoc{
		Name:        "Binding",
		Description: "TODO: Describe this state",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusUnbinding), stateswitch.StateDoc{
		Name:        "Unbinding",
		Description: "TODO: Describe this state",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusUnbindingPendingUserAction), stateswitch.StateDoc{
		Name:        "Unbinding, Pending User Action",
		Description: "TODO: Describe this state",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusReclaiming), stateswitch.StateDoc{
		Name:        "Reclaiming",
		Description: "TODO: Describe this state",
	})
	sm.DescribeState(stateswitch.State(models.HostStatusReclaimingRebooting), stateswitch.StateDoc{
		Name:        "Reclaiming, Rebooting",
		Description: "TODO: Describe this state",
	})
}

func documentTransitionTypes(sm stateswitch.StateMachine) {
	sm.DescribeTransitionType(TransitionTypeRegisterHost, stateswitch.TransitionTypeDoc{
		Name:        "Register Host",
		Description: "Triggered when a host boots the discovery ISO and calls the Register API",
	})
	sm.DescribeTransitionType(TransitionTypeHostInstallationFailed, stateswitch.TransitionTypeDoc{
		Name:        "Installation Failed",
		Description: "TODO: Document this transition type",
	})
	sm.DescribeTransitionType(TransitionTypeCancelInstallation, stateswitch.TransitionTypeDoc{
		Name:        "Cancel Installation",
		Description: "Triggered on each host when the user cancels the cluster installation",
	})
	sm.DescribeTransitionType(TransitionTypeInstallHost, stateswitch.TransitionTypeDoc{
		Name:        "Install Host",
		Description: "Triggered on each host when the user or Assisted kube-API controllers trigger cluster installation",
	})
	sm.DescribeTransitionType(TransitionTypeResettingPendingUserAction, stateswitch.TransitionTypeDoc{
		Name:        "Resetting, Pending User Action",
		Description: "TODO: Document this transition type",
	})
	sm.DescribeTransitionType(TransitionTypeRefresh, stateswitch.TransitionTypeDoc{
		Name:        "Refresh",
		Description: "Triggered on some hosts periodically by the background host monitor goroutine that runs on the leader instance of the Assisted Service. Responsible for driving transitions between states that require re-evaluation of all the validation results and potential timeout conditions",
	})
	sm.DescribeTransitionType(TransitionTypeMediaDisconnect, stateswitch.TransitionTypeDoc{
		Name:        "Media Disconnect",
		Description: "Triggered when the a step response returned by the agent indicates that a virtual media disconnection has occurred",
	})
	sm.DescribeTransitionType(TransitionTypeBindHost, stateswitch.TransitionTypeDoc{
		Name:        "Bind Host",
		Description: "Triggered when a previously unbound host is bound to a cluster",
	})
	sm.DescribeTransitionType(TransitionTypeUnbindHost, stateswitch.TransitionTypeDoc{
		Name:        "Unbind Host",
		Description: "TODO: Document this transition",
	})
	sm.DescribeTransitionType(TransitionTypeReclaimHost, stateswitch.TransitionTypeDoc{
		Name:        "Reclaim Host",
		Description: "TODO: Document this transition",
	})
	sm.DescribeTransitionType(TransitionTypeRebootingForReclaim, stateswitch.TransitionTypeDoc{
		Name:        "Rebooting for Reclaim",
		Description: "TODO: Document this transition type",
	})
	sm.DescribeTransitionType(TransitionTypeReclaimFailed, stateswitch.TransitionTypeDoc{
		Name:        "Reclaim Failed",
		Description: "TODO: Document this transition type",
	})
}
