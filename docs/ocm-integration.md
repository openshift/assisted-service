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
ocm login --token <your token from https://console.redhat.com/openshift/token>
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

## How to add organization capabilities to AMS

AMS has the concept of _organization capabilities_ that we can use to enable
and disable feature for certain organizations. To one of this capabilities the
first step is to add it to the AMS code. That requires a patch similar to
[this](https://gitlab.cee.redhat.com/service/uhc-account-manager/-/merge_requests/3168):

```patch
commit 5461cd16723bffdca00c0fe0dafd9f218bda67a5
Author: Mat Kowalski <mko@redhat.com>
Date:   Thu Sep 15 13:49:55 2022 +0200

    MGMT-11563: Add bare metal installer multiarch capability

    Contributes-to: MGMT-11563
    Contributes-to: MGMT-11859
    Implements-enhancement: saas-per-organization-feature-access

diff --git a/pkg/api/capability_types.go b/pkg/api/capability_types.go
index 6e909a7a..ebaee754 100644
--- a/pkg/api/capability_types.go
+++ b/pkg/api/capability_types.go
@@ -28,6 +28,7 @@ const CapabilitySubscribedOcpMarketplace = "capability.cluster.subscribed_ocp_ma
 const CapabilitySubscribedOsdMarketplace = "capability.cluster.subscribed_osd_marketplace"
 const CapabilityEnableTermsEnforcement = "capability.account.enable_terms_enforcement"
 const CapabilityBareMetalInstallerAdmin = "capability.account.bare_metal_installer_admin"
+const CapabilityBareMetalInstallerMultiarch = "capability.account.bare_metal_installer_multiarch"
 const CapabilityReleaseOcpClusters = "capability.cluster.release_ocp_clusters"
 const CapabilityAutoscaleClustersDeprecated = "capability.organization.autoscale_clusters"
 const CapabilityAutoscaleClusters = "capability.cluster.autoscale_clusters"
@@ -60,6 +61,7 @@ var CapabilityKeys = []string{
        CapabilitySubscribedOcpMarketplace,
        CapabilitySubscribedOsdMarketplace,
        CapabilityBareMetalInstallerAdmin,
+       CapabilityBareMetalInstallerMultiarch,
        CapabilityReleaseOcpClusters,
        CapabilityEnableTermsEnforcement,
        CapabilityUseRosaPaidAMI,
```

That needs to be merged, and then the new version of AMS pushed to the
corresponding environment (integration, stage and eventually production). The
SD-B team is responsible for that, ping the `@sd-b-team` in the
[#service-development-b](https://coreos.slack.com/archives/CBDNMS43V) channel
in Slack.

The second step is to enable the capability for the organizations that you
want, so you need to find the identifier of that organization. For example, for
your own organization. For example, for yourself:

```shell
$ ocm whoami | jq -r '.organization.id'
2GOuuT2l3odeHRdWqY57fviSE2K
```

And for a given user name:

```shell
$ ocm get /api/accounts_mgmt/v1/accounts -p search="username = 'jane.doe'" | jq -r '.items[].organization.id'
2GOuxr72XcFbrWvyA9rS9McOUjO
```

Then add the label to the organization (labels are the mechanism used by AMS to
store these capabilities):

```shell
$ ocm post /api/accounts_mgmt/v1/organizations/2GOuxr72XcFbrWvyA9rS9McOUjO/labels <<.
{
  "key": "capability.organization.bare_metal_installer_multiarch",
  "value": "true",
  "internal": true
}
.
```

To disable a capability just delete the label:

```shell
$ ocm delete /api/accounts_mgmt/v1/organizations/2GOuxr72XcFbrWvyA9rS9McOUjO/labels/capability.organization.bare_metal_installer_multiarch
```

To check the capabilities enabled for an organization:

```shell
$ ocm get /api/accounts_mgmt/v1/organizations/2GOuxr72XcFbrWvyA9rS9McOUjO/labels | jq -r '.items[] | select(.type == "Organization") | .key'
sts_ocm_role
capability.organization.bare_metal_installer_multiarch
capability.organization.hypershift
capability.cluster.subscribed_ocp
moa_entitlement_expires_at
```

Chances are that you will not have permission to enable or disable
capabilities, specially in the production environment. In that case open a
ticket like [this](https://issues.redhat.com/browse/SDB-3211) in the
[SDB](https://issues.redhat.com/projects/SDB) board requesting it. Make sure to
include in the ticket the name of the capability that you want to enable, the
identifier of the organization and the environment. For example, you could can
write a title and description like these:

```
Enable `bare_metal_installer_multiarch` capability for `My Org` in production

We need to enable the `bare_metal_installer_multiarch` capability for the
`2GOuxr72XcFbrWvyA9rS9McOUjO` organization (also known as `My Org`) in the
production environment. This is necessary to enable multiarch support in the
assisted installer service.

Adding the capability should be something like this:

{noformat}
$ ocm login --token ... --url production
$ ocm post /api/accounts_mgmt/v1/organizations/2GOuxr72XcFbrWvyA9rS9McOUjO/labels <<.
{
  "key": "capability.organization.bare_metal_installer_multiarch",
  "value": "true",
  "internal": true
}
{noformat}
```

Note that organization identifiers and capabilities are environment specific,
so make sure you do it in the right environment. For example, to use the
integration environment:

```shell
$ ocm login --token ... --url integration
```

To use the stage environment:

```shell
$ ocm login --token ... --url stage
```

The default, when the `--url` option isn't provided is to use the production
environment.

If you need to do this very often in the integration or stage environments, and
without asking the SDB team, you can request the `BareMetalInstallerAdmin` and
`UHCSupport` roles sending a merge request similar to
[this](https://gitlab.cee.redhat.com/service/ocm-resources/-/merge_requests/2713)
to the [ocm-resources](https://gitlab.cee.redhat.com/service/ocm-resources)
project. Note that for the production environment this will not be accepted, so
you will still need to open a ticket.
