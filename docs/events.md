# Events

Events that may be of interest to users of the Assisted Service are made available via the REST API. The events are stored in the SQL database. Each event is associated with a cluster and has a severity; some events are also associated with a host.

Using events, a user should be able to understand how the cluster reached its current state, and understand what, if anything, is wrong with it.

_Note_: Be sure not to disclose secrets or sensitive information that is not otherwise available.

## Event emission guidelines

### ERROR

Something wrong has happened that must be investigated. These should be relatively rare and are important to understand, so make them as verbose as possible - describe what happened, what the user might do to mitigate it, and anything needed to debug it.

Examples:

1. REST API call failure.
1. When an async process related to the resource fails.

### WARNING

Something unexpected happened, but we can continue. As with ERROR logs, make the messages as verbose as necessary for understanding what happened.

Examples:

1. When a previously-passing (or previously-uncomputed) validation fails.
1. When non-critical components failed to install.
1. When the cluster specs pass minimum validations but are not supported.

### INFO

This is good for marking major milestones in a flow for debuggability. Verbosity here should be as low as possible without impeding debuggability in the field. GET requests should have _no_ INFO logs. They should be added for major milestones in flows where things may go wrong and are interesting to note.

Examples:

1. When a cluster or host resource changes status.
1. When a previously-failing validation passes.
1. When a cluster or host resource progresses to a new installation stage.
