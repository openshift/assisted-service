package eventstest

import "github.com/openshift/assisted-service/internal/events"

type eventPartMatcher func(event interface{}) bool

func NewEventMatcher(matcherGens ...eventPartMatcher) EventMatcher {
	var matchers = make([]eventPartMatcher, 0, 5)
	matchers = append(matchers, matcherGens...)
	return EventMatcher{
		matchers,
	}
}

func WithNameMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.BaseEvent)
		if !ok {
			return false
		}
		if e.GetName() == expected {
			return true
		}
		return false
	}
}

func WithClusterIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.ClusterEvent)
		if !ok {
			return false
		}
		if e.GetClusterId().String() == expected {
			return true
		}
		return false
	}
}

func WithHostIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.HostEvent)
		if !ok {
			return false
		}
		if e.GetHostId().String() == expected {
			return true
		}
		return false
	}
}

func WithSeverityMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.HostEvent)
		if !ok {
			return false
		}
		if e.GetSeverity() == expected {
			return true
		}
		return false
	}
}

type EventMatcher struct {
	matchers []eventPartMatcher
}

// Matches implements gomock.Matcher
func (e EventMatcher) Matches(input interface{}) bool {
	event, ok := input.(events.BaseEvent)
	if !ok {
		return false
	}
	for _, matcher := range e.matchers {
		if !matcher(event) {
			return false
		}
	}
	return true
}

func (e EventMatcher) String() string {
	return "verifies event fields"
}
