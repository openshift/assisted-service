Assisted Installer Stand-Alone
==============================

This document describes the process of running the Assisted Installer in
stand-alone mode via `podman`.

# Pre-Requisites

## OpenShift User Pull Secret

You will need a valid OpenShift user pull secret. Copy or download the pull
secret from https://cloud.redhat.com/openshift/install/pull-secret

## Download CoreOS Installer

This is used by the assisted-service to generate the discovery ISO. You can grab
it from a released container image by:

```
podman run --privileged --pull=always -it --rm \
  -v .:/data --entrypoint /bin/cp \
  quay.io/coreos/coreos-installer:v0.7.0 /usr/sbin/coreos-installer /data/coreos-installer
```

## Download RHCOS Live ISO

The RHCOS Live ISO is used as the base when building discovery ISOs. These
images can be found at https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos
Later on, in this document, you will see it referred to as `livecd.iso`.

To pull the latest RHCOS Live ISO for OpenShift 4.6 and save it as `livecd.iso`.

```
curl https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/latest/rhcos-live.x86_64.iso -o livecd.iso
```

**NOTE** 
The RHCOS images might not change with every release of OpenShift Container Platform.
You must download images with the highest version that is less than or equal to the
OpenShift Container Platform version that you install. Use the image versions that
match your OpenShift Container Platform version if they are available.

# Running the Assisted Installer using Podman

## Environment

The first thing you will need to do is grab the
[`onprem-environment`](https://raw.githubusercontent.com/openshift/assisted-service/master/onprem-environment)
file. Once you have this file, and have modified it based on your needs -- if
you need the Assisted Installer UI to run on a different port to avoid conflict
with `httpd` as an example -- you should source it.

```
source onprem-environment
```

**NOTE**
The remainder of this document relies on the values stored in
`onprem-environment` being set in the shell.

## NGINX Configuration

Once you have your environment setup, you will need to grab the
[`nginx.conf`](https://raw.githubusercontent.com/openshift/assisted-service/master/deploy/ui/nginx.conf)
used to configure the Assisted Installer's UI. With the `nginx.conf` file in
your current working directory, you will want to update it to reflect any
changes you made to `onprem-environment` previously.

```
# Configure the port used to access the Assisted Installer UI
sed -i "s|listen.*;|listen $UI_PORT;|" nginx.conf

# Configure the port used by the Assisted Installer to acces the inventory
sed -i "s|proxy_pass.*;|proxy_pass $SERVICE_BASE_URL;|" nginx.conf
```

**NOTE**
The `SERVICE_BASE_URL` is the `ip:port` where the assisted-service API is being
served. The Assisted Installer's agent uses the `SERVICE_BASE_URL` to talk back
to the API.

## Create the Assisted Installer Pod

Once you have made any adjustments to ports as necessary, you can create the
assisted-installer pod.

```
podman pod create --name assisted-installer -p ${DB_PORT},${SERVICE_API_PORT},${UI_PORT}
```

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
  -v ${PWD}/livecd.iso:/data/livecd.iso:z \
  -v ${PWD}/coreos-installer:/data/coreos-installer:z \
  --restart always \
  quay.io/ocpmetal/assisted-service:latest /assisted-service --port ${SERVICE_API_PORT}
```

**NOTE**
* `onprem-environment` is the file downloaded and modified previously
* `$(PWD)/livecd.iso` should be updated to reflect the RHCOS Live ISO previously
    downloaded.
* `$(PWD)/coreos-installer` is referencing the coreos-installer binary
    previously downloaded.

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
