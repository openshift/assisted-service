package jq

type Variable struct {
	name  string
	value any
}

func String(name, value string) Variable {
	return Variable{
		name:  name,
		value: value,
	}
}

func Int(name string, value int) Variable {
	return Variable{
		name:  name,
		value: value,
	}
}

func Any(name string, value any) Variable {
	return Variable{
		name:  name,
		value: value,
	}
}
