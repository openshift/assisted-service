# Events

Events that may be of interest to users of the Assisted Service are made available via the REST API.  The events are stored in the SQL database.  Each event is associated with a cluster and has a severity; some events are also associated with a host.

Using events, a user should be able to understand how the cluster reached its current state, and understand what, if anything, is wrong with it.

*Note*: Be sure not to disclose secrets or sensitive information that is not otherwise available.

## Event emission guidelines

### ERROR
Something wrong has happened that must be investigated.  These should be relatively rare and are important to understand, so make them as verbose as possible - describe what happened, what the user might do to mitigate it, and anything needed to debug it.

Examples:
1. REST API call failure.
1. When an async process related to the resource fails.

### WARNING
Something unexpected happened, but we can continue.  As with ERROR logs, make the messages as verbose as necessary for understanding what happened.

Examples:
1. When a previously-passing (or previously-uncomputed) validation fails.
1. When non-critical components failed to install.
1. When the cluster specs pass minimum validations but are not supported.

### INFO

This is good for marking major milestones in a flow for debuggability.  Verbosity here should be as low as possible without impeding debuggability in the field.  GET requests should have *no* INFO logs.  They should be added for major milestones in flows where things may go wrong and are interesting to note.

Examples:
1. When a cluster or host resource changes status.
1. When a previously-failing validation passes.
1. When a cluster or host resource progresses to a new installation stage.

## Event streaming

Events are streamed to an event stream, along with some resources state and metadata.

```json
{
  "name": "MyEventName",
  "payload": {
     "message": "my message",
     "foo": "bar"
  },
  "metadata": {
    "versions": {
      "assisted-installer": "quay.io/edge-infrastructure/assisted-installer:latest",
      "assisted-installer-controller": "quay.io/edge-infrastructure/assisted-installer-controller:latest",
      "assisted-installer-service": "Unknown",
      "discovery-agent": "quay.io/edge-infrastructure/assisted-installer-agent:latest"
    }
  }
}
```

We will also stream ClusterState Host and InfraEnv changes.

### Event stream

The event stream is implemented in Kafka (RHOSAK Kafka instance for integration/stage and production).
Locally, a single-node instance of kafka is used.

#### Local development

To deploy kafka we need to have the following env var enabled:
```
export ENABLE_EVENT_STREAMING=true
```

Then run:
```
skipper make deploy-all
```

This will deploy kafka and the assisted service with the right env vars.

#### Read event stream

To read events from the event stream, we can run the following:

```bash
oc exec -it ai-kafka-0 -- kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic events-stream \
    --from-beginning
```

#### Impact on reliability of the service

There are a few possible scenarios:
* feature flag is turned off: streaming events it's just silently ignored (debug message at the start will explicitly say that event stream is not configured)
* feature flag is turned on, and event stream is working: all messages are delivered
* feature flag is turned on, and event stream is badly configured (i.e. invalid parameters): application will not start
* feature flag is turned on, and event stream is configured with a bad url: app will start but will fail every single event stream, generating a warning log line. This is because the client uses lazy connection and automatically retries to estabilish it (it helps when the URL does work but we have unreliable connection)
* feature flag is turned on, event stream is partially working: some events stream will fail with a warning log line
