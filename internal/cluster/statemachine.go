package cluster

import (
	"fmt"

	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeCancelInstallation     = "CancelInstallation"
	TransitionTypeResetCluster           = "ResetCluster"
	TransitionTypePrepareForInstallation = "PrepareForInstallation"
	TransitionTypeRefreshStatus          = "RefreshStatus"
)

func NewClusterStateMachine(th TransitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	documentStates(sm)
	documentTransitionTypes(sm)

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
			stateswitch.State(models.ClusterStatusError),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusCancelled),
		PostTransition:   th.PostCancelInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Cancel installation of installing cluster",
			Description: "Move cluster to the cancelled state when user cancels installation",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
		},
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostCancelInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Cancel installation of preparing cluster",
			Description: "Cancelling a cluster during preparation simply cancels the preparation and moves it back to the ready, rather than putting the cluster in the cancelled state",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetCluster,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
			stateswitch.State(models.ClusterStatusError),
			stateswitch.State(models.ClusterStatusCancelled),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostResetCluster,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Reset installation",
			Description: "Reset the cluster, allowing it to be installed again",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypePrepareForInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusReady),
		},
		DestinationState: stateswitch.State(models.ClusterStatusPreparingForInstallation),
		PostTransition:   th.PostPrepareForInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Start installation",
			Description: "Begins preparing the cluster for installation",
		},
	})

	var pendingConditions = stateswitch.And(
		If(IsMachineCidrDefined),
		If(isClusterCidrDefined),
		If(isServiceCidrDefined),
		If(IsDNSDomainDefined),
		If(IsPullSecretSet),
	)

	var vipsDefinedConditions = stateswitch.And(
		If(AreIngressVipsDefined),
		If(AreApiVipsDefined),
		If(AreIngressVipsDefined),
	)

	var requiredForInstall = stateswitch.And(
		If(IsMachineCidrEqualsToCalculatedCidr),
		If(AreApiVipsValid),
		If(AreIngressVipsValid),
		If(AllHostsAreReadyToInstall),
		If(SufficientMastersCount),
		If(networkPrefixValid),
		If(noCidrOverlapping),
		If(IsNtpServerConfigured),
		If(IsOdfRequirementsSatisfied),
		If(IsLsoRequirementsSatisfied),
		If(IsCnvRequirementsSatisfied),
		If(IsLvmRequirementsSatisfied),
		If(IsMceRequirementsSatisfied),
		If(isNetworkTypeValid),
		If(NetworksSameAddressFamilies),
	)

	// Refresh cluster status conditions - Non DHCP
	var requiredInputFieldsExistNonDhcp = stateswitch.And(vipsDefinedConditions, pendingConditions)

	// Refresh cluster status conditions - DHCP
	var isSufficientForInstallDhcp = stateswitch.And(requiredForInstall, vipsDefinedConditions)

	var allRefreshStatusConditions = stateswitch.And(pendingConditions, vipsDefinedConditions, requiredForInstall)

	// Non DHCP transitions
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(stateswitch.Not(If(VipDhcpAllocationSet)), stateswitch.Not(requiredInputFieldsExistNonDhcp)),
		DestinationState: stateswitch.State(models.ClusterStatusPendingForInput),
		PostTransition:   th.PostRefreshCluster(statusInfoPendingForInput),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh discovering cluster - detect required input",
			Description: "In order for this transition to be fired at least one of the validations in requiredInputFieldsExistNonDhcp must fail. This transition handles the case that there is missing input that has to be provided from a user or other external means",
		},
	})
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition: stateswitch.And(
			stateswitch.Not(If(VipDhcpAllocationSet)),
			requiredInputFieldsExistNonDhcp,
			stateswitch.Not(requiredForInstall),
		),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(StatusInfoInsufficient),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh discovering cluster - detect insufficient",
			Description: "In order for this transition to be fired at least one of the validations in isSufficientForInstallNonDhcp must fail. This transition handles the case that one of the required validations that are required in order for the cluster to be in ready state  has failed",
		},
	})

	// DHCP transitions
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(If(VipDhcpAllocationSet), stateswitch.Not(pendingConditions)),
		DestinationState: stateswitch.State(models.ClusterStatusPendingForInput),
		PostTransition:   th.PostRefreshCluster(statusInfoPendingForInput),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "In order for this transition to be fired at least one of the validation IsMachineCidrDefined must fail. This transition handles the case that there is missing input that has to be provided from a user or other external means",
		},
	})
	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(If(VipDhcpAllocationSet), pendingConditions, stateswitch.Not(isSufficientForInstallDhcp)),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(StatusInfoInsufficient),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "In order for this transition to be fired at least one of the validations in isSufficientForInstallDhcp must fail. This transition handles the case that one of the required validations that are required in order for the host to be in known state (ready for installation) has failed",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        allRefreshStatusConditions,
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostRefreshCluster(StatusInfoReady),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh discovering cluster - detect ready",
			Description: "This transition is fired when all validations pass",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        stateswitch.And(th.IsPreparingTimedOut, stateswitch.Not(If(FailedPreparingtHostsExist))),
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostPreparingTimedOut,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh preparing cluster - detect timeout",
			Description: "This transition is fired when the preparing installation reach the timeout",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        stateswitch.And(If(AllHostsPreparedSuccessfully), If(ClusterPreparationSucceeded)),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		Transition:       th.InstallCluster,
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh preparing cluster - done preparing",
			Description: "This transition is fired when cluster installation preperation is complete and all hosts within the cluster have also finished preparing",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        If(UnPreparingtHostsExist),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(statusInfoUnpreparingHostExists),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh preparing cluster - insufficient",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates:   []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition: stateswitch.Or(
			If(FailedPreparingtHostsExist),
			stateswitch.And(
				stateswitch.Not(If(UnPreparingtHostsExist)),
				If(ClusterPreparationFailed),
			),
		),
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostRefreshCluster(statusInfoClusterFailedToPrepare),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Refresh preparing cluster - failed",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.Not(
			stateswitch.Or(
				th.IsInstalling,
				th.IsFinalizing,
			),
		),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoError),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition:        th.IsInstallationTimedOut,
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoTimeout),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Timed out while waiting for user",
			Description: "User was asked to take action and did not do so in time, give up and display appropriate error",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		Condition: stateswitch.And(th.IsFinalizingTimedOut,
			stateswitch.Not(th.SoftTimeoutsEnabled)),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizingTimeout),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Timed out while finalizing",
			Description: "Cluster finalization took too long, display appropriate error",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		Condition: stateswitch.And(stateswitch.Not(th.SoftTimeoutsEnabled),
			th.IsFinalizingStageTimedOut,
			stateswitch.Not(isInFinalizingStages(nonFailingFinalizingStages...))),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizingStageTimeout, FinalizingStage, th.FinalizingStageTimeoutMinutes),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Finalizing stage timed out.  Move to error",
			Description: "Cluster finalization stage took too long, display appropriate error",
		},
	})

	for _, st := range []string{models.ClusterStatusInstalling, models.ClusterStatusFinalizing} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType: TransitionTypeRefreshStatus,
			SourceStates: []stateswitch.State{
				stateswitch.State(st),
			},
			Condition: stateswitch.And(
				stateswitch.Not(finalizingStageTimeoutTriggered),
				stateswitch.Or(th.SoftTimeoutsEnabled,
					isInFinalizingStages(nonFailingFinalizingStages...)),
				th.IsFinalizingStageTimedOut),
			DestinationState: stateswitch.State(st),
			PostTransition:   th.PostRefreshFinalizingStageSoftTimedOut,
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        "Finalizing stage is taking too long.  Emit an appropriate event",
				Description: "Cluster finalization stage is taking too long, emit a warning event and continue installation",
			},
		})
	}

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			th.IsInstallingPendingUserAction,
			stateswitch.Or(
				th.IsInstalling,
				th.IsFinalizing)),
		DestinationState: stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsInstalling,
			stateswitch.Not(th.IsFinalizing),
		),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsFinalizing,
		),
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizing),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		Condition: stateswitch.And(
			stateswitch.Or(
				th.IsInstalling,
				th.IsFinalizing,
			),
			th.IsInstallingPendingUserAction,
		),
		DestinationState: stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		PostTransition:   th.PostRefreshCluster(statusInfoInstallingPendingUserAction),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "TODO: Name this transition",
			Description: "TODO: Document this transition",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			th.IsFinalizing,
			stateswitch.Not(th.IsInstallingPendingUserAction),
		),
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizing),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Move to finalizing",
			Description: "This transition is fired when the cluster is in installing and should move to finalizing",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsFinalizing),
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsInstalling,
		),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Stay in installing",
			Description: "Installing cluster should stay in installing",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		Condition:        th.WithAMSSubscriptions,
		PostTransition:   th.PostUpdateFinalizingAMSConsoleUrl,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Update AMS subscription",
			Description: "Update AMS subscription with console URL",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		Condition: stateswitch.And(
			th.areAllHostsDone,
			th.hasClusterCompleteInstallation,
		),
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusFinalizing)},
		DestinationState: stateswitch.State(models.ClusterStatusInstalled),
		PostTransition:   th.PostCompleteInstallation,
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Finalizing complete",
			Description: "The cluster has completed finalizing",
		},
	})

	sm.AddTransitionRule(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsFinalizing),
			stateswitch.Not(th.IsInstalling),
		),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoError),
		Documentation: stateswitch.TransitionRuleDoc{
			Name:        "Installation error",
			Description: "This transition is fired when the cluster is in installing and should move to error",
		},
	})

	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusError),
		stateswitch.State(models.ClusterStatusCancelled),
	} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Condition:        th.IsLogCollectionTimedOut,
			PostTransition:   th.PostRefreshLogsProgress(string(models.LogsStateTimeout)),
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Log collection timeout during %s", state),
				Description: fmt.Sprintf("Stay in %s state and update logs progress to timeout", state),
			},
		})
	}

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusPreparingForInstallation),
		stateswitch.State(models.ClusterStatusFinalizing),
		stateswitch.State(models.ClusterStatusInstalled),
		stateswitch.State(models.ClusterStatusError),
		stateswitch.State(models.ClusterStatusCancelled),
		stateswitch.State(models.ClusterStatusAddingHosts),
	} {
		sm.AddTransitionRule(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Documentation: stateswitch.TransitionRuleDoc{
				Name:        fmt.Sprintf("Maintain %s state", state),
				Description: fmt.Sprintf("Stay in %s state", state),
			},
		})
	}

	return sm
}

