# REST-API V2 - Getting Started Guide

This document is a step-by-step guide that demonstrates how to deploy a cluster via REST-API.

**Note**:
* This document is not meant to expand on each and every resource in detail; for that, you may want to take a look at [swagger.yaml](../../swagger.yaml). Instead, expect the details needed to understand the context and what is currently happening.

* The order in which actions are performed here is **important**, as some resources depend on the existence and states of others.

## What To Expect
* A clear understanding of what happened in assisted-service backend per each action described below.

## How To Use This Guide
* Make sure that you understand how and why it is done for each step you follow.

* The steps listed below are merely a baseline; your use case may differ, and therefore, adjustments might be required.

* If you are not using assisted-service [via UI](https://github.com/openshift-assisted/assisted-installer-ui), you may want to use a client.
    * A generated [Go based client](../../client)
    * To generate a client in other languages:
        * Copy [swagger.yaml](../../swagger.yaml) to [Swagger Editor](https://editor.swagger.io/).
        * Click `Generate Client` and select a language.

## Assumptions
* You have deployed `assisted-service` and `assisted-image-service`, and can query the API.

* If relevant, you can authenticate to assisted-service - see `AUTH_TYPE` in assisted-service-config `ConfigMap` (default=none)

## What Changed - V1 to V2 
For that, please read the [REST-API V1 to V2 transition guide](rest-api-v1-v2-transition-guide.md).

## Prerequisites

### Pull Secret

* Use the secret obtained from [console.redhat.com](https://console.redhat.com/openshift/install/pull-secret)

## Register A Cluster

* `POST /v2/clusters`
* operationId: `v2RegisterCluster`

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"testcluster","high_availability_mode":"Full","openshift_version":"4.8","pull_secret":<pull_secret_here>,"base_dns_domain":"redhat.com"}' \
    <HOST>:<PORT>/api/assisted-install/v2/clusters
```

## Register An InfraEnv
* `POST /v2/infra-envs`
* operationId: `RegisterInfraEnv`

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"testcluster_infra-env","pull_secret":"<pull_secret_here>","cluster_id":"<cluster_id>","openshift_version":"4.8"}' \
    <HOST>:<PORT>/api/assisted-install/v2/infra-envs
```

## Inspect Cluster Status

* `GET /v2/clusters`
* operationId: `v2ListClusters`
```bash
curl <HOST>:<PORT>/api/assisted-install/v2/clusters/<cluster_id> | jq '.'
```
### Result
See [cluster.json](samples/cluster.json)

## Modify and Generate Discovery ISO

### Get Public Key
* In case it is locally stored, you will usually find it at `~/.ssh/id_rsa.pub`

### Patch Cluster
* `PATCH /v2/clusters/{cluster_id}`
* operationId: `V2UpdateCluster`


* Note:
  - Configure your public SSH key here to gain SSH access to your live cluster (post-install).
  - Some more advanced use-cases, which are out of scope for this document, may include multiple Infra Envs linked to the same cluster. In such a case, the SSH key you set here will be the key you use to access your cluster post-installation.



```bash
curl -X PATCH -H "Content-Type: application/json" \
    -d '{"http_proxy":"","https_proxy":"","no_proxy":"","pull_secret":"","ssh_public_key":"<public_key_here>"}' \
    <HOST>:<PORT>/api/assisted-install/v2/clusters/<cluster_id>
```

### Patch InfraEnv
* `PATCH /v2/infra-envs/{infra_env_id}`
* operationId: `UpdateInfraEnv`


* Note: Configure your public SSH key here to gain SSH access to your hosts in the discovery phase (pre-install).

```bash
curl -X PATCH -H "Content-Type: application/json" \
    -d '{"proxy":{"http_proxy":"","https_proxy":"","no_proxy":""},"ssh_authorized_key":"<public_key_here>","pull_secret":"","image_type":"full-iso"}' \
    <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_en_id>
```

## Inspect InfraEnv Status
* `GET /v2/infra-envs`
* operationId: `ListInfraEnvs`
```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id> | jq '.'
```
### Result
See [infra_env.json](samples/infra_env.json)

## Get InfraEnv Image Download URL
* `GET /v2/infra-envs/{infra_env_id}/downloads/image-url`
* operationId: `GetInfraEnvDownloadURL`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/downloads/image-url | jq '.url'
```

### Result
```json
{
    "expires_at": "0001-01-01T00:00:00.000Z",
    "url": "http://<HOST>:8080/images/<infra_env_id>?arch=x86_64&type=full-iso&version=4.8"
}
```

## Boot Hosts from Discovery Image
* Download the image using the above-mentiond url, and boot your hosts using that image.

## Inspect Registered Hosts
* `GET /v2/infra-envs/{infra_env_id}/hosts`
* operationId: `v2ListHosts`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/hosts  | jq '.'
```

### Result
See [hosts.json](samples/hosts.json)

## Start Installation
* `POST   /v2/clusters/{cluster_id}/actions/install`
* operationId: `v2InstallCluster`

:warning: **Note the `status_info` for all 3 above-mentioned hosts is `Host is ready to be installed`.**

```bash
curl -X POST <HOST>:<PORT>/api/assisted-install/v2/clusters/<cluster_id>/actions/install
```

## Check Status
You may monitor the installation progress by:
1. Inspecting the assisted-service log
2. Inspecting cluster status, [at mentioned above](#Inspect_Cluster_Status).
3. Checking Events, which you can filter by, `cluster_id`, `infra_env_id`, `host_id`:
    ```bash
    curl <HOST>:<PORT>/api/assisted-install/v2/events\?cluster_id\=<cluster_id>
    ```   

