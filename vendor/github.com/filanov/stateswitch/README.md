[![Actions Status](https://github.com/filanov/stateswitch/workflows/make_all/badge.svg)](https://github.com/filanov/stateswitch/actions)

# stateswitch

## Overview

A simple and clear way to create and represent state machine.

```go
sm := stateswitch.NewStateMachine()

// Define the state machine rules (and optionally document each rule)
sm.AddTransitionRule(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateKnown,
    Condition:        th.IsSufficient,
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
    Documentation: stateswitch.TransitionRuleDoc{
        Name:        "Move to known when receiving good hardware information",
        Description: "Once we receive hardware information from a server, we can consider it known if the hardware information is sufficient",
    },
})
sm.AddTransitionRule(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateInsufficient,
    Condition:        th.IsInsufficient,
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
    Documentation: stateswitch.TransitionRuleDoc{
        Name:        "Move to insufficient when receiving bad hardware information",
        Description: "Once we receive hardware infomration from a server, we consider the server to be insufficient if the hardware is insufficient",
    },
})
sm.AddTransitionRule(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeRegister,
    SourceStates:     stateswitch.States{""},
    DestinationState: StateDiscovering,
    Condition:        nil,
    Transition:       nil,
    PostTransition:   th.RegisterNew,
    Documentation: stateswitch.TransitionRuleDoc{
        Name:        "Initial registration",
        Description: "A new server which registers enters our initial discovering state",
    },
})
sm.AddTransitionRule(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeRegister,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StateDiscovering,
    Condition:        nil,
    Transition:       nil,
    PostTransition:   th.RegisterAgain,
    Documentation: stateswitch.TransitionRuleDoc{
        Name:        "Re-registration",
        Description: "We should ignore repeated registrations from servers that are already registered",
    },
})

// Document transition types (optional)
sm.DescribeTransitionType(TransitionTypeSetHwInfo, stateswitch.TransitionTypeDoc{
    Name:        "Set hardware info",
    Description: "Triggered for every hardware information change",
})
sm.DescribeTransitionType(TransitionTypeRegister, stateswitch.TransitionTypeDoc{
    Name:        "Register",
    Description: "Triggered when a server registers",
})

// Document possible states (optional)
sm.DescribeState(StateDiscovering, stateswitch.StateDoc{
    Name:        "Discovering",
    Description: "Indicates that the server has registered but we still don't know anything about its hardware",
})
sm.DescribeState(StateKnown, stateswitch.StateDoc{
    Name:        "Discovering",
    Description: "Indicates that the server has registered but we still don't know anything about its hardware",
})
sm.DescribeState(StateInsufficient, stateswitch.StateDoc{
    Name:        "Insufficient",
    Description: "Indicates that the server has sufficient hardware",
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
sm.AddTransitionRule(stateswitch.TransitionRule{
	TransitionType:   TransitionTypeSetHwInfo,
	SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
	DestinationState: StateInsufficient,
	Condition:        th.IsInsufficient,
	Transition:       th.SetHwInfo,
	PostTransition:   th.PostSetHwInfo,
	Documentation: stateswitch.TransitionRuleDoc{
		Name:        "Example transition rule",
		Description: "Example documentation for transition rule",
	},
})
```

`TransitionRule` define the behavior that will be selected for your object by transition type,
source state and conditions that you define.
The first transition that will satisfy those requirements will be activated. 
`Condtion`, `Transition`, `PostTranstion` and `Documentation` are all optional, the transition may only change the state.

Since `Condtion` represent boolean entity, stateswitch provides means to create a combination of these entities from basic 
boolean operations: `Not`,`And`, `Or`.  For example, rule with complex condition:

```go
sm.AddTransitionRule(stateswitch.TransitionRule{
    TransitionType:   TransitionTypeSetHwInfo,
    SourceStates:     stateswitch.States{StateDiscovering, StateKnown, StateInsufficient},
    DestinationState: StatePending,
    Condition:        And(th.IsConnected, th.HasInventory, Not(th.RoleDefined)),
    Transition:       th.SetHwInfo,
    PostTransition:   th.PostSetHwInfo,
	Documentation: stateswitch.TransitionRuleDoc{
		Name:        "Example transition rule",
		Description: "Example documentation for transition rule",
	},
})
```

Run transition by type, state machine will select the right one for you.

```go
h.sm.Run(TransitionTypeSetHwInfo, &stateHost{host: host}, &TransitionArgsSetHwInfo{hwInfo: hw})
```

for more details and full examples take a look at the examples section.

### State machine representation

Once a state-machine has been initialized, you can generate a JSON file that
describes it by using the `AsJSON` method:

```go
machineJSON, err := sm.AsJSON()
if err != nil {
    panic(err)
}

fmt.Println(string(machineJSON))
```

This results in a JSON output that looks something like
[this](./examples/doc/example_asjson.json). This file can be used, for example,
for generating documentation or graphs for your state machine.

You can add the above code snippet to a dedicated binary that will generate the
JSON and use it in your CI/CD, or you can have you can serve the JSON as an API
endpoint - up to you.

## Examples

Example can be found [here](https://github.com/filanov/stateswitch/tree/master/examples)
