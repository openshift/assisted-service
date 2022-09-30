---
title: on-prem-data-collection
authors:
  - "@CrystalChun"
creation-date: 2023-01-18
last-updated: 2023-01-18
---

# On-premises Data Collection

## Summary

The Assisted Installer ("AI") will provide a process to collect event data about cluster installations when it is deployed on-premises as part of either [Red Hat Advanced Cluster Management for Kubernetes](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.6) ("RHACM") or as a stand-alone Podman deployment. Data was already collected from clusters installed using [Red Hat's Hybrid Cloud Console](https://console.redhat.com/openshift/assisted-installer/clusters) (“SaaS”), but there was no method for obtaining the same data from on-premises deployments.

## Motivation

Data provides insight in order to improve the product for our users.

### Goals

1. Create a process to send [event](https://github.com/openshift/assisted-service/tree/master/docs/dev/events.md) data from on-premises deployments of AI.
2. Analyze and process on-premises data (by use of visualizations, dashboards, etc.) at Red Hat similarly to SaaS data.
3. Analyze on-premises data both separately and together with SaaS data.


### Non-Goals

1. Implementing new data types to collect is out of the scope of this epic. This epic defines how and when the data already collected will be processed and sent.
2. Define how SaaS should redesign their data pipeline (taken care of in [MGMT-11244](https://issues.redhat.com/browse/MGMT-11244)) is out of the scope of this epic. This epic only refers to on-premises deployments of AI.
3. Define the creation and/or management of an event streaming instance such as Kafka and the endpoint this data will be hosted at (both taken care of in [MGMT-11244](https://issues.redhat.com/browse/MGMT-11244)).
4. Collecting must-gather data (and logs) for debugging failed cluster installation is outside the scope of this epic. This epic is only concerned with [event](https://github.com/openshift/assisted-service/blob/6b8401d3b0f0ceea5a837f33c17b9b1cd5ec52a3/internal/events/api/event.go) data.
5. Data formatting and handling by the event streaming service is out of the scope of this epic. The event streaming service should be able to process the data from all versions of the on-premises environment.
    - Similarly, the handling of data received from the event streaming service is out of the scope of this epic.

## Proposal

### Data Sending Flow

All of the event data for a cluster will be queried from the DB, packaged, and sent to the data hosting endpoint when a cluster installation attempt finishes (regardless of success or failure).

Sending data should not interfere with customer workloads and deployments.

NOTE: Data from pre-existing (already installed) cluster(s) using previous/older versions of AI will not be sent when this feature is enabled.

### Assisted-Service Changes 

Changes that need to be made in the `Assisted-Service` to support this:
- Needs to know what the data hosting service endpoint is and how to authenticate into it
- Needs code changes when an install is attempted to query and send data
- Needs to know how to format the data to send to the data hosting service
    - Includes sanitizing the data such as hashing user's emails like in the [event scraper](https://github.com/openshift-assisted/assisted-events-scrape/blob/master/assisted-events-scrape/utils/anonymizer.py#L25)
- Needs to expose an option for users to opt-out of this feature

### Additional Changes

- Documentation about how to opt-out
- Documentation of requirements: non-disconnected installation

### User Stories

#### Story 1

As an infrastructure operator, I can provision and manage my fleet of clusters knowing that data points are automatically collected and sent to the Assisted Installer team without having to do any additional configuration. I know my data will be safe and secure and the team will only collect data they need to improve the product. 

#### Story 2

As a developer on the Assisted Installer team, I can analyze the customer data to determine if a feature is worth implementing/keeping/improving. I know that the customer data is accurate and up-to-date. All of the data is parse-able and can be easily tailored to the graphs/visualizations that help my analysis.

#### Story 3

As a product owner, I can determine if the product is moving in the right direction based on the actual customer data. I can prioritize features and bug fixes based on the data.

### Implementation Details/Notes/Constraints

Assisted Service changes:
1. New environment variables to be added:
    - `ENABLE_DATA_COLLECTION`: If set to `false`, it will disable this feature and thereby allow a customer to opt-out, otherwise the feature is enabled by default.
    - `DATA_UPLOAD_ENDPOINT`: The URL the data will be sent to.
2. Add data collection and sending logic
    - Collecting and sending: querying the DB for events of that cluster, sanitizing the data, and sending it as a tar.gz to the `DATA_UPLOAD_ENDPOINT` over HTTPS.
    - Called when a cluster reaches one of the following states: `error`, `cancelled`, `installed`
            - Each of these cluster stages represent an end state and that the cluster attempted installation.
    - Sending data is non-blocking and sent as best effort
3. Add logic for authenticating to the `DATA_UPLOAD_ENDPOINT` by using the user's OCM pull secret
4. Add opt-out logic: 
    - If the user has [opted out of Telemetry](https://docs.openshift.com/container-platform/4.11/support/remote_health_monitoring/opting-out-of-remote-health-reporting.html) (missing cloud.openshift.com from the global pull secret)
    - If the user has set `ENABLE_DATA_COLLECTION` to `false`.

### Risks and Mitigations

#### Risk 1: Overusing Customer's Network & CPU

On-premises users might have large-scale clusters so network usage and processor usage could be impacted depending on how large the data set is.

##### Mitigation

To avoid user workflow interruption, data collection, processing, and sending can happen as a concurrent thread. Data sending should be non-blocking and best effort.

#### Risk 2: Storage Limits and Data Loss

Large-scale clusters most likely means large data sets that are sent to the event streaming service. There is a risk that amount of data could overflow the storage limits of the server used.

Furthermore, if the limits are reached, how will newer data be handled? Will the pre-existing data get overwritten or will the new data be rejected? There's potential for lost data here.

##### Mitigation

More research needs to be done here to see what the limits are and how we can overcome them. If using RH Ingress, there could be a daily job that would export data from here and import it into our own storage.

#### Risk 3: DDoS-ing Insights Ingress Server

Without any constraints, it's possible for the assisted service to send data to the data collection server repeatedly. This can prevent other clients from sending their data, and cause loss of data, or prevent us from reaching the data since the server will be busy with the repeated connections.

##### Mitigation

1. Clearly defining when events are sent (see `Data Sending Flow` under the `Proposal` section above)
2. Data is sent as best effort
3. Retry will be implemented as a future enhancement

#### Risk 4: Duplicate Data

With all the cases for sending data, there's a high chance for duplicated data to be sent to the data collection server. It can skew the analysis of data and it won't be representative of the actual data from the field.

##### Mitigation

A field can be used as a marker to indicate the data has been sent to the event streaming service or that the data already exists. One option is to use the timestamp, cluster ID, and user email to detect duplicate events.

## Design Details

### UI Impact

No UI changes will need to be made.

### Test Plan

#### Test 1

Add a check at the end of a cluster install attempt that will query the data hosting endpoint and verify the payload made it to the data hosting service.

#### Test 2

Check against a large-scale cluster with > 100 hosts to verify:
1. All of the cluster's events are being stored, queried, and sent (within a reasonable time)
2. That the event streaming can receive and process the large data set

## Drawbacks

- Dependency on the data hosting service selected in [MGMT-11244](https://issues.redhat.com/browse/MGMT-11244) could cause delays in this epic being completed.
- Won't get data from disconnected environment users since this solution requires a connection to push the data

## Alternatives

### Alternative Solution 1: Customer Uploads Data

#### Details

The Assisted Service would provide an endpoint that the customer can query to get all data that they would upload to us.

#### Benefits

- Support disconnected deployments since the service will not be pushing the data.
- Minimizes risk of DDoS-ing the data streaming service

#### Drawbacks

- Likely will not get data
    - Due to the customer having to do extra work to upload the data
- Data would be skewed since we'd only get data when the customer uploads it
    - Customer is most likely only going to upload when they have an issue
