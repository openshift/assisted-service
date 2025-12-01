## Events

Events generation is designed to expose a uniformed method of
initializing the assisted-service events.
Each event definition requires a set of properties based on its type.
The definition is used for generating a function for emitting the event with
the required parameters.

### Adding an Event
In order to add a new event, follow the next steps:

1. Add event definition to [docs/events.yaml](https://github.com/openshift/assisted-service/blob/master/docs/events.yaml)
2. Generate the code for creating the event by: ```skipper make generate-events```
3. Use the generated function for emitting the event from [internal/common/events/event.go](https://github.com/openshift/assisted-service/blob/master/internal/common/events/events.go)

### Event Definition
Event definition should specify the following attributes:
1. __name__: A unique name of the event. The name needs to remain unique and constant
as it may be referred by the service's clients (e.g. by the UI). The name should match
the structure `<event-context>_<past_tense>`.
2. __message__: A template of the message that will be rendered if it
contains any references to the properties. E.g. the message `"Install
cluster {cluster_id}"` expects the existence of a property named
`cluster_id`.
3. __event_type__: Can be either `cluster`, `host` or `infra_env`.
   1. "cluster" type requires the existence of `cluster_id` in properties.
   2. "host" type requires the existence of `host_id` and `infra_env_id` in properties.
   3. "infra_env" type requires the existence of `infra_env_id` in properties.
4. __severity__: Any of "info", "warning", "error" or "critical". See more info about severity levels [here](../events.md).
5. __properties__: A list of properties to be rendered into the message (if
   referred by) or metadata of the event (e.g. `cluster_id`, `host_id`).

### Testing
Having an explicit event per scenario assists in setting expectations in tests for the events.
An event-matcher ([internal/events/eventstest/events_test_utils.go](https://github.com/openshift/assisted-service/blob/master/internal/events/eventstest/events_test_utils.go)) simplifies the verification of expectations for each test.
E.g.:
```go
mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
	eventstest.WithNameMatcher(eventgen.QuickDiskFormatEventName),
	eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
	eventstest.WithClusterIdMatcher(host.ClusterID.String()),
	eventstest.WithMessageMatcher(message),
	eventstest.WithHostIdMatcher(host.ID.String()))).Times(times)
```

### Event Rate Limiting

Event rate limiting is implemented to prevent event flooding by throttling high-frequency events.

#### Implementation

Rate limiting is implemented in [internal/events/event.go](https://github.com/openshift/assisted-service/blob/master/internal/events/event.go):

1. **eventLimits map**: Package-level variable containing event name → duration mappings
2. **exceedsLimits()**: Checks if an event exceeds its rate limit by querying recent events
3. **InitializeEventLimits()**: Parses `EVENT_RATE_LIMITS` JSON and merges with hardcoded defaults

#### Configuration

Operators configure rate limits via the `EVENT_RATE_LIMITS` environment variable (see [docs/events.md](https://github.com/openshift/assisted-service/blob/master/docs/events.md#event-rate-limiting)).

Format: `{"event_name": "duration"}` where duration follows Go's `time.ParseDuration` format.

#### Adding Default Rate Limits

To add a hardcoded default rate limit for an event:

```go
// In internal/events/event.go
var eventLimits = map[string]time.Duration{
	commonevents.UpgradeAgentFailedEventName:   time.Hour,
	commonevents.UpgradeAgentFinishedEventName: time.Hour,
	commonevents.YourNewEventName:              30 * time.Minute,  // Add here
}
```

#### How It Works

1. **Initialization** (cmd/main.go):
   - `events.InitializeEventLimits(Options.EventRateLimits, log)` is called before DB setup
   - Custom limits from ENV var are merged into the `eventLimits` map
   - Invalid configuration causes startup failure

2. **Event Creation** (internal/events/event.go):
   - `v2SaveEvent()` calls `exceedsLimits()` before persisting
   - `exceedsLimits()` queries the database for recent events with the same name
   - If count > 0 within the limit window, the event is discarded

3. **Rate Limit Scope**:
   - Rate limits are enforced **per entity** (cluster/host/infraenv), not globally
   - The `exceedsLimits()` function conditionally filters by `cluster_id`, `host_id`, and `infra_env_id` (lines 126-134)
   - Example: `upgrade_agent_failed` limited to 1h means each host can trigger it once per hour
   - Different hosts/clusters can trigger the same event independently within their own limit windows

#### Error Handling

- **Invalid JSON**: Service fails at startup with `"Failed to parse EVENT_RATE_LIMITS json"`
- **Invalid duration**: Service fails at startup with `"Invalid duration for event 'X'"`
- **Unknown event names**: Accepted (allows rate limiting events not in hardcoded defaults)

#### Logging

- **Startup**: Logs all configured limits at INFO level via `logCurrentLimits()`
- **Runtime**: Discarded events logged at WARN level via `reportDiscarded()`