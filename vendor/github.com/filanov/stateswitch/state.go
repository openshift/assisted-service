package stateswitch

type State string
type States []State

// Check it states list contain specific state
func (s States) Contain(state State) bool {
	for _, st := range s {
		if st == state {
			return true
		}
	}
	return false
}
