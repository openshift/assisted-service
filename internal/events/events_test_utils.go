package events

/*
type ClientOption func(c *Client)
func NewClient(opts ...ClientOption) *Client {
   client :=  &Client{}
   for _, opt := range opts {
      opt(client)
   }

   return client
}
*/
type eventPartMatcher func(event interface{}) bool

func NewEventMatcher(matcherGens ...eventPartMatcher) EventMatcher {
	var matchers = make([]eventPartMatcher, 0, 5)
	matchers = append(matchers, matcherGens...)
	return EventMatcher{
		matchers,
	}
}

func WithIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(BaseEvent)
		if !ok {
			return false
		}
		if e.GetId() == expected {
			return true
		}
		return false
	}
}

func WithClusterIdMatcher(expected string) eventPartMatcher {
	return func(event interface{}) bool {
		e, ok := event.(ClusterEvent)
		if !ok {
			return false
		}
		if e.GetClusterId().String() == expected {
			return true
		}
		return false
	}
}

type EventMatcher struct {
	matchers []eventPartMatcher
}

func (e EventMatcher) Matches(input interface{}) bool {
	event, ok := input.(BaseEvent)
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
	return "input contains Ids"
}
