# Event Rate Limits

Event rate limits are implemented to prevent event flooding by throttling high-frequency events on a per-cluster, per-host, or per-infraenv basis.

## Overview

The Assisted Service supports configurable rate limiting to prevent event flooding. Rate limiting throttles high-frequency events, ensuring that the same event cannot be emitted multiple times within a specified time window for the same entity (cluster, host, or infraenv).

## Implementation

Rate limiting is implemented in [`internal/events/event.go`](https://github.com/openshift/assisted-service/blob/master/internal/events/event.go):

1. **`eventLimits` map**: Package-level variable containing event name → duration mappings
2. **`exceedsLimits()`**: Checks if an event exceeds its rate limit by querying recent events from the database
3. **`InitializeEventLimits()`**: Parses `EVENT_RATE_LIMITS` JSON and merges with hardcoded defaults

### Default Rate Limits

Some events have hardcoded default rate limits defined in the [`eventLimits` map](https://github.com/openshift/assisted-service/blob/master/internal/events/event.go#L189-L192). These defaults can be overridden via configuration (see [Configuration](#configuration) below).

## Configuration

Rate limits are configured via the `EVENT_RATE_LIMITS` environment variable. The configuration method varies by deployment type:

### Format

The configuration uses JSON format with event names as keys and duration strings as values:

```json
{"event_name": "duration"}
```

Duration format follows Go's `time.ParseDuration` syntax:
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h30m` - 1 hour 30 minutes
- `2h` - 2 hours

### Direct Deployments

For direct deployments, set the environment variable:

```bash
export EVENT_RATE_LIMITS='{"upgrade_agent_failed":"2h","infra_env_deregister_failed":"30m"}'
```

### SaaS Deployments

For SaaS deployments, `EVENT_RATE_LIMITS` has been added to the OpenShift template to allow Red Hat SREs to manage rate limiting for customers. 

### ACM/Infrastructure Operator Deployments

For ACM/Infrastructure Operator deployments, override via custom ConfigMap using the annotation pattern:

1. Create a ConfigMap with `EVENT_RATE_LIMITS`:

```bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-assisted-service-config
  namespace: assisted-installer
data:
  EVENT_RATE_LIMITS: '{"upgrade_agent_failed":"2h","infra_env_deregister_failed":"30m"}'
EOF
```

2. Apply annotation to AgentServiceConfig:

```bash
oc annotate --overwrite AgentServiceConfig agent \
  unsupported.agent-install.openshift.io/assisted-service-configmap=my-assisted-service-config
```

3. Restart the deployment to apply changes:

```bash
oc rollout restart deployment/assisted-service -n assisted-installer
```

See [Operator documentation](../operator.md#specifying-environmental-variables-via-configmap) for more details on custom ConfigMap overrides.

### Configuration Behavior

- **Default limits**: Some events have hardcoded rate limits (e.g., `upgrade_agent_failed: 1h`)
- **Custom overrides**: Your configuration overrides defaults for specified events
- **Additive**: You can add rate limits for events not in the defaults
- **Per-entity**: Limits apply per cluster/host/infraenv, not globally

## How It Works

### Initialization

1. **Startup** (`cmd/main.go`):
   - `events.InitializeEventLimits(Options.EventRateLimits, log)` is called before DB setup
   - Custom limits from `EVENT_RATE_LIMITS` environment variable are parsed and merged into the `eventLimits` map
   - Custom limits override hardcoded defaults
   - **If initialization fails, the service fails to start** (failOnError terminates the process)

2. **Validation**:
   - Invalid JSON format causes startup failure with error containing: `"failed to parse EVENT_RATE_LIMITS json"`
   - Invalid duration format causes startup failure with error containing: `"invalid duration for event"`
   - Unknown event names are accepted (allows rate limiting events not in hardcoded defaults)

### Event Creation

1. **Event Persistence** (`internal/events/event.go`):
   - `v2SaveEvent()` calls `exceedsLimits()` before persisting the event
   - `exceedsLimits()` queries the database for recent events with the same name within the limit window
   - If count > 0 within the limit window, the event is discarded and the transaction is rolled back

2. **Rate Limit Scope**:
   - Rate limits are configured globally (one limit per event name), but are applied at runtime based on the entity to which the event is scoped
   - The `exceedsLimits()` function queries for recent events with the same name, scoped to the entity IDs present in the current event (`cluster_id`, `host_id`, `infra_env_id`)
   - This allows different entities (hosts/clusters/infraenvs) to trigger the same event independently within their own limit windows

### Example

With this configuration:
```bash
EVENT_RATE_LIMITS='{"upgrade_agent_failed":"2h"}'
```

If a host generates multiple `upgrade_agent_failed` events within 2 hours, only the first is recorded. Subsequent events are discarded and logged as warnings.

## Adding Default Rate Limits

To add a hardcoded default rate limit for an event:

1. Edit `internal/events/event.go`
2. Add the event to the `eventLimits` map:

```go
var eventLimits = map[string]time.Duration{
	commonevents.UpgradeAgentFailedEventName:   time.Hour,
	commonevents.UpgradeAgentFinishedEventName: time.Hour,
	commonevents.UpgradeAgentStartedEventName:  time.Hour,
	commonevents.YourNewEventName:              30 * time.Minute,  // Add here
}
```

## Logging

### Startup Logging

At startup, the service logs all configured limits at DEBUG level via `logCurrentLimits()`:

```
DEBUG Event rate limits configured  upgrade_agent_failed=2h0m0s upgrade_agent_finished=1h0m0s
```

If no limits are configured:
```
DEBUG No event rate limits configured
```

### Runtime Logging

Discarded events are logged at WARN level via `reportDiscarded()`:

```
WARN Event will be discarded  name=upgrade_agent_failed limit=2h0m0s count=1 cluster_id=<uuid> host_id=<uuid>
```

The log includes:
- Event name
- Rate limit duration
- Count of recent events found
- Associated entity IDs (cluster_id, host_id, infra_env_id)
- Event metadata (category, request_id, props, severity, message, time)

## Error Handling

### Startup Errors

If `InitializeEventLimits()` fails during startup, the service will not start:

- **Invalid JSON**: Service fails at startup with error containing: `"failed to parse EVENT_RATE_LIMITS json"`
- **Invalid duration**: Service fails at startup with error containing: `"invalid duration for event"`

### Runtime Errors

- **Unknown event names**: Accepted (allows rate limiting events not in hardcoded defaults)
- **Database errors during limit check**: Event creation fails and is logged as an error, but does not prevent the service from running
