# REST-API V2 - Day2

This document is a guide that demonstrates how to add additional hosts to an existing cluster; Also known as a day2 flow.
You'll find instructions on how to add hosts to a cluster with an without [late binding](../enhancements/agent-late-binding.md).

## With Late Binding

### Register Hosts To InfraEnv

#### Register An InfraEnv

- `POST /v2/infra-envs`
- operationId: `RegisterInfraEnv`

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"testcluster_infra-env","pull_secret":"<pull_secret_here>","openshift_version":"4.9"}' \
    <HOST>:<PORT>/api/assisted-install/v2/infra-envs
```

Note: `openshift_version` is optional, if not specified it defaults to the latest available OpenShift version.

#### Get InfraEnv Image Download URL

- `GET /v2/infra-envs/{infra_env_id}/downloads/image-url`
- operationId: `GetInfraEnvDownloadURL`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/downloads/image-url | jq '.url'
```

#### Boot Hosts from Discovery Image

- Download the image using the above-mentiond url, and boot your hosts using that image.

#### Inspect Registered Hosts

- `GET /v2/infra-envs/{infra_env_id}/hosts`
- operationId: `v2ListHosts`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/hosts  | jq '.'
```

### Import A Cluster and Bind Host(s)

#### Import

- `POST /v2/clusters/import`
- operationId: `v2ImportCluster`

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"mycluster","openshift_cluster_id":"<id>","api_vip_dnsname":"api-vip.example.com"}' \
    <HOST>:<PORT>/api/assisted-install/v2/clusters/import
```

#### Bind host to Cluster

- `POST /v2/infra-envs/{infra_env_id}/hosts/{host_id}/actions/bind`
- operationId: `BindHost`

Repeat this step once per each host.

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"cluster_id":"<imported cluster_id here>"}' \
    <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_en_id>/hosts/<host_id>/actions/bind
```

#### Install Host For Day2 Cluster

- `POST /v2/infra-envs/{infra_env_id}/hosts/{host_id}/actions/install`
- operationId: `v2InstallHost`

Repeat this step once per each host.

```bash
curl -X POST -H <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_en_id>/hosts/<host_id>/actions/install
```

## Without Late Binding

### Import A Cluster And Register An InfraEnv

#### Import

- `POST /v2/clusters/import`
- operationId: `v2ImportCluster`

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"mycluster","openshift_cluster_id":"<id>","api_vip_dnsname":"api-vip.example.com"}' \
    <HOST>:<PORT>/api/assisted-install/v2/clusters/import
```

#### Register An InfraEnv

- `POST /v2/infra-envs`
- operationId: `RegisterInfraEnv`

To associate the `InfraEnv` with the imported cluster, you **must** specify the imported `cluster_id`.

```bash
curl -X POST -H "Content-Type: application/json" \
    -d '{"name":"testcluster_infra-env","pull_secret":"<pull_secret_here>","openshift_version":"4.9", "cluster_id": "<imported cluster_id>"}' \
    <HOST>:<PORT>/api/assisted-install/v2/infra-envs
```

Note: `openshift_version` is optional, if not specified it defaults to the latest available OpenShift version.

#### Get InfraEnv Image Download URL

- `GET /v2/infra-envs/{infra_env_id}/downloads/image-url`
- operationId: `GetInfraEnvDownloadURL`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/downloads/image-url | jq '.url'
```

### Register Day2 Host(s) And Install

#### Boot Hosts from Discovery Image

- Download the image using the above-mentiond url, and boot your hosts using that image.

#### Inspect Registered Hosts

- `GET /v2/infra-envs/{infra_env_id}/hosts`
- operationId: `v2ListHosts`

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_env_id>/hosts  | jq '.'
```

#### Install Host For Day2 Cluster

- `POST /v2/infra-envs/{infra_env_id}/hosts/{host_id}/actions/install`
- operationId: `v2InstallHost`

Repeat this step once per each host.

```bash
curl -X POST -H <HOST>:<PORT>/api/assisted-install/v2/infra-envs/<infra_en_id>/hosts/<host_id>/actions/install
```
