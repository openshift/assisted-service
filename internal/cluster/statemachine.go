package cluster

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeResetCluster               = "ResetCluster"
	TransitionTypePrepareForInstallation     = "PrepareForInstallation"
	TransitionTypeUpdateInstallationProgress = "UpdateInstallationProgress"
	TransitionTypeCompleteInstallation       = "CompleteInstallation"
	TransitionTypeHandlePreInstallationError = "Handle pre-installation-error"
	TransitionTypeRefreshStatus              = "RefreshStatus"
)

func NewClusterStateMachine(th *transitionHandler) stateswitch.StateMachine {
	sm := stateswitch.NewStateMachine()

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCancelInstallation,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
			stateswitch.State(models.ClusterStatusError),
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusCancelled),
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

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeUpdateInstallationProgress,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostUpdateInstallationProgress,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCompleteInstallation,
		Condition:      th.isSuccess,
		Transition: func(stateSwitch stateswitch.StateSwitch, args stateswitch.TransitionArgs) error {
			params, _ := args.(*TransitionArgsCompleteInstallation)
			params.reason = statusInfoInstalled
			return nil
		},
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusInstalled),
		PostTransition:   th.PostCompleteInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCompleteInstallation,
		Condition:      th.notSuccess,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostCompleteInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeHandlePreInstallationError,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
			stateswitch.State(models.ClusterStatusError),
		},
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostHandlePreInstallationError,
	})

	var pendingConditions = stateswitch.And(If(IsMachineCidrDefined), If(isClusterCidrDefined), If(isServiceCidrDefined), If(IsDNSDomainDefined), If(IsPullSecretSet))
	var vipsDefinedConditions = stateswitch.And(If(isApiVipDefined), If(isIngressVipDefined))
	var requiredForInstall = stateswitch.And(If(isMachineCidrEqualsToCalculatedCidr), If(isApiVipValid), If(isIngressVipValid), If(AllHostsAreReadyToInstall),
		If(SufficientMastersCount), If(networkPrefixValid), If(noCidrOverlapping), If(IsNtpServerConfigured))

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
		PostTransition:   th.PostRefreshCluster(statusInfoInsufficient),
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
		PostTransition:   th.PostRefreshCluster(statusInfoInsufficient),
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
		PostTransition:   th.PostRefreshCluster(statusInfoReady),
	})

	// This transition is fired when the preparing installation reach the timeout
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType:   TransitionTypeRefreshStatus,
		SourceStates:     []stateswitch.State{stateswitch.State(models.ClusterStatusPreparingForInstallation)},
		Condition:        th.IsPreparingTimedOut,
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoPreparingForInstallationTimeout),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition:        stateswitch.Not(th.IsInstalling),
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostRefreshCluster(statusInfoError),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			th.IsInstallingPendingUserAction,
			th.IsInstalling),
		DestinationState: stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstallingPendingUserAction),
		},
		Condition: stateswitch.And(
			stateswitch.Not(th.IsInstallingPendingUserAction),
			th.IsInstalling),
		DestinationState: stateswitch.State(models.ClusterStatusInstalling),
		PostTransition:   th.PostRefreshCluster(statusInfoInstalling),
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusInstalling),
		},
		Condition: stateswitch.And(
			th.IsInstalling,
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

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusPreparingForInstallation),
		stateswitch.State(models.ClusterStatusFinalizing),
		stateswitch.State(models.ClusterStatusInstalled),
		stateswitch.State(models.ClusterStatusError),
		stateswitch.State(models.ClusterStatusAddingHosts)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	return sm
}
