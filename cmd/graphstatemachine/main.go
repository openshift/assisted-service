package main

import (
	"fmt"

	"github.com/filanov/stateswitch"
	"github.com/golang/mock/gomock"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/host"
)

func main() {
	for _, machine := range []stateswitch.StateMachine{
		hostStateMachine(),
		clusterStateMachine(),
		poolHostStateMachine(),
	} {
		machineJSON, err := machine.AsJSON()
		if err != nil {
			panic(err)
		}

		fmt.Println(string(machineJSON))
	}
}

func hostStateMachine() stateswitch.StateMachine {
	mockController := gomock.NewController(nil)
	mockTransitionHandler := host.NewMockTransitionHandler(mockController)
	mockTransitionHandler.EXPECT().HasStatusTimedOut(gomock.Any()).Return(func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error) {
		return false, nil
	}).AnyTimes()

	mockTransitionHandler.EXPECT().PostRefreshHost(gomock.Any()).Return(
		func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
	).AnyTimes()

	mockTransitionHandler.EXPECT().PostRefreshLogsProgress(gomock.Any()).Return(
		func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
	).AnyTimes()

	return host.NewHostStateMachine(stateswitch.NewStateMachine(), mockTransitionHandler)
}

func clusterStateMachine() stateswitch.StateMachine {
	mockController := gomock.NewController(nil)
	mockTransitionHandler := cluster.NewMockTransitionHandler(mockController)

	mockTransitionHandler.EXPECT().PostRefreshCluster(gomock.Any()).Return(
		func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
	).AnyTimes()

	mockTransitionHandler.EXPECT().PostRefreshLogsProgress(gomock.Any()).Return(
		func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
	).AnyTimes()

	return cluster.NewClusterStateMachine(mockTransitionHandler)
}

func poolHostStateMachine() stateswitch.StateMachine {
	mockController := gomock.NewController(nil)
	mockTransitionHandler := host.NewMockTransitionHandler(mockController)
	mockTransitionHandler.EXPECT().HasStatusTimedOut(gomock.Any()).Return(func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error) {
		return false, nil
	}).AnyTimes()

	mockTransitionHandler.EXPECT().PostRefreshHost(gomock.Any()).Return(
		func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
	).AnyTimes()

	return host.NewPoolHostStateMachine(stateswitch.NewStateMachine(), mockTransitionHandler)
}