func documentStates(sm stateswitch.StateMachine) {
	sm.DescribeState(stateswitch.State(models.ClusterStatusInsufficient), stateswitch.StateDoc{
		Name:        "Insufficient",
		Description: "This is the initial state for regular, non-imported clusters",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusReady), stateswitch.StateDoc{
		Name:        "Ready",
		Description: "The cluster is ready to begin installation",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusError), stateswitch.StateDoc{
		Name:        "Error",
		Description: "The cluster has encountered an error during installation and cannot proceed. Usually due to a timeout",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusPreparingForInstallation), stateswitch.StateDoc{
		Name:        "Preparing For Installation",
		Description: "A transient state between Ready and Installing, cluster hosts are performing pre-installation validations",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusPendingForInput), stateswitch.StateDoc{
		Name:        "Pending For Input",
		Description: "The cluster is not ready for installation because it needs more information from the user",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusInstalling), stateswitch.StateDoc{
		Name:        "Installing",
		Description: "The cluster installation is in progress",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusFinalizing), stateswitch.StateDoc{
		Name:        "Finalizing",
		Description: "The cluster has sufficient ready control-plane and worker nodes, but OCP is still finalizing the installation",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusInstalled), stateswitch.StateDoc{
		Name:        "Installed",
		Description: "The cluster installation is considered complete, all operators are healthy and the cluster is ready to use",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusAddingHosts), stateswitch.StateDoc{
		Name:        "AddingHosts",
		Description: "The cluster is fully installed and is ready to accept new hosts. Installed clusters usually transition to this state automatically when installation is complete, depending on the configuration of the service. This is the initial state for imported clusters, as they are already installed",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusCancelled), stateswitch.StateDoc{
		Name:        "Cancelled",
		Description: "The cluster installation was cancelled by the user. Cluster must be reset to be able to install again",
	})
	sm.DescribeState(stateswitch.State(models.ClusterStatusInstallingPendingUserAction), stateswitch.StateDoc{
		Name:        "Installing, Pending User Action",
		Description: "Installation is in progress, but is blocked and cannot continue until the user takes action",
	})
}

func documentTransitionTypes(sm stateswitch.StateMachine) {
	sm.DescribeTransitionType(TransitionTypeCancelInstallation, stateswitch.TransitionTypeDoc{
		Name:        "Cancel Installation",
		Description: "Triggered when the user cancels the installation",
	})
	sm.DescribeTransitionType(TransitionTypeResetCluster, stateswitch.TransitionTypeDoc{
		Name:        "Reset Cluster",
		Description: "Triggered when the user resets the cluster",
	})
	sm.DescribeTransitionType(TransitionTypePrepareForInstallation, stateswitch.TransitionTypeDoc{
		Name:        "PrepareForInstallation",
		Description: "Triggered when the user starts the installation",
	})
	sm.DescribeTransitionType(TransitionTypeRefreshStatus, stateswitch.TransitionTypeDoc{
		Name:        "RefreshStatus",
		Description: "Triggered on some clusters periodically by the background cluster monitor goroutine that runs on the leader instance of the Assisted Service. Responsible for driving transitions between states that require re-evaluation of all the validation results and potential timeout conditions",
	})
}
