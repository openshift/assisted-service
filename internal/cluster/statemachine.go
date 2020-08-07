package cluster

import (
	"github.com/filanov/stateswitch"
	"github.com/openshift/assisted-service/models"
)

const (
	TransitionTypeCancelInstallation         = "CancelInstallation"
	TransitionTypeResetCluster               = "ResetCluster"
	TransitionTypePrepareForInstallation     = "PrepareForInstallation"
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
			stateswitch.State(models.ClusterStatusError),
		},
		DestinationState: stateswitch.State(models.ClusterStatusError),
		PostTransition:   th.PostCancelInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeResetCluster,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPreparingForInstallation),
			stateswitch.State(models.ClusterStatusInstalling),
			stateswitch.State(models.ClusterStatusError),
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
		DestinationState: clusterStatusInstalled,
		PostTransition:   th.PostCompleteInstallation,
	})

	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeCompleteInstallation,
		Condition:      th.notSuccess,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusFinalizing),
		},
		DestinationState: clusterStatusError,
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

	// Refresh cluster status
	var requiredInputFieldsExist = stateswitch.And(If(IsMachineCidrDefined), If(NoPendingForInputHost), If(isApiVipDefined), If(isIngressVipDefined))
	var isSufficientForInstall = stateswitch.And(If(isMachineCidrEqualsToCalculatedCidr), If(isApiVipValid),
		If(isIngressVipValid), If(AllHostsAreReadyToInstall), If(HasExactlyThreeMasters))

	// In order for this transition to be fired at least one of the validations in sufficientInputValidations must fail.
	// This transition handles the case that there is missing input that has to be provided from a user or other external means
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.Not(requiredInputFieldsExist),
		DestinationState: stateswitch.State(models.ClusterStatusPendingForInput),
		PostTransition:   th.PostRefreshCluster(statusInfoPendingForInput),
	})

	// In order for this transition to be fired at least one of the validations in sufficientForInstallValidations must fail.
	// This transition handles the case that one of the required validations that are required in order for the host
	// to be in known state (ready for installation) has failed
	sm.AddTransition(stateswitch.TransitionRule{
		TransitionType: TransitionTypeRefreshStatus,
		SourceStates: []stateswitch.State{
			stateswitch.State(models.ClusterStatusPendingForInput),
			stateswitch.State(models.ClusterStatusReady),
			stateswitch.State(models.ClusterStatusInsufficient),
		},
		Condition:        stateswitch.And(requiredInputFieldsExist, stateswitch.Not(isSufficientForInstall)),
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
		Condition:        stateswitch.And(requiredInputFieldsExist, isSufficientForInstall),
		DestinationState: stateswitch.State(models.ClusterStatusReady),
		PostTransition:   th.PostRefreshCluster(statusInfoReady),
	})

	// TODO - fix it This transition is fired when the preparing installation reach the timeout
	//sm.AddTransition(stateswitch.TransitionRule{
	//	TransitionType: TransitionTypeRefreshStatus,
	//	SourceStates: []stateswitch.State{
	//		stateswitch.State(models.ClusterStatusPreparingForInstallation),
	//	},
	//	Condition:        stateswitch.And(requiredInputFieldsExist, isSufficientForInstall),
	//	DestinationState: stateswitch.State(models.ClusterStatusError),
	//	PostTransition:   th.PostRefreshCluster(statusInfoPreparingForInstallationTimeout),
	//})

	// Noop transitions
	for _, state := range []stateswitch.State{
		stateswitch.State(models.ClusterStatusFinalizing),
		stateswitch.State(models.ClusterStatusInstalled),
		stateswitch.State(models.ClusterStatusError)} {
		sm.AddTransition(stateswitch.TransitionRule{
			TransitionType:   TransitionTypeRefreshStatus,
			SourceStates:     []stateswitch.State{state},
			DestinationState: state,
		})
	}

	return sm
}
