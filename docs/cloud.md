# Assisted Installer hosted in cloud.redhat.com

Users with a Red Hat account in cloud.redhat.com are able to use the Assisted Installer to install OCP clusters on their Bare Metals nodes.

## Using Assisted Installer via UI

The UI is available here: https://cloud.redhat.com/openshift/assisted-installer/clusters/

## Using Assisted Installer via API

The API is available here: https://api.openshift.com/api/assisted-install/v1/

### Authentication

On cloud.redhat.com, Assisted Service APIs calls are authenticated.

There are two kind of authentications: User and Agent.
Some APIs accept both types. See configuration in [API definition](../swagger.yaml).

Agent authentication is used by the processes running on the nodes (Agent, Installer & Controller).
The Agent APIs are not meant to be used by end users.

#### User Authentication

User Authentication is using JWT tokens.

In order to get a JWT token, `ocm` CLI is needed:

```
sudo dnf copr enable ocm/tools
sudo dnf install ocm-cli
```

Obtain offline token from https://cloud.redhat.com/openshift/token (This token does not expire)

Login to ocm:
```
ocm login --token (with token from https://cloud.redhat.com/openshift/token)
```

Generate JWT token :
```
ocm token
```

The JWT token is valid for 15 minutes and need to be provided in the header of the HTTP request.

Here an example to get all the user's clusters:

```
curl https://api.openshift.com/api/assisted-install/v1/clusters -H "Authorization: Bearer $(ocm token)"
```

#### Agent Authentication

Agent authentication uses a token from the Pull Secret and needs to be provided in the header of the HTTP request.

Here an example:

```
 curl -X POST https://api.openshift.com/api/assisted-install/v1/clusters/f74fe2e3-1d99-4383-b2f3-8213af03ddeb/hosts -H "X-Secret-Key: <PULL_SECRET_TOKEN>"
 ```

 ### UUIDs

 API objects instances are defined with an unique identifier (UUID).

 In the UI, the cluster UUID is available as part of the URL, for example: `https://cloud.redhat.com/openshift/assisted-installer/clusters/f74fe2e3-1d99-4383-b2f3-8213af03ddeb`

 The host UUID is available in the UI in the 'Host Details' section.
