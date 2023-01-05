package eventstest

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/thoas/go-funk"
)

type eventPartMatcher func(event interface{}) (bool, string)

func NewEventMatcher(matcherGens ...eventPartMatcher) *EventMatcher {
	var matchers = make([]eventPartMatcher, 0, 5)
	matchers = append(matchers, matcherGens...)
	return &EventMatcher{
		matchers,
		"",
	}
}

func WithNameMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.BaseEvent)
		if !ok {
			return false, "internal error"
		}
		if e.GetName() == expected {
			return true, ""
		}

		return false, fmt.Sprintf("expected event name %s to equal %s", e.GetName(), expected)
	}
}

func WithClusterIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		var clusterID string
		switch e := event.(type) {
		case eventsapi.ClusterEvent:
			clusterID = e.GetClusterId().String()
		case eventsapi.HostEvent:
			if e.GetClusterId() == nil {
				clusterID = ""
			} else {
				clusterID = e.GetClusterId().String()
			}
		case eventsapi.InfraEnvEvent:
			if e.GetClusterId() == nil {
				// cluster_id is an optional parameter for infraEnv events
				clusterID = ""
			} else {
				clusterID = e.GetClusterId().String()
			}
		default:
			return false, "internal error"
		}

		if clusterID == expected {
			return true, ""
		}

		return false, fmt.Sprintf("expected event cluster ID %s to equal %s", clusterID, expected)
	}
}

func WithHostIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.HostEvent)
		if !ok {
			return false, "internal error"
		}
		if e.GetHostId().String() == expected {
			return true, ""
		}
		return false, fmt.Sprintf("expected event host ID %s to equal %s", e.GetHostId().String(), expected)
	}
}

func WithInfraEnvIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		var infraenvID string
		switch e := event.(type) {
		case eventsapi.HostEvent:
			infraenvID = e.GetInfraEnvId().String()
		case eventsapi.InfraEnvEvent:
			infraenvID = e.GetInfraEnvId().String()
		default:
			return false, "internal error"
		}

		if infraenvID == expected {
			return true, ""
		}

		return false, fmt.Sprintf("expected event infraenv ID %s to equal %s", infraenvID, expected)
	}
}

func WithSeverityMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.BaseEvent)
		if !ok {
			return false, "internal error"
		}
		if e.GetSeverity() == expected {
			return true, ""
		}
		return false, fmt.Sprintf("expected event severity %s to equal %s", e.GetSeverity(), expected)
	}
}

func WithMessageMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.BaseEvent)
		if !ok {
			return false, "internal error"
		}
		if e.FormatMessage() == expected {
			return true, ""
		}
		return false, fmt.Sprintf("expected event host ID %s to equal %s", e.FormatMessage(), expected)
	}
}

func WithMessageContainsMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.BaseEvent)
		if !ok {
			return false, "internal error"
		}

		if funk.Contains(e.FormatMessage(), expected) {
			return true, ""
		}

		return false, fmt.Sprintf("expected event message %s to contain %s", e.FormatMessage(), expected)
	}
}

func WithInfoMatcher(expected string) eventPartMatcher {
	return func(event interface{}) (bool, string) {
		e, ok := event.(eventsapi.InfoEvent)
		if !ok {
			return false, "internal error"
		}
		if e.GetInfo() == expected {
			return true, ""
		}
		return false, fmt.Sprintf("expected event info %s to equal %s", e.GetInfo(), expected)
	}
}

type EventMatcher struct {
	matchers []eventPartMatcher
	message  string
}

// Matches implements gomock.Matcher
func (e *EventMatcher) Matches(input interface{}) bool {
	event, ok := input.(eventsapi.BaseEvent)
	if !ok {
		e.message = "Unsupported event type"
		return false
	}
	for _, matcher := range e.matchers {
		matched, matcherMessage := matcher(event)
		if !matched {
			matcherFunc := runtime.FuncForPC(reflect.ValueOf(matcher).Pointer()).Name()
			matcherFuncName := filepath.Base(strings.TrimSuffix(matcherFunc, ".func1"))
			e.message = fmt.Sprintf(
				"Failed Matcher: %s for event type %s with message: %s, full event:\n\t%+v\n",
				matcherFuncName, event.GetName(), matcherMessage, event)
			return false
		}
	}
	return true
}

func (e *EventMatcher) String() string {
	return e.message
}

func FindEventByName(events []*common.Event, eventName string) *common.Event {
	for _, event := range events {
		if event.Name == eventName {
			return event
		}
	}
	return nil
}
