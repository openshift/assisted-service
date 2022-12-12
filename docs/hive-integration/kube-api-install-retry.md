# Hive Integration - Retry to Install a cluster

In a case where the cluster installation has failed, the user may want to restart the installation.
A failed install may happen for multiple reasons; make sure you understand the root cause before trying again.

To restart the installation, the user will need to:

1. Delete the cluster `AgentClusterInstall` resource.
2. Delete all the cluster `BareMetalHost` resources (a single resource in the case of SNO).
3. Recreate the `AgentClusterInstall` resource that was deleted in step 1
4. Recreate the `BareMetalHost` resources that were deleted in step 2

:warning: Note:

Recreating `AgentClusterInstall` will de-register and re-register the backend cluster, which will trigger a discovery image generation for `InfraEnv`.

Baremetal Agent Controller (a.k.a `BMAC`) is inspecting `InfraEnv` for any changes to `status.isoDownloadURL `, and will pick up the newly generated discovery image.
If you boot your machines in other methods (boot it yourself), make sure you use the new image for that.

This document will capture the changes before (failed install) and after (resources recreated) to demonstrate this change to the image, but you won't have to do that when you reattempt the installation.

## Baseline

How may a failed installation look?
For that look at the `AgentClusterInstall` conditions:

```bash
$ kubectl -n test-namespace get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.metadata.name}{'\n'}{range .status.conditions[*]}{.type}{'\t'}{.message}{'\n'}"
```

```bash
test-agent-cluster-install
SpecSynced The Spec has been successfully applied
Validated  The cluster's validations are passing
RequirementsMet    The cluster installation stopped
Completed  The installation has failed: cluster has hosts in error
Failed The installation failed: cluster has hosts in error
Stopped    The installation has stopped due to error
```

### Capture current discovery image URL

Expect URLs to match.

#### InfraEnv

```bash
$ kubectl -n test-namespace get infraenvs.agent-install.openshift.io test-infraenv -o=jsonpath="{.status.createdTime}{'\n'}{.status.isoDownloadURL}{'\n'}"
```

```bash
2021-06-23T14:24:57Z
https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/2748ddac-0ac9-489b-a38c-ce0d29d22b02/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMjc0OGRkYWMtMGFjOS00ODliLWEzOGMtY2UwZDI5ZDIyYjAyIn0.L67oWuxClinXtCiqRcieOS4vAJCFNVztAE_A2TYnBYJawhAox6NfiuxUih2TKwZxbNVCOwLdQXt_5rjYL6Xn5g
```

#### BareMetalHost

```bash
$ kubectl -n test-namespace get baremetalhosts.metal3.io ostest-extraworker-3  -o=jsonpath="{.spec.image.url}{'\n'}"
```

```bash
https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/2748ddac-0ac9-489b-a38c-ce0d29d22b02/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMjc0OGRkYWMtMGFjOS00ODliLWEzOGMtY2UwZDI5ZDIyYjAyIn0.L67oWuxClinXtCiqRcieOS4vAJCFNVztAE_A2TYnBYJawhAox6NfiuxUih2TKwZxbNVCOwLdQXt_5rjYL6Xn5g
```

## Delete and Recreate

1. As mentioned above, Delete both `AgentClusterInstall` and all the cluster `BareMetalHost` resources.
2. Create `AgentClusterInstall`
3. The `InfraEnv` controller:

   3.1. Gets notified for a successful backend cluster registration.

   3.2. Reconcile and send a request for the backend to generate a discovery image.

4. Inspect `InfraEnv` for:

   4.1. Changes to `status.isoDownloadURL`, cluster-id and token.

   4.2. Notice that `status.createdTime` was updated.

```bash
$ kubectl -n test-namespace get infraenvs.agent-install.openshift.io test-infraenv  -o=jsonpath="{.status.createdTime}{'\n'}{.status.isoDownloadURL}{'\n'}"
```

```bash
2021-06-24T10:31:16Z
https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/21ade42e-1c78-48b0-bde7-e875632527c1/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMjFhZGU0MmUtMWM3OC00OGIwLWJkZTctZTg3NTYzMjUyN2MxIn0.IbVClRJQm8nihs7N2B9hiJ523qioKKqymaxGWkQPCIdnMspx_pfWRUeieYyEVDUExLeBPuFlwb84mLPCuZCLzg
```

5. Inspect AgentClusterInstall conditions

```bash
$ kubectl -n test-namespace get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install
-o=jsonpath="{.metadata.name}{'\n'}{range .status.conditions[*]}{.type}{'\t'}{.message}{'\n'}"
```

```bash
test-agent-cluster-install
SpecSynced The Spec has been successfully applied
RequirementsMet    The cluster is not ready to begin the installation
Validated  The cluster's validations are failing: Single-node clusters must have a single master node and no workers.
Completed  The installation has not yet started
Failed The installation has not failed
Stopped    The installation is waiting to start or in progress
```

6. Create `BareMetalHost` resource(s). Note that `BMAC` will wait for the `InfraEnv` image to be at least 1 minute old.
7. Check `BareMetalHost` events:

```bash
$ kubectl -n test-namespace describe baremetalhosts.metal3.io  ostest-extraworker-3
```

```bash
<...>
Events:
Type    Reason                Age   From                         Message
  ----    ------                ----  ----                         -------
Normal  Registered            61s   metal3-baremetal-controller  Registered new host
Normal  BMCAccessValidated    50s   metal3-baremetal-controller  Verified access to BMC
Normal  InspectionSkipped     50s   metal3-baremetal-controller  disabled by annotation
Normal  ProfileSet            50s   metal3-baremetal-controller  Hardware profile set: unknown
Normal  ProvisioningStarted   49s   metal3-baremetal-controller  Image provisioning started for https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/21ade42e-1c78-48b0-bde7-e875632527c1/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMjFhZGU0MmUtMWM3OC00OGIwLWJkZTctZTg3NTYzMjUyN2MxIn0.IbVClRJQm8nihs7N2B9hiJ523qioKKqymaxGWkQPCIdnMspx_pfWRUeieYyEVDUExLeBPuFlwb84mLPCuZCLzg
Normal  ProvisioningComplete  39s   metal3-baremetal-controller  Image provisioning completed for https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/21ade42e-1c78-48b0-bde7-e875632527c1/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMjFhZGU0MmUtMWM3OC00OGIwLWJkZTctZTg3NTYzMjUyN2MxIn0.IbVClRJQm8nihs7N2B9hiJ523qioKKqymaxGWkQPCIdnMspx_pfWRUeieYyEVDUExLeBPuFlwb84mLPCuZCLzg

```

8. Check backend cluster events:

```bash
$ curl -s -k $(kubectl -n test-namespace get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.status.debugInfo.eventsURL}")  | jq "."
```

First, expect:

```json
{
  "cluster_id": "21ade42e-1c78-48b0-bde7-e875632527c1",
  "event_time": "2021-06-24T10:49:19.232Z",
  "message": "Started image download (image type is \"minimal-iso\")",
  "severity": "info"
}
```

Lastly, when installed:

```json
{
  "cluster_id": "21ade42e-1c78-48b0-bde7-e875632527c1",
  "event_time": "2021-06-24T11:34:08.644Z",
  "message": "Successfully finished installing cluster test-cluster",
  "severity": "info"
}
```
