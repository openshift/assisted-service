## Events

Events generation is designed to expose a uniformed method of
initializing the assisted-service events.
Each event definition requires a set of properties based on its type.
The definition is used for generating a function for emitting the event with
the required parameters.

### Adding an Event

In order to add a new event, follow the next steps:

1. Add event definition to [docs/events.yaml](https://github.com/openshift/assisted-service/blob/master/docs/events.yaml)
2. Generate the code for creating the event by: `skipper make generate-events`
3. Use the generated function for emitting the event from [internal/common/events/event.go](https://github.com/openshift/assisted-service/blob/master/internal/common/events/events.go)

### Event Definition

Event definition should specify the following attributes:

1. **name**: A unique name of the event. The name needs to remain unique and constant
   as it may be referred by the service's clients (e.g. by the UI). The name should match
   the structure `<event-context>_<past_tense>`.
2. **message**: A template of the message that will be rendered if it
   contains any references to the properties. E.g. the message `"Install cluster {cluster_id}"` expects the existence of a property named
   `cluster_id`.
3. **event_type**: Can be either `cluster`, `host` or `infra_env`.
   1. "cluster" type requires the existence of `cluster_id` in properties.
   2. "host" type requires the existence of `host_id` and `infra_env_id` in properties.
   3. "infra_env" type requires the existence of `infra_env_id` in properties.
4. **severity**: Any of "info", "warning", "error" or "critical". See more info about severity levels [here](../events.md).
5. **properties**: A list of properties to be rendered into the message (if
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
