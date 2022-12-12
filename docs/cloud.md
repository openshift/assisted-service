# Assisted Installer hosted in console.redhat.com

Users with a Red Hat account in console.redhat.com are able to use the Assisted Installer to install OCP clusters on their Bare Metals nodes.

# Using Assisted Installer via UI

The UI is available here: https://console.redhat.com/openshift/assisted-installer/clusters/

# Using Assisted Installer via API

The API is available here: https://api.openshift.com/api/assisted-install/v2/

## Authentication

On api.redhat.com, Assisted Service APIs calls are authenticated.

There are two kinds of authentication: User and Agent.
Some APIs accept both types. See configuration in [API definition](../swagger.yaml).

Agent authentication is used by the processes running on the nodes (Agent, Installer & Controller).
The Agent APIs are not meant to be used by end users.

## User Authentication

User Authentication is using JWT tokens. The JWT token is valid for 15 minutes and needs to be provided in the header of the HTTP request.

First you must obtain your offline token from https://console.redhat.com/openshift/token (This token does not expire) and set it as `OFFLINE_TOKEN`:

```bash
OFFLINE_TOKEN=<your offline token>
```

Then there are two ways to get the JWT token:

### ocm-cli client

```bash
sudo dnf copr enable ocm/tools
sudo dnf install ocm-cli
```

Login to ocm:

```bash
ocm login --token (with token from https://console.redhat.com/openshift/token)
```

Generate JWT token :

```bash
JWT_TOKEN=$(ocm token)
```

### HTTP request

```bash
JWT_TOKEN=$(curl https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token -d client_id=cloud-services -d grant_type=refresh_token -d refresh_token=${OFFLINE_TOKEN} | jq -r '.access_token')
```

### API Usage example

Here is an example of a request to get all the user's clusters:

```bash
curl https://api.openshift.com/api/assisted-install/v2/clusters -H "Authorization: Bearer ${JWT_TOKEN}"
```

## Agent Authentication

Agent authentication uses a token from the Pull Secret and needs to be provided in the header of the HTTP request.

Here is an example:

```bash
PULL_SECRET_FILE=$HOME/.docker/config.json
PULL_SECRET_TOKEN=$(jq -r '.auths."cloud.openshift.com".auth' $PULL_SECRET_FILE)
curl -X POST https://api.openshift.com/api/assisted-install/v2/clusters/f74fe2e3-1d99-4383-b2f3-8213af03ddeb/hosts -H "X-Secret-Key: <PULL_SECRET_TOKEN>"
```

# UUIDs

API object instances are defined with an unique identifier (UUID).

In the UI, the cluster UUID is available as part of the URL, for example: `https://console.redhat.com/openshift/assisted-installer/clusters/f74fe2e3-1d99-4383-b2f3-8213af03ddeb`

The host UUID is available in the UI in the 'Host Details' section.
