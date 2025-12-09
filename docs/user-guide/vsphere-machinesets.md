# Using MachineSets on an Assisted Installer vSphere Cluster

This guide shows how to take an **OpenShift cluster installed with Assisted Installer on vSphere** and add **MachineSet-managed worker nodes** (including optional autoscaling).

Assumptions:

- Cluster was installed via **Assisted Installer (console.redhat.com)**.
- Platform is **vSphere**.
- You have **cluster-admin** access (`kubeconfig`) and access to **vCenter**.

Tested on OpenShift version ≈ **4.20** (commands are similar for nearby releases).

---

## 1. Install the cluster with Assisted Installer (vSphere platform)

When [installing the cluster](https://docs.redhat.com/en/documentation/assisted_installer_for_openshift_container_platform/2025/html/installing_openshift_container_platform_with_the_assisted_installer/installing-on-vsphere) using Assisted Installer and selecting the vSphere platform, **MachineSets / Machines** are not created by Assisted Installer. You must create them manually.

```bash
oc get machinesets -n openshift-machine-api
# No resources found

oc get machines -n openshift-machine-api
# No resources found
```

---

## 2. Complete vSphere post-installation configuration

**Before creating MachineSets**, you must complete the vSphere platform integration post-installation steps. These steps configure the cloud provider integration, which is essential for the Machine API to provision VMs automatically.

The post-installation configuration updates:

- **vSphere credentials** - vCenter username and password stored in secrets:
  - `vsphere-creds` (namespace: `kube-system`) - Used by vSphere cloud provider and CSI driver
  - `vsphere-cloud-credentials` (namespace: `openshift-machine-api`) - Used by Machine API to create VMs
- **Cloud provider config** - vCenter address, datacenter, datastore, and folder paths
- **Infrastructure object** - Failure domains, compute cluster, resource pool, and network topology

**Choose one method to complete the configuration:**

1. **[CLI method](https://docs.redhat.com/en/documentation/assisted_installer_for_openshift_container_platform/2025/html/installing_openshift_container_platform_with_the_assisted_installer/installing-on-vsphere#vsphere-post-installation-configuration_installing-on-vsphere)** - Manual YAML editing and `oc` commands
2. **[Web console method](https://docs.redhat.com/en/documentation/assisted_installer_for_openshift_container_platform/2025/html/installing_openshift_container_platform_with_the_assisted_installer/installing-on-vsphere#vsphere-post-installation-configuration-console_installing-on-vsphere)** - Guided wizard in OpenShift web console (recommended for most users)

> **Important:** Without completing these steps, MachineSets cannot provision VMs on vSphere. The configuration takes approximately 1 hour as operators restart and nodes reboot. This process will restart multiple core operators and may reboot nodes-this is expected behavior.

**Verification:** After completing the configuration, ensure all operators show "All succeeded" status in the OpenShift web console under **Home → Overview → Status → Operators**.


## 3. Create a clean RHCOS template and convert it to a vSphere template

> **Important:** Do **not** use a template cloned from an existing worker node.  
> It will already have run Ignition and will not join the cluster correctly.

### 3.1 Download the RHCOS VMware OVA

The latest RHCOS version can be found at https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/latest/.

Look for the latest VMware OVA file:

```
rhcos-<version>-x86_64-vmware.x86_64.ova
```

> **Important:** The RHCOS OVA version must match your OpenShift cluster minor version exactly. For example:
> - OpenShift 4.20.z → use RHCOS 4.20.z OVA
> - OpenShift 4.19.z → use RHCOS 4.19.z OVA
>
> Cross-version combinations are not supported and may cause node registration failures.

#### What is an OVA and why do we need it?
OVA (Open Virtual Appliance) is a standardized packaging format used by VMware and other virtualization platforms.
An .ova file is essentially:
 - A prebuilt virtual machine image
 - Packaged with a descriptor (.ovf)
 - Containing clean, uninitialized OS disks
 - Designed to be imported into VMware vSphere

For RHCOS specifically:

 - The RHCOS OVA contains a fresh, never-booted RHCOS image
 - Ignition will run on first boot, as required for OpenShift Machine API
 - It has no kubelet state, no machine-id, no certificates, making it safe to clone repeatedly

This is why using the official RHCOS OVA is mandatory. A VM cloned from an already-running worker will not work because it is no longer "Ignition-first-boot clean."


### 3.2 Deploy OVF/OVA

1. In vSphere UI → Right-click folder → **Deploy OVF Template…**
2. Either paste the HTTPS URL from mirror.openshift.com or upload the OVA file you downloaded
3. Choose compute resource, datastore, folder, network (any)
4. **Important:** During deployment, ensure the **Guest OS family** is set to:
   - **Guest OS family**: Linux
   - **Guest OS version**: Red Hat Enterprise Linux 9 (64-bit)

   > VMware may default to "Other Linux" when importing the OVA. Setting this correctly ensures proper VMware Tools integration and IP address detection.

5. Complete the deployment

### 3.3 Do *not* power on → Convert to template

**Important:** Do NOT power on the VM before converting it to a template.

Right‑click the VM → **Template → Convert to Template**

---

## 4. Gather infrastructure data

You need to collect specific vSphere configuration values from your cluster to populate the MachineSet YAML. These values are stored in the `Infrastructure` object.

### 4.1 View the complete Infrastructure object

```bash
oc get infrastructure cluster -o yaml
```

### 4.2 Extract specific values

Use these commands to get the exact values you'll need for the MachineSet:

```bash
# Infrastructure ID (for naming the MachineSet)
oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}'
# Example output: my-test-cluster-d7-jzsct

# vCenter server address
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.vcenters[0].server}'
# Example output: vcenter-1.example.devcluster.openshift.com

# Datacenter name
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.datacenter}'
# Example output: EXAMPLEdatacenter-1

# Compute cluster path
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.computeCluster}'
# Example output: /EXAMPLEdatacenter-1/host/EXAMPLEcluster-1

# Datastore path
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.datastore}'
# Example output: /EXAMPLEdatacenter-1/datastore/vsanDatastore

# VM folder path
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.folder}'
# Example output: /EXAMPLEdatacenter-1/vm/assisted-clusters/test-cluster-a1b2c3d4

# Resource pool path
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.resourcePool}'
# Example output: /EXAMPLEdatacenter-1/host/EXAMPLEcluster-1/Resources

# Network/portgroup name
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.networks[0]}'
# Example output: VM Network

# RHCOS template path (if available)
oc get infrastructure cluster -o jsonpath='{.spec.platformSpec.vsphere.failureDomains[0].topology.template}'
# Example output: /EXAMPLEdatacenter-1/vm/assisted-clusters/test-cluster-a1b2c3d4/rhcos-4.20.0
```

> **Note:**
> - If the template path is not shown in the Infrastructure object, you'll need to find your RHCOS template in the vSphere UI. Right-click the template → **Copy Inventory Path** to get the full path.
> - The folder structure in the path may differ based on where you created the template. Use the actual inventory path from vSphere-the example shows a common structure but yours may vary.

---

## 5. Create a MachineSet

### 5.1 Example vSphere MachineSet YAML

The following is a generic example. Replace the placeholder values (in ALL_CAPS or `<angle brackets>`) with values from your environment:

```yaml
apiVersion: machine.openshift.io/v1beta1
kind: MachineSet
metadata:
  # Typically: <INFRA_ID>-worker
  name: <INFRA_ID>-worker
  namespace: openshift-machine-api
  labels:
    machine.openshift.io/cluster-api-cluster: <INFRA_ID>
spec:
  # Start with 1; you can scale or autoscale later
  replicas: 1
  selector:
    matchLabels:
      machine.openshift.io/cluster-api-cluster: <INFRA_ID>
      machine.openshift.io/cluster-api-machineset: <INFRA_ID>-worker
  template:
    metadata:
      labels:
        machine.openshift.io/cluster-api-cluster: <INFRA_ID>
        machine.openshift.io/cluster-api-machine-role: worker
        machine.openshift.io/cluster-api-machine-type: worker
        machine.openshift.io/cluster-api-machineset: <INFRA_ID>-worker
        node-role.kubernetes.io/worker: ""
    spec:
      providerSpec:
        value:
          apiVersion: vsphereprovider.openshift.io/v1beta1
          kind: VSphereMachineProviderSpec

          # vSphere credentials & worker ignition user-data
          credentialsSecret:
            # Usually created by the vSphere integration wizard
            name: vsphere-cloud-credentials
          userDataSecret:
            # Contains worker Ignition for the cluster
            name: worker-user-data

          # VM sizing (adjust as needed)
          numCPUs: 4
          memoryMiB: 16384        # 16 GiB
          diskGiB: 120
          numCoresPerSocket: 1

          # Networking – portgroup / network name in vSphere
          network:
            devices:
              - networkName: "<PORTGROUP_NAME>"

          # Path to the clean RHCOS template created from the OVA
          # Example: /DATA_CENTER/vm/path/to/template/rhcos-4.20.0
          template: "/DATA_CENTER/vm/path/to/template/rhcos-<RHCOS_VERSION>"

          # Where to place the cloned VMs in vSphere
          workspace:
            server: "VCENTER.EXAMPLE.COM"
            datacenter: "DATA_CENTER"
            datastore: "DATASTORE_NAME"   # datastore name (not full inventory path)
            folder: "/DATA_CENTER/vm/path/to/cluster/folder"
            resourcePool: "/DATA_CENTER/host/CLUSTER_NAME/Resources"

          # Optional toggles
          powerOn: true
          cpuHotAddEnabled: true
          memoryHotAddEnabled: true
```

### 5.2 Placeholder Reference

The MachineSet YAML uses several placeholders that must be replaced with values from your environment. See **Section 4.2** for commands to extract most of these values from the `Infrastructure` object.

| Placeholder | Description |
|------------|-------------|
| **`<INFRA_ID>`** | Cluster infrastructure ID (used for naming resources) |
| **`<PORTGROUP_NAME>`** | vSphere network/portgroup name (e.g., `VM Network`) |
| **`<RHCOS_VERSION>`** | RHCOS version in your template name (e.g., `4.20.0`) |
| **`DATA_CENTER`** | vSphere datacenter name |
| **`DATASTORE_NAME`** | Datastore short name (not full path) |
| **`VCENTER.EXAMPLE.COM`** | vCenter server FQDN |
| **`/DATA_CENTER/vm/path/to/template/...`** | Full inventory path to RHCOS template (right-click template in vSphere → Copy Inventory Path) |
| **`/DATA_CENTER/host/CLUSTER_NAME/Resources`** | Resource pool path where VMs will run |
| **`/DATA_CENTER/vm/path/to/cluster/folder`** | Folder where cloned VMs will be placed |
| **`<CLUSTER_NAME>`** | vSphere cluster name containing ESXi hosts |

---

Apply:

```bash
oc apply -f machineset-vsphere-worker.yaml
```

---

## 6. Wait for node to join and approve CSRs

After applying the MachineSet, the Machine API will clone a new VM from the RHCOS template and boot it. The node will then request to join the cluster.

### 6.1 Monitor the provisioning process

Watch the Machine object as it transitions through provisioning phases:

```bash
# Watch Machine objects (should show Provisioning → Provisioned → Running)
oc get machines -n openshift-machine-api -w

# Expected output:
# NAME                               PHASE          TYPE   REGION   ZONE   AGE
# my-test-cluster-d7-jzsct-worker-0   Provisioning   ...    ...      ...    10s
# my-test-cluster-d7-jzsct-worker-0   Provisioned    ...    ...      ...    2m
# my-test-cluster-d7-jzsct-worker-0   Running        ...    ...      ...    5m
```

### 6.2 Approve Certificate Signing Requests (CSRs)

When the new node boots and contacts the cluster, it generates Certificate Signing Requests (CSRs).
On most OpenShift 4.x clusters, **worker CSRs are auto-approved by default**, so you usually don't need to do anything here. This step is only required if CSRs get stuck in `Pending` state.

**Check for pending CSRs:**

```bash
oc get csr
```

You should see CSRs in `Pending` status:

```
NAME        AGE   SIGNERNAME                                    REQUESTOR                                         CONDITION
csr-xxxxx   1m    kubernetes.io/kube-apiserver-client-kubelet   system:serviceaccount:openshift-machine-config-operator:node-bootstrapper   Pending
```

**Approve pending CSRs:**

```bash
# Approve all pending CSRs
oc get csr -o name | xargs oc adm certificate approve
```

> **Note:** Typically, each new node generates **two CSRs**:
> 1. First CSR for the kubelet client certificate (approve this first)
> 2. Second CSR for the kubelet serving certificate (appears ~1 minute later, approve this too)

**Verify the node joins the cluster:**

```bash
# Watch nodes (the new worker should appear and become Ready)
oc get nodes -w

# Expected output:
# NAME                                        STATUS   ROLES    AGE   VERSION
# my-test-cluster-d7-jzsct-master-0        Ready    master   1d    v1.29.x
# my-test-cluster-d7-jzsct-master-1        Ready    master   1d    v1.29.x
# my-test-cluster-d7-jzsct-master-2        Ready    master   1d    v1.29.x
# my-test-cluster-d7-jzsct-worker-0        Ready    worker   5m    v1.29.x  ← New node
```

### 6.3 Troubleshooting

#### Machine stuck in "Provisioning" phase

**Check Machine API operator logs:**

```bash
# View machine-api-controllers logs
oc logs -n openshift-machine-api deployment/machine-api-controllers -c machine-controller

# Look for errors like:
# - "failed to create VM: permission denied"
# - "datastore not found"
# - "template not found"
```

**Common causes:**
- Incorrect vSphere credentials in `vsphere-cloud-credentials` secret (namespace: `openshift-machine-api`)
- Template path is wrong or template doesn't exist
- Datastore, folder, or resource pool paths are incorrect
- Insufficient permissions in vCenter

**Check the vSphere UI:**
- Does a new VM appear in the folder?
- Is the VM powered on?
- Check VM events for errors

#### Machine provisioned but node doesn't join

**Check if CSRs are pending:**

```bash
oc get csr | grep Pending
```

If CSRs are pending for more than 5 minutes, approve them manually (see Section 6.2).

**Check the Machine object status:**

```bash
oc describe machine -n openshift-machine-api <machine-name>

# Look for events and status conditions at the bottom
```

**Verify VM networking in vSphere:**
- Is the VM on the correct network/portgroup?
- Does the VM have a valid IP address (check VM console)?
- Can the VM reach the API server? (check VM console: `curl -k https://api.<cluster-domain>:6443`)

#### Node stuck in "NotReady" status

**Check node status:**

```bash
oc describe node <node-name>

# Look for conditions and events
```

**Common causes:**
- Network plugin not initialized (check `oc get pods -n openshift-multus`)
- Node hasn't finished configuring (wait 5-10 minutes)
- Ignition failed to run properly

**Check Machine Config Daemon logs on the node:**

```bash
oc debug node/<node-name>
chroot /host
journalctl -u kubelet
```

#### VM created but ignition doesn't run

**Symptoms:** VM boots but never contacts the cluster, no CSRs generated

**Causes:**
- Template was not created from clean RHCOS OVA (see Section 3)
- Template was cloned from an existing node (not supported)
- `userDataSecret` (worker-user-data) is missing or incorrect

**Solution:** Delete the Machine, recreate the RHCOS template from the official OVA, and try again.

#### Permission errors in vCenter

**Error:** `permission denied` or `insufficient permissions`

**Required vCenter permissions:**
- Virtual Machine > Configuration > All
- Virtual Machine > Interaction > All
- Virtual Machine > Inventory > All
- Resource > Assign virtual machine to resource pool
- Datastore > Allocate space
- Network > Assign network

**Verify credentials:**

```bash
# Check the vsphere-cloud-credentials secret (used by Machine API)
oc get secret vsphere-cloud-credentials -n openshift-machine-api -o yaml
```

---

## 7. Scale or enable autoscaling

Once your MachineSet is working and nodes are joining successfully, you can scale manually or enable automatic scaling based on cluster load.

### 7.1 Manual scaling

Scale the number of worker nodes manually:

```bash
# Scale to a specific number of replicas
oc scale machineset <machineset-name> -n openshift-machine-api --replicas=3

# Example:
oc scale machineset my-test-cluster-d7-jzsct-worker -n openshift-machine-api --replicas=3
```

### 7.2 Automatic scaling (autoscaling)

OpenShift can automatically scale your cluster based on resource utilization using two resources:

1. **ClusterAutoscaler** - Cluster-wide configuration (one per cluster)
2. **MachineAutoscaler** - Per-MachineSet configuration (one per MachineSet)

#### 7.2.1 Create ClusterAutoscaler

The `ClusterAutoscaler` defines cluster-wide autoscaling behavior and resource limits.

**Create a ClusterAutoscaler:**

```yaml
apiVersion: autoscaling.openshift.io/v1
kind: ClusterAutoscaler
metadata:
  name: default
spec:
  # Pod priority threshold - pods below this priority won't trigger scale-up
  podPriorityThreshold: -10

  # Resource limits for the entire cluster
  resourceLimits:
    maxNodesTotal: 24
    cores:
      min: 8
      max: 128
    memory:
      min: 32
      max: 512

  # Scale down configuration
  scaleDown:
    enabled: true
    delayAfterAdd: 10m
    delayAfterDelete: 10m
    delayAfterFailure: 3m
    unneededTime: 10m
    utilizationThreshold: "0.5"

  # Scale up configuration
  scaleUp:
    newPodScaleUpDelay: 0s
```

**Key configuration options:**

| Field | Description |
|-------|-------------|
| `maxNodesTotal` | Maximum total nodes across all MachineSets (default: no limit) |
| `resourceLimits.cores` | Min/max total CPU cores in the cluster |
| `resourceLimits.memory` | Min/max total memory (GiB) in the cluster |
| `scaleDown.enabled` | Enable automatic scale-down when nodes are underutilized |
| `scaleDown.unneededTime` | How long a node must be underutilized before removal (default: 10m) |
| `scaleDown.utilizationThreshold` | Node utilization below this triggers scale-down (default: 0.5 = 50%) |
| `podPriorityThreshold` | Only pods with priority >= this value trigger scale-up |

**Apply the ClusterAutoscaler:**

```bash
oc apply -f cluster-autoscaler.yaml
```

#### 7.2.2 Create MachineAutoscaler

The `MachineAutoscaler` defines autoscaling limits for a specific MachineSet.

**Create a MachineAutoscaler for your worker MachineSet:**

```yaml
apiVersion: autoscaling.openshift.io/v1beta1
kind: MachineAutoscaler
metadata:
  name: my-test-cluster-d7-jzsct-worker
  namespace: openshift-machine-api
spec:
  # Minimum replicas (nodes will not scale below this)
  minReplicas: 2

  # Maximum replicas (nodes will not scale above this)
  maxReplicas: 10

  # Reference to the MachineSet to autoscale
  scaleTargetRef:
    apiVersion: machine.openshift.io/v1beta1
    kind: MachineSet
    name: my-test-cluster-d7-jzsct-worker
```

**Key configuration options:**

| Field | Description |
|-------|-------------|
| `metadata.name` | Name of the MachineAutoscaler (typically matches MachineSet name) |
| `metadata.namespace` | Must be `openshift-machine-api` |
| `minReplicas` | Minimum number of nodes (cluster will not scale below this) |
| `maxReplicas` | Maximum number of nodes (cluster will not scale above this) |
| `scaleTargetRef.name` | Name of the MachineSet to autoscale |

**Apply the MachineAutoscaler:**

```bash
oc apply -f machine-autoscaler.yaml
```

### 7.3 Verify autoscaling configuration

**Check ClusterAutoscaler status:**

```bash
oc get clusterautoscaler
oc describe clusterautoscaler default
```

**Check MachineAutoscaler status:**

```bash
oc get machineautoscaler -n openshift-machine-api
oc describe machineautoscaler <name> -n openshift-machine-api
```

**Watch autoscaling in action:**

```bash
# First, identify the cluster-autoscaler deployment (usually cluster-autoscaler-default)
oc get deployment -n openshift-machine-api | grep cluster-autoscaler

# Monitor cluster-autoscaler logs for scaling decisions
oc logs -n openshift-machine-api deployment/cluster-autoscaler-default -f

# Alternatively, watch the operator logs
oc logs -n openshift-machine-api deployment/cluster-autoscaler-operator -f

# Watch Machines scale
oc get machines -n openshift-machine-api -w

# Watch nodes join/leave
oc get nodes -w
```

### 7.4 Testing autoscaling

To test scale-up, create a workload that requests more resources than available:

```bash
# Create a deployment with resource requests that exceed current capacity
oc create deployment autoscale-test --image=registry.access.redhat.com/ubi8/ubi:latest -- sleep infinity
oc set resources deployment autoscale-test --requests=cpu=1,memory=2Gi
oc scale deployment autoscale-test --replicas=50
```

Watch the cluster autoscaler provision new nodes to accommodate the pods:

```bash
oc get machines -n openshift-machine-api -w
```

To test scale-down, delete the test deployment and wait for the autoscaler to remove underutilized nodes:

```bash
oc delete deployment autoscale-test

# Watch nodes scale down after unneededTime elapses (default: 10 minutes)
oc get nodes -w
```

### 7.5 Additional resources

For complete autoscaling documentation, see:

- [ClusterAutoscaler API Reference](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/autoscale_apis/clusterautoscaler-autoscaling-openshift-io-v1)
- [MachineAutoscaler API Reference](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/autoscale_apis/machineautoscaler-autoscaling-openshift-io-v1beta1)
- [OpenShift Machine Management Documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/machine_management/index)

---

## 8. Verification & Testing

After creating the MachineSet and ensuring it references a clean RHCOS OVA template, use the following steps to verify everything is working correctly.

### 8.1 Verify MachineSet status

Check that the MachineSet is recognized and shows the desired replica count:

```bash
oc get machinesets -n openshift-machine-api
```

**Expected output example:**

```
NAME                               DESIRED   CURRENT   READY   AVAILABLE   AGE
my-test-cluster-d7-jzsct-worker   1         1         1       1           5m
```

**Status columns:**

- **DESIRED** - What you configured in `spec.replicas`
- **CURRENT** - How many Machines the Machine API created
- **READY** - How many Machines have successfully joined the cluster
- **AVAILABLE** - How many Machines are ready and available

All columns should match when the MachineSet is healthy.

### 8.2 Verify Machine creation and lifecycle

Watch the Machine object as it progresses through provisioning phases:

```bash
oc get machines -n openshift-machine-api -w
```

**Expected progression:**

1. **Provisioning** - VM is being cloned from template
2. **Provisioned** - VM created and powered on
3. **Running** - Node has joined the cluster
4. Machine gains a `NodeRef` (confirms it successfully joined)

**Inspect the Machine details:**

```bash
oc describe machine -n openshift-machine-api <machine-name>
```

**Look for successful status:**

```
Status:
  Phase: Running
  Node Ref:
    Kind: Node
    Name: my-test-cluster-d7-jzsct-worker-xxxxx
    UID: ...
```

The presence of `Node Ref` confirms the Machine successfully registered a node in the cluster.

### 8.3 Verify CSRs and node joins cluster

After the Machine is provisioned, verify that CSRs are approved and the node successfully joins the cluster.

See **Section 6** for detailed steps on:
- Monitoring the provisioning process (Section 6.1)
- Approving Certificate Signing Requests (Section 6.2)
- Verifying nodes join and reach `Ready` status (Section 6.2)
- Troubleshooting common issues (Section 6.3)

**Quick verification:**

```bash
# Check CSRs are approved
oc get csr

# Check node is Ready
oc get nodes
```

---

## 9. Test MachineSet scaling

After verifying your MachineSet works, test scaling to ensure it behaves correctly. **Scaling MachineSets on clusters installed with Assisted Installer works the same as any other OpenShift cluster.**

See **Section 7** for complete scaling documentation, including:
- Manual scaling (Section 7.1)
- Autoscaling configuration (Section 7.2)
- Testing autoscaling (Section 7.4)

For general OpenShift scaling documentation, see:
- [Machine Management - Manually scaling a MachineSet](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/machine_management/index)
- [Applying autoscaling to an OpenShift Container Platform cluster](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/machine_management/applying-autoscaling)

---

## 10. Success checklist

Use this checklist to confirm your MachineSet configuration is working correctly:

| Step | Success Criteria | Verification Command |
|------|------------------|----------------------|
| **MachineSet Created** | MachineSet shows `DESIRED = CURRENT = READY` | `oc get machinesets -n openshift-machine-api` |
| **Machine Created** | Machine reaches `Running` phase with `NodeRef` | `oc describe machine -n openshift-machine-api <name>` |
| **CSRs Approved** | All CSRs are `Approved,Issued` | `oc get csr` |
| **Node Joined** | Node appears as `Ready` | `oc get nodes` |
| **Manual Scale-Up** | Additional workers created successfully | `oc scale machineset ... --replicas=N` |
| **Manual Scale-Down** | Workers removed and VMs deleted from vSphere | `oc scale machineset ... --replicas=N` |
| **Autoscaling (optional)** | Workers added automatically on high load | Create test deployment with high requests |
| **Template Correct** | VMs boot cleanly with Ignition, no CSR delays | Check VM console, CSR approval time |

### Optional: Verify MachineHealthCheck

MachineHealthCheck automatically replaces unhealthy nodes. While not required, it's recommended for production clusters.

**Check if MachineHealthCheck is configured:**

```bash
oc get machinehealthcheck -A
```

If no MachineHealthChecks exist and you want automatic node remediation, see the [OpenShift documentation on MachineHealthCheck](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/machine_management/index).

---

## 11. Common verification issues

### MachineSet shows DESIRED but CURRENT is 0

**Cause:** Machine API cannot create VMs in vSphere

**Check:**
- Machine API controller logs: `oc logs -n openshift-machine-api deployment/machine-api-controllers -c machine-controller`
- Look for errors about template, credentials (in `vsphere-cloud-credentials` secret, namespace: `openshift-machine-api`), or permissions

### Machine stuck in "Provisioning" for >5 minutes

**Cause:** VM creation is failing in vSphere

**Check:**
- vSphere UI: Is a VM being created?
- vSphere UI: Check VM events for errors
- Template path is correct and template exists
- Credentials in `vsphere-cloud-credentials` secret (namespace: `openshift-machine-api`) are valid

### Node appears but stays "NotReady"

**Cause:** Node is booted but not fully configured

**Check:**
- Wait 5-10 minutes (normal configuration time)
- Check node conditions: `oc describe node <name>`
- Check network plugin: `oc get pods -n openshift-multus`

### CSRs never appear

**Cause:** VM is not contacting the API server

**Check:**
- VM console in vSphere: Is Ignition running?
- VM has correct network configuration and IP
- VM can reach API server: `curl -k https://api.<cluster-domain>:6443`
- Template was created from clean RHCOS OVA (not cloned from existing worker)

---
