---
title: backup-restore-support
authors:
  - "@danielerez"
creation-date: 2024-08-08
last-updated: 2024-08-08
---

# Backup/Restore Support

## Summary

The Assisted Installer should support backup/restore and disaster recovery scenarios, either using OADP (OpenShift API for Data Protection) for ACM (Advanced Cluster Management), or, using Kube-API.
I.e. the assisted-service should be resilient in such scenarios which, for this context and effort, means that restored/moved spoke clusters should keep the same state and behave the same on the new hub cluster. 

## Motivation

Provide resiliency in the assisted-service for safe backup/restore flows, allowing spoke clusters to be used without any restriction after DR scenarios or moving between hubs.

### Goals

Spoke clusters (with/without BMHs) should have a consistent state and function properly, including the ability to add/remove nodes, after these scenarios: 
* Spoke clusters restored on a passive hub with ACM/OADP functionality.
* Spoke clusters moved between hubs using Kube-API.

### Non-Goals

* Improve import day2 cluster solution by getting full host data (e.g. using an agent on existing nodes).

## Proposal

### User Stories

#### Story 1

As an Infrastructure Admin, I want to backup an installed spoke cluster from an active hub, including the associated BMHs (e.g. by using ACM/Kube-API/GitOps/etc).
Then, I want to safely restore the cluster on a passive hub, including the BMHs that should keep the same state. I.e. the restored cluster should behave exactly as in the active hub with no restrictions (support day2 operations, etc).
Note: if unbound hosts are included, they should still be automatically reprovisioned using a new discovery ISO.

#### Story 2

As an Infrastructure Admin, I want to backup an installed spoke cluster that doesn't have associated BMHs. Then, I want to safely restore the cluster on a passive hub.
Note: if unbound hosts are included, they should be manually booted using a new discovery ISO. 

#### Story 3

As an Infrastructure Admin, I want to restore a spoke cluster in non-installed status (draft/in-progress/etc). The cluster on target hub should, ideally, keep the same status. Or, at least, fail gracefully and let the user retry the installation.
Unbound hosts should be treated as previous stories (depends on BMHs).

#### Story 4

As an Infrastructure Admin, I want to safely move installed spoke clusters between hubs.
Concerns are similar to previous stories.

### Implementation Details/Notes/Constraints

#### Solution for BMHs reconcile (due to a missing status)

