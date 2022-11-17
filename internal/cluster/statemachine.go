package cluster

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeResetCluster               = "ResetCluster"
	TransitionTypePrepareForInstallation     = "PrepareForInstallation"
	TransitionTypeHandlePreInstallationError = "Handle pre-installation-error"
	TransitionTypeRefreshStatus              = "RefreshStatus"
)

func NewClusterStateMachine(th *transitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
			stateswitch.State(models.ClusterStatusError),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusCancelled),
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
		},
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
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
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypePrepareForInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusReady),
		},
		DestinationState: stateswitch.State(models.ClusterStatusPreparingForInstallation),
		PostTransition:   th.PostPrepareForInstallation,
	})

	var pendingConditions = stateswitch.And(If(IsMachineCidrDefined), If(isClusterCidrDefined), If(isServiceCidrDefined), If(IsDNSDomainDefined), If(IsPullSecretSet))
	var vipsDefinedConditions = stateswitch.And(If(AreIngressVipsDefined), If(AreApiVipsDefined), If(AreIngressVipsDefined))
	var requiredForInstall = stateswitch.And(If(IsMachineCidrEqualsToCalculatedCidr), If(AreApiVipsValid), If(AreIngressVipsValid), If(AllHostsAreReadyToInstall),
		If(SufficientMastersCount), If(networkPrefixValid), If(noCidrOverlapping), If(IsNtpServerConfigured), If(IsOdfRequirementsSatisfied),
		If(IsLsoRequirementsSatisfied), If(IsCnvRequirementsSatisfied), If(IsLvmRequirementsSatisfied), If(isNetworkTypeValid), If(NetworksSameAddressFamilies))

	// Refresh cluster status conditions - Non DHCP
	var requiredInputFieldsExistNonDhcp = stateswitch.And(vipsDefinedConditions, pendingConditions)

	// Refresh cluster status conditions - DHCP
	var isSufficientForInstallDhcp = stateswitch.And(requiredForInstall, vipsDefinedConditions)

	var allRefreshStatusConditions = stateswitch.And(pendingConditions, vipsDefinedConditions, requiredForInstall)

	// Non DHCP transitions

	// In order for this transition to be fired at least one of the validations in requiredInputFieldsExistNonDhcp must fail.
	// This transition handles the case that there is missing input that has to be provided from a user or other external means
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(stateswitch.Not(If(VipDhcpAllocationSet)), stateswitch.Not(requiredInputFieldsExistNonDhcp)),
		DestinationState: stateswitch.State(models.ClusterStatusPendingForInput),
		PostTransition:   th.PostRefreshCluster(statusInfoPendingForInput),
	})

	// In order for this transition to be fired at least one of the validations in isSufficientForInstallNonDhcp must fail.
	// This transition handles the case that one of the required validations that are required in order for the cluster
	// to be in ready state  has failed
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(stateswitch.Not(If(VipDhcpAllocationSet)), requiredInputFieldsExistNonDhcp, stateswitch.Not(requiredForInstall)),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(StatusInfoInsufficient),
	})

	// DHCP transitions

	// In order for this transition to be fired at least one of the validation IsMachineCidrDefined must fail.
	// This transition handles the case that there is missing input that has to be provided from a user or other external means
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(If(VipDhcpAllocationSet), stateswitch.Not(pendingConditions)),
		DestinationState: stateswitch.State(models.ClusterStatusPendingForInput),
		PostTransition:   th.PostRefreshCluster(statusInfoPendingForInput),
	})

	// In order for this transition to be fired at least one of the validations in isSufficientForInstallDhcp must fail.
	// This transition handles the case that one of the required validations that are required in order for the host
	// to be in known state (ready for installation) has failed
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(If(VipDhcpAllocationSet), pendingConditions, stateswitch.Not(isSufficientForInstallDhcp)),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(StatusInfoInsufficient),
	})

	// This transition is fired when all validations pass
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        allRefreshStatusConditions,
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostRefreshCluster(StatusInfoReady),
	})

	// This transition is fired when the preparing installation reach the timeout
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        th.IsPreparingTimedOut,
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostPreparingTimedOut,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        stateswitch.And(If(AllHostsPreparedSuccessfully), If(ClusterPreparationSucceeded)),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		Transition:       th.InstallCluster,
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        If(UnPreparingtHostsExist),
		DestinationState: stateswitch.State(models.ClusterStatusInsufficient),
		PostTransition:   th.PostRefreshCluster(statusInfoUnpreparingHostExists),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        stateswitch.Or(If(FailedPreparingtHostsExist), stateswitch.And(stateswitch.Not(If(UnPreparingtHostsExist)), If(ClusterPreparationFailed))),
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostRefreshCluster(statusInfoClusterFailedToPrepare),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.Not(stateswitch.Or(
			th.IsInstalling,
			th.IsFinalizing)),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoError),
	})

	// Installation timeout while pending user action
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition:        th.IsInstallationTimedOut,
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoTimeout),
	})

	// Timeout in finalizing stage
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		Condition:        th.IsFinalizingTimedOut,
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizingTimeout),
	})

	sm.AddTransition(stateswitch.TransitionRule{
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
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsInstalling,
			stateswitch.Not(th.IsFinalizing)),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsFinalizing),
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizing),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		Condition: stateswitch.And(
			stateswitch.Or(
				th.IsInstalling,
				th.IsFinalizing),
			th.IsInstallingPendingUserAction),
		DestinationState: stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		PostTransition:   th.PostRefreshCluster(statusInfoInstallingPendingUserAction),
	})

	// This transition is fired when the cluster is in installing and should move to finalizing
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			th.IsFinalizing,
			stateswitch.Not(th.IsInstallingPendingUserAction)),
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		PostTransition:   th.PostRefreshCluster(statusInfoFinalizing),
	})

	// This transition is fired when the cluster is in installing
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsFinalizing),
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsInstalling),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusFinalizing),
		Condition:        th.WithAMSSubscriptions,
		PostTransition:   th.PostUpdateFinalizingAMSConsoleUrl,
	})

	// This transition is fired when the cluster is in finalizing
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		Condition: stateswitch.And(
			th.areAllHostsDone,
			th.hasClusterCompleteInstallation),
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusFinalizing)},
		DestinationState: stateswitch.State(models.ClusterStatusInstalled),
		PostTransition:   th.PostCompleteInstallation,
	})

	// This transition is fired when the cluster is in installing and should move to error
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsFinalizing),
			stateswitch.Not(th.IsInstalling)),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoError),
	})

	// check timeout of log collection
	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusError),
		stateswitch.State(models.ClusterStatusCancelled)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
			Condition:        th.IsLogCollectionTimedOut,
			PostTransition:   th.PostRefreshLogsProgress(string(models.LogsStateTimeout)),
		})
	}

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusPreparingForInstallation),
		stateswitch.State(models.ClusterStatusFinalizing),
		stateswitch.State(models.ClusterStatusInstalled),
		stateswitch.State(models.ClusterStatusError),
		stateswitch.State(models.ClusterStatusCancelled),
		stateswitch.State(models.ClusterStatusAddingHosts)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	return sm
}
