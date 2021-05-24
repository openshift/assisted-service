# OCM Integration

OpenShift Cluster Manager (OCM) is a managed service where users can install, operate and upgrade Red Hat OpenShift 4 clusters.  This document describes the integration was done such that Assisted Installed (AI) clusters are shown as part of a user's clusters in the OCM UI.
To achieve this, the assisted-installer users the [OCM client](https://github.com/openshift-online/ocm-sdk-go) to make several calls during a cluster's installation.

## AMS

AMS is the micro-service in OCM which holds the users' clusters list, which is indeed a list of `Subscriptions` AMS objects, and handles the authZ for assisted-service API calls.

## Cluster lifecycle

### Cluster registration

On cluster registration, the service will create an AMS subscription for the cluster with some initial values:
```
status: "Reserved"
cluster_id: The cluster id registered in AI DB.
display_name: The cluster name in AI DB.
product_id: "OCP"
product_category: "AssistedInstall"
```

### Cluster is renamed

In this case, the assisted-service will patch the subscription with the new cluster name.

### Cluster installation

The assisted-service contacts AMS at several points during the cluster installation process.

Once an `openshift_cluster_id` is generated during the `preparing-for-installation` state, the service will patch the subscription with:
```
external_cluster_id: Openshift cluster ID
```

Later on, when console operator is installed during the `finalizing` state, the service will patch the subscription with:
```
console_url: Cluster's console URL
```

Finally, when the cluster is successfully installed, the service will patch the subscription with:
```
status: "Active"
```

If installation fails, the subscription's status remains `Reserved`, therefore, the subscription stays untouched in AMS until the cluster is deleted from AI.
In addition, in case `external_cluster_id` was already updated in the subscription, it will remain obsolete until the cluster is deleted from AI or the user restarts the installation, a new `openshift_cluster_id` is generated and the obsolete id that the subscription currently holds is overwritten by the new one.

### Cluster is sending metrics to Openshift Telemeter

Once metrics from the installed cluster reach the Telemeter server, Telemeter will notify AMS and AMS will search for a subscription with a matching `external_cluster_id` (which is included in the metrics that the cluster sends). If it finds such a subscription it will add a `metrics` field to the subscription, otherwise, it will create a new subscription for that "unsubscribed" cluster but in this case, all the data patched by AI will be missing - this should be a bug indication. 

### Cluster deletion

On cluster deregistration, we have 2 flows:

#### Clusters with `Active` subscription

Those clusters are fully installed and are running on the clients' machines, therefore, when deleting those clusters from the service, whether is deleted by the user or by GC, the subscription is left untouched and the client will continue to see his cluster on the OCM UI.

AMS is refreshing subscriptions periodically and if a running cluster stop delivering metrics for some time it will change its subscription status to "Stale" and after a longer period to `Archived`

#### Clusters with `Reserved` subscription

Those clusters where not installed for some reason and AMS won't monitor those subscriptions, therefore, it is AI's responsibility to delete those subscriptions in AMS if the cluster is deleted from the service.

## How to see the subscription in AMS using the OCM-cli

You can sign-in to AMS using the [ocm-cli](https://github.com/openshift-online/ocm-cli) in order to get information regarding your subscriptions.
First, follow how to [install](https://github.com/openshift-online/ocm-cli#installation) ocm-cli.

Then you need to log in to your user:
```
ocm login --token <your token from https://cloud.redhat.com/openshift/token>
```

You can also use assisted-service service-account authZ to get subscription that are owned by other users using the following command:
```
ocm login --client-id <id> --client-secret <secret> --url=<url>
```

For the cloud use:
```
--client-id assisted-installer-int, --url=https://api.integration.openshift.com
--client-id assisted-installer-stage, --url=https://api.stage.openshift.com
--client-id assisted-installer-prod, --url=https://api.openshift.com
```

Then you can query AMS for data (use `jq` to process the result):
```
// get a list of 100 subscriptions max
ocm get subscriptions

// get a specific subscription, you can find 'ams_subscription_id' in the cluster metadata
ocm get subscription <subscription ID>

// you can filter subscriptions using the --parameter flag:
ocm get subscriptions --parameter search="cluster_id = '<cluster ID>'"
ocm get subscriptions --parameter search="external_cluster_id = '<external cluster ID>'"
ocm get subscriptions --parameter search="status = 'Active'"
...
```

## This is how a full subscription looks likes after all the steps above

```
{
  "id": "1svZIyseCY2KM9J1V0OYaAIxWjB",
  "kind": "Subscription",
  "href": "/api/accounts_mgmt/v1/subscriptions/1svZIyseCY2KM9J1V0OYaAIxWjB",
  "plan": {
    "id": "OCP-AssistedInstall",
    "kind": "Plan",
    "href": "/api/accounts_mgmt/v1/plans/OCP-AssistedInstall",
    "type": "OCP",
    "category": "AssistedInstall"
  },
  "cluster_id": "fcf4c3c2-79a0-422c-8754-27cf02dfa9d2",
  "external_cluster_id": "da1c2141-9aaf-477b-b061-ab9cf5746ae9",
  "organization_id": "1gEOo7TCnW5JGwsw0ULeUH4l53m",
  "last_telemetry_date": "2021-05-23T08:00:18.145764Z",
  "created_at": "2021-05-23T07:29:07.979367Z",
  "updated_at": "2021-05-23T08:02:38.46325Z",
  "support_level": "Eval",
  "display_name": "assisted-test-cluster-22ac517-assisted-installer",
  "creator": {
    "id": "1gEOnuuPzqAUbLNV5QAoS4EhYfy",
    "kind": "Account",
    "href": "/api/accounts_mgmt/v1/accounts/1gEOnuuPzqAUbLNV5QAoS4EhYfy"
  },
  "managed": false,
  "status": "Active",
  "provenance": "Provisioning",
  "last_reconcile_date": "0001-01-01T00:00:00Z",
  "console_url": "https://console-openshift-console.apps.assisted-test-cluster-22ac517-assisted-installer.redhat.com",
  "last_released_at": "0001-01-01T00:00:00Z",
  "metrics": [
    {
      "health_state": "healthy",
      "query_timestamp": "2021-05-23T08:15:07Z",
      "memory": {
        "updated_timestamp": "2021-05-23T08:15:07.824Z",
        "used": {
          "value": 23618293760,
          "unit": "B"
        },
        "total": {
          "value": 52269928448,
          "unit": "B"
        }
      },
      "cpu": {
        "updated_timestamp": "2021-05-23T08:15:13.033Z",
        "used": {
          "value": 2.99952380952381,
          "unit": ""
        },
        "total": {
          "value": 12,
          "unit": ""
        }
      },
      "sockets": {
        "updated_timestamp": "0001-01-01T00:00:00Z",
        "used": {
          "value": 0,
          "unit": ""
        },
        "total": {
          "value": 0,
          "unit": ""
        }
      },
      "compute_nodes_memory": {
        "updated_timestamp": "1970-01-01T00:00:00Z",
        "used": {
          "value": 0,
          "unit": "B"
        },
        "total": {
          "value": 0,
          "unit": "B"
        }
      },
      "compute_nodes_cpu": {
        "updated_timestamp": "0001-01-01T00:00:00Z",
        "used": {
          "value": 0,
          "unit": ""
        },
        "total": {
          "value": 0,
          "unit": ""
        }
      },
      "compute_nodes_sockets": {
        "updated_timestamp": "0001-01-01T00:00:00Z",
        "used": {
          "value": 0,
          "unit": ""
        },
        "total": {
          "value": 0,
          "unit": ""
        }
      },
      "storage": {
        "updated_timestamp": "1970-01-01T00:00:00Z",
        "used": {
          "value": 0,
          "unit": "B"
        },
        "total": {
          "value": 0,
          "unit": "B"
        }
      },
      "nodes": {
        "total": 3,
        "master": 3
      },
      "operating_system": "",
      "upgrade": {
        "updated_timestamp": "2021-05-23T08:15:12.581Z",
        "available": true
      },
      "state": "ready",
      "state_description": "",
      "openshift_version": "4.7.9",
      "cloud_provider": "baremetal",
      "region": "",
      "console_url": "https://console-openshift-console.apps.assisted-test-cluster-22ac517-assisted-installer.redhat.com",
      "critical_alerts_firing": 0,
      "operators_condition_failing": 0,
      "subscription_cpu_total": 0,
      "subscription_socket_total": 0,
      "subscription_obligation_exists": 1,
      "cluster_type": ""
    }
  ],
  "cloud_provider_id": "baremetal",
  "trial_end_date": "0001-01-01T00:00:00Z"
}
```