According to BMO documentation, the suggested approach for moving BareMetalHost from one cluster to another is using the `baremetalhost.metal3.io/status` [annotation](https://github.com/metal3-io/metal3-docs/blob/6a656b3eb195c1b09ba35fcad4d011c6cb02dbc2/docs/user-guide/src/bmo/status_annotation.md).
I.e. Extract the current status of the BMH, store it in an annotation, create the BMH on the new hub:
```
# Save the status in json format to a file
kubectl get bmh <name-of-bmh> -o jsonpath="{.status}" > status.json
# Save the BMH and apply the status annotation to the saved BMH.
kubectl -n metal3 annotate bmh <name-of-bmh> \
  baremetalhost.metal3.io/status="$(cat status.json)" \
  --dry-run=client -o yaml > bmh.yaml
# Apply the saved BMH on the target hub
kubectl --kubeconfig <target-hub-kubeconfig> apply -f bmh.yaml
```
This flow ensures that the BMH on the new hub has status (BMO applies the status using the annotation). Hence, as the BMH should be in state 'Provisioned', the BMH reconcile is avoided.
I.e. prevent destruction of restored spoke clusters.

##### Suggested solution for automating the flow (make it transparent to the user):

In assisted-service -> `bmh_agent_controller`:
* If a BMH is in 'Provisioned' state (and, doesn't have `status`+`paused` annotations):
  * Extract BMH's status and add `baremetalhost.metal3.io/status` to the object metadata.
  * Add also, add the `baremetalhost.metal3.io/paused` annotation:
    * This is required since once the annotation is added, BMO updates the BMH's status and delete the annotation.
    * The [paused](https://github.com/metal3-io/baremetal-operator/blob/ac05af9fb9a0b444c4441a758c731e6f518c21df/docs/api.md#pausing-reconciliation) annotation, is similar to the `detached` annotation we're already using - but, it's also pausing the reconcile loop. Thus, keeping the status annotation intact.
* If a BMH is missing 'Status' (and, has `status`+`paused` annotation):
  * To restore the original Status:
    * Remove the `baremetalhost.metal3.io/paused` annotation
      * Status is restored, but `baremetalhost.metal3.io/status` annotation is removed.
      * The `status` and `paused` annotation will be restored (see previous bullet). 

#### Solution for deleted Agent CRs

1. Don't delete the Agent CR when an associated host doesn't exits on DB.
   * Remove relevant [code](https://github.com/openshift/assisted-service/blob/5bbf696fc0cc5a220fd8a0d1e408ab0bcc889e3f/internal/controller/controllers/agent_controller.go#L143-L151) from AgentController.

2. Restore the Host record in DB according to properties from the Agent CR.
   * Seems we can infer more than the mandatory properties:
     * host.ID <-- agent.Name
     * host.Role <-- agent.Role
     * host.InfraEnvID <-- agent.metadata.annotations['infraenvs.agent-install.openshift.io']
     * host.Kind <-- 'Host' or 'AddToExistingClusterHost' if cluster is already installed
     * host.InstallerArgs <-- agent.InstallerArgs
     * host.RequestedHostname <-- agent.Hostname
   * The entire inventory exists in Agent.Status. But, need to ensure it exists also post restore. If not, we can consider adding an annotation for it (i.e something as `status` in BMH).
   * Note: some of the inventory data can also be restored from existing annotations.
     E.g.:
     * inventory.agent-install.openshift.io/cpu-architecture: x86_64
     * inventory.agent-install.openshift.io/cpu-virtenabled: "true"
     * inventory.agent-install.openshift.io/host-isvirtual: "true"
     * inventory.agent-install.openshift.io/host-manufacturer: RedHat
     * inventory.agent-install.openshift.io/host-productname: KVM
     * inventory.agent-install.openshift.io/storage-hasnonrotationaldisk: "false"
   
### Risks and Mitigations

* In general, these changes might affect flows on spoke cluster (before and after restore), need to ensure no regressions on all scenarios.
* Adding the 'paused' annotation means a stale BMH status, need to check if it's crucial.
* Keeping the Agent CR and restoring values into Host in DB could affect current flows (e.g. BindHost, UnbindHost, etc).
* Restoring Hosts will result in partial records in DB. E.g. missing Inventory - could be challenging to tackle (I.e. probably need to add annotations to Agent).

### Open Questions

* Any specific concerns for the ConvergedFlow/Non-ConvergedFlow?
* How seamlessly should we support restoring of 'in-progress' clusters (as the focus is on 'installed' clusters)?
  * Need to consider conflicts when restoring from stale a backup. E.g. a backup with an unbound agent, that has already been installed since the backup was taken.
  * We could just ensure that such clusters have some indication that an error has occurred. I.e. so the user could retry the installation.

### UI Impact

* No UI changes are required.

* The ACM UI should display the correct status for spoke clusters, etc.

### Test Plan

* All existing E2E tests should pass on restored/moved spoke clusters.
  * Specifically, should test day2 flows on clusters.
* Should be tested both with ACM/OADP Backup/Restore feature and with the ZTP flow.

## Current Alternatives

* Using ACM/OADP, the current workaround for including BMHs safely:
  * Forcing BMHs in restore by adding an annotation: `cluster.open-cluster-management.io/backup: cluster-activation`
  * Include BMHs original status during the restore (to avoid reprovisioning of the BMHs).
    I.e. add 'restoreStatus' in the Restore CR
    ```
    apiVersion: cluster.open-cluster-management.io/v1beta1
    kind: Restore
    ...
    spec:
      restoreStatus:
        includedResources:
        - BareMetalHost
    ```

* For moving a spoke cluster between hubs:
  * Current workaround is to use [import an existing cluster](../hive-integration/import-installed-cluster.md) flow.
