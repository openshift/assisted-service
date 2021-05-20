Assisted Installer Stand-Alone
==============================

This document describes the process of running the Assisted Installer in
stand-alone mode via `podman`.

# Pre-Requisites

## OpenShift User Pull Secret

You will need a valid OpenShift user pull secret. Copy or download the pull
secret from https://cloud.redhat.com/openshift/install/pull-secret

# Running the Assisted Installer using Podman

## Environment

The first thing you will need to do is grab the
[`onprem-environment`](https://raw.githubusercontent.com/openshift/assisted-service/master/onprem-environment)
file. Once you have this file, source it.

```
source onprem-environment
```

**NOTE**
* The remainder of this document relies on the values stored in
    `onprem-environment` being set in the shell.
* The `SERVICE_BASE_URL` is the `ip:port` where the assisted-service
    API is being served. The Assisted Installer's agent uses the
    `SERVICE_BASE_URL` to talk back to the API.


## NGINX Configuration

Once you have sourced `onprem-environment`, you will need to grab the
[`nginx.conf`](https://raw.githubusercontent.com/openshift/assisted-service/master/deploy/ui/nginx.conf)
used to configure the Assisted Installer's UI. There are two fields of note:

1. `listen 8080;` refers to the port used to access the Assisted Installer's UI.
  As an example, if you wanted the UI to listen on port `9090` to avoid conflict
  with a port already used on the host you would `sed -i "s|listen.*;|listen 9090;|" nginx.conf`.
1. `proxy_pass http://localhost:8090;` is the default value of `SERVICE_BASE_URL`.
  You could update this with, `sed -i "s|proxy_pass.*;|proxy_pass $SERVICE_BASE_URL;|" nginx.conf`.

## Create the Assisted Installer Pod

Once you have made any adjustments to ports as necessary, you can create the
assisted-installer pod.

```
podman pod create --name assisted-installer -p 5432:5432,8080:8080,8090:8090
```

**NOTE**
The ports allocated to the `assisted-installer` should be updated to reflect any
changes required for your configuration.

* `5432` is the port for Database communication
* `8080` is the port for accessing the Assisted Installer's UI
* `8090` is the port referenced in `SERVICE_BASE_URL`; the URL used by the
    Assisted Installer's agent to talk back to the assisted-service.

## Start PostgreSQL

Use podman to run postgreSQL.

```
podman run -dt --pod assisted-installer \
  --name db \
  --env-file onprem-environment \
  --pull always \
  quay.io/ocpmetal/postgresql-12-centos7
```

**NOTE**
* `onprem-environment` is the file downloaded and modified previously

## Start Assisted Service

Use podman to start the Assisted Service.

```
podman run -dt --pod assisted-installer \
  --name installer \
  --env-file onprem-environment \
  --pull always \
  --restart always \
  quay.io/ocpmetal/assisted-service:latest /assisted-service
```

**NOTE**
* `onprem-environment` is the file downloaded and modified previously
* If you modified the port for `SERVICE_BASE_URL` you would add `--port ${SERVICE_API_PORT}`

## Start Assisted Installer UI

```
podman run -dt --pod assisted-installer \
  --name ui \
  --env-file onprem-environment \
  --pull always \
  -v ${PWD}/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z \
  quay.io/ocpmetal/ocp-metal-ui:latest
```

**NOTE**
* `onprem-environment` is the file downloaded and modified previously
* `$(PWD)/nginx.conf` references the previously downloaded -- and potentially
    modified -- `nginx.conf`

# Accessing the Assisted Installer

At this stage, you should be able to access the Assisted Installer UI at
`http://localhost:8080`
