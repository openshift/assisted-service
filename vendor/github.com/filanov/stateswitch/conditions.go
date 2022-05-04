package stateswitch

type not struct {
	operand Condition
}

func (n not) do(stateSwitch StateSwitch, args TransitionArgs) (bool, error) {
	b, err := n.operand(stateSwitch, args)
	if err != nil {
		return false, err
	}
	return !b, nil
}

func Not(operand Condition) Condition {
	return not{operand: operand}.do
}

type and struct {
	operands []Condition
}

func (a and) do(stateSwitch StateSwitch, args TransitionArgs) (bool, error) {
	for _, o := range a.operands {
		b, err := o(stateSwitch, args)
		if err != nil {
			return false, err
		}
		if !b {
			return false, nil
		}
	}
	return true, nil
}

func And(operands ...Condition) Condition {
	return and{operands: operands}.do
}

type or struct {
	operands []Condition
}

func (or or) do(stateSwitch StateSwitch, args TransitionArgs) (bool, error) {
	for _, o := range or.operands {
		b, err := o(stateSwitch, args)
		if err != nil {
			return false, err
		}
		if b {
			return true, nil
		}
	}
	return false, nil
}

func Or(operands ...Condition) Condition {
	return or{operands: operands}.do
}
