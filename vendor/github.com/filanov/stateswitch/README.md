[![Actions Status](https://github.com/filanov/stateswitch/workflows/make_all/badge.svg)](https://github.com/filanov/stateswitch/actions)

# stateswitch

## Overview

A simple and clear way to create and represent state machine.

```go
sm := stateswitch.NewStateMachine()

sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateKnown,
    Condition:        th.IsSufficient,
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
})

sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateInsufficient,
    Condition:        th.IsInsufficient,
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
})

sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeRegister,
    SourceStates:     stateswitch.States{""},
    DestinationState: StateDiscovering,
    Condition:        nil,
    Transition:       nil,
    PostTransition:   th.RegisterNew,
})

sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeRegister,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateDiscovering,
    Condition:        nil,
    Transition:       nil,
    PostTransition:   th.RegisterAgain,
})
```

## Usage

First your state object need to implement the state interface:

```go
type StateSwitch interface {
	// State return current state
	State() State
	// SetState set a new state
	SetState(state State) error
}
```

Then you need to create state machine

```go
sm := stateswitch.NewStateMachine()
```

Add transitions with the expected behavior 
```go
sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateInsufficient,
    Condition:        th.IsInsufficient,
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
})
```

`TransitionRule` define the behavior that will be selected for your object by transition type,
source state and conditions that you define.
The first transition that will satisfy those requirements will be activated. 
`Condtion`, `Transition` `PostTranstion` are all optional, the transition may only change the state.

Since `Condtion` represent boolean entity, statewitch provides means to create a combination of these entities from basic 
boolean operations: `Not`,`And`, `Or`.  For example, rule with complex condition:

```go
sm.AddTransition(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StatePending,
    Condition:        And(th.IsConnected, th.HasInventory, Not(th.RoleDefined)),
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
})
```

Run transition by type, state machine will select the right one for you.

```go
h.sm.Run(TransitionTypeSetHwInfo, &stateHost{host: host}, &TransitionArgsSetHwInfo{hwInfo: hw})
```

for more details and full examples take a look at the examples section.


## Examples

Example can be found [here](https://github.com/filanov/stateswitch/tree/master/examples)