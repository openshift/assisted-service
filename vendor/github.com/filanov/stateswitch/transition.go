package stateswitch

// TransitionType reprents an event that can cause a state transition
type TransitionType string

// TransitionRule is a rule that defines the required source states and
// conditions needed to move to a particular destination state when a
// particular transition type happens
type TransitionRule struct {
	TransitionType   TransitionType
	SourceStates     States
	DestinationState State
	Condition        Condition
	Transition       Transition
	PostTransition   PostTransition

	// Documentation for the transition rule, can be left empty
	Documentation TransitionRuleDoc
}

// IsAllowedToRun validate if current state supported, after then check the condition,
// if it pass then transition is a allowed. Nil condition is automatic approval.
func (tr TransitionRule) IsAllowedToRun(stateSwitch StateSwitch, args TransitionArgs) (bool, error) {
	if tr.SourceStates.Contain(stateSwitch.State()) {
		if tr.Condition == nil {
			return true, nil
		}
		return tr.Condition(stateSwitch, args)
	}
	return false, nil
}

type TransitionRules []TransitionRule

// Find search for all matching transitions by transition type
func (tr TransitionRules) Find(transitionType TransitionType) TransitionRules {
	match := TransitionRules{}
	for i := range tr {
		if tr[i].TransitionType == transitionType {
			match = append(match, tr[i])
		}
	}
	return match
}

type TransitionArgs interface{}

// Transition is users business logic, should not set the state or return next state
// If condition return true this function will be executed
// Not mandatory
type Transition func(stateSwitch StateSwitch, args TransitionArgs) error

// Condition for the transition, transition will be executed only if this function return true
// Can be nil, in this case it's considered as return true, nil
// Not mandatory
type Condition func(stateSwitch StateSwitch, args TransitionArgs) (bool, error)

// PostTransition will be called if condition and transition are successful.
// Not mandatory
type PostTransition func(stateSwitch StateSwitch, args TransitionArgs) error
