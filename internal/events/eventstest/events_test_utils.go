package eventstest

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/openshift/assisted-service/internal/events"
)

type eventPartMatcher func(event interface{}) bool

func NewEventMatcher(matcherGens ...eventPartMatcher) *EventMatcher {
	var matchers = make([]eventPartMatcher, 0, 5)
	matchers = append(matchers, matcherGens...)
	return &EventMatcher{
		matchers,
		"",
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
		switch e := event.(type) {
		case events.ClusterEvent:
			return e.GetClusterId().String() == expected
		case events.HostEvent:
			if e.GetClusterId() == nil {
				return false
			}
			return e.GetClusterId().String() == expected
		default:
			return false
		}
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

func WithInfraEnvIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.HostEvent)
		if !ok {
			return false
		}
		if e.GetInfraEnvId().String() == expected {
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

func WithMessageMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(events.BaseEvent)
		if !ok {
			return false
		}
		if e.FormatMessage() == expected {
			return true
		}
		return false
	}
}

type EventMatcher struct {
	matchers []eventPartMatcher
	message  string
}

// Matches implements gomock.Matcher
func (e *EventMatcher) Matches(input interface{}) bool {
	event, ok := input.(events.BaseEvent)
	if !ok {
		e.message = "Unsupported event type"
		return false
	}
	for _, matcher := range e.matchers {
		if !matcher(event) {
			matcherFunc := runtime.FuncForPC(reflect.ValueOf(matcher).Pointer()).Name()
			matcherFuncName := filepath.Base(strings.TrimSuffix(matcherFunc, ".func1"))
			e.message = fmt.Sprintf("Failed Mathcer: %s with %+v \n", matcherFuncName, event)
			return false
		}
	}
	return true
}

func (e *EventMatcher) String() string {
	return e.message
}
