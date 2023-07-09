# Kube API - Getting Started Guide

This document is a step-by-step guide that demonstrates how to deploy a single-node cluster end to end.

**Note**:
* This document is not meant to expand on each and every resource in detail; [other documents already do that](README.md). Instead, expect the details needed to understand the context and what is currently happening.

* The order in which actions are performed here is **optional**, and you may choose to go by a different one. The one order restriction the API does have is in regards to creating `NMstateConfigs` (which are optional) before `InfraEnv`, for more details, [check the warning mentioned here](README.md#NMStateConfig).
## What To Expect
* A walk through of custom resource definitions (CRDs) and understand how they relate to each other.

* A clear understanding of what happened, both in OpenShift / Kubernetes and assisted-service backend per each action described below.

## How To Use This guide

* Make sure that for each step you follow, you understand how and why it is done.

* You are encouraged to copy and paste the below-mentioned commands to your terminal and see things in action.

* Note the inline comments in the `yaml` examples. There is no need to remove those in case you which to copy and create the resources directly in your environment.

## Assumptions
* You have deployed both the operator and assisted-service by, for example, [using these instructions](../operator.md).  Alternatively, you may choose to deploy via OLM or as a part of ACM, which is also applicable.
* Using the discovery image:

    * **Boot it yourself**: You are able to use the generated discovery image to boot your host.

    * **Zero Touch Provisioning (ZTP)**:  Detailed in the [advanced section](#advanced-zero-touch-provisioning). Configure `BMH` / `BareMetalHost` to automate the host discovery procedure. This document uses [dev-scripts](https://github.com/openshift-metal3/dev-scripts/) for that. [read about it here](baremetal-agent-controller.md).


## Resource Types and Relationships
![kubeAPI4.9](kubeAPI4.9.jpg)


## Cluster Prerequisites

### 1. Create a namespace for your resources

```yaml
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Namespace
metadata:
  name: demo-worker4 # Use any name, but note that using 'assisted-installer' is not recommended
EOF
```

### 2. Create a Pull Secret

   * Use the secret obtained from [console.redhat.com](https://console.redhat.com/openshift/install/pull-secret)
```yaml
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Secret
type: kubernetes.io/dockerconfigjson
metadata:
  name: pull-secret
  namespace: demo-worker4
stringData:
  .dockerconfigjson: '{"auths":{"cloud.openshift.com":{"auth":"your secret here","email":"user@example.com"}}}'
EOF
```

### 3. Generate `ClusterImageSet`, to specify the OpenShift version to be used.

```yaml
cat <<EOF | kubectl create -f -
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
   name: openshift-v4.8.0
spec:
   releaseImage: quay.io/openshift-release-dev/ocp-release:4.8.0-rc.0-x86_64  # That version may change over time
EOF
```
## Creating Cluster Resources

### Define a cluster with assisted-service
For that, you'll to need create two resources:
#### 1. Create [ClusterDepoyment](README.md#clusterdeployment)

```yaml
   cat <<EOF | kubectl create -f -
   apiVersion: hive.openshift.io/v1
   kind: ClusterDeployment
   metadata:
      name: single-node
      namespace: demo-worker4
   spec:
      baseDomain: hive.example.com
      clusterInstallRef:
         group: extensions.hive.openshift.io
         kind: AgentClusterInstall
         name: test-agent-cluster-install # Use the same name for AgentClusterInstall
         version: v1beta1
      clusterName: test-cluster
      controlPlaneConfig:
         servingCertificates: {}
      platform:
         agentBareMetal:
            agentSelector:
               matchLabels:
                  bla: aaa
      pullSecretRef:
         name: pull-secret  # Use the pull secret name mentioned in Cluster Prerequisites step 2
   EOF
```
##### Result

* At this point, there is no `AgentClusterInstall`, so it won't do much and won't even register the cluster.

###### ClusterDeployment resource conditions:
```bash
kubectl -n demo-worker4 get clusterdeployments.hive.openshift.io single-node -o=jsonpath="{.metadata.name}{'\n'}{range .status.conditions[*]}{.type}{'\t'}{.message}{'\n'}"
```
```bash
single-node
RelocationFailed	no ClusterRelocates match
Hibernating	Condition Initialized
AuthenticationFailure	Platform credentials passed authentication check
```

###### assisted-service log:
```go
time="2021-06-28T20:45:07Z" level=info msg="ClusterDeployment Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:115" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=b5022e40-4a1e-4658-ba0a-5159938b7789
time="2021-06-28T20:45:07Z" level=info msg="AgentClusterInstall does not exist for ClusterDeployment single-node" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:147" AgentClusterInstall=test-agent-cluster-install cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=b5022e40-4a1e-4658-ba0a-5159938b7789
time="2021-06-28T20:45:07Z" level=info msg="ClusterDeployment Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:112" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=b5022e40-4a1e-4658-ba0a-5159938b7789
```

#### 2. Create [AgentClusterInstal](README.md#agentclusterinstall)

```yaml
cat <<EOF | kubectl create -f -
apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  name: test-agent-cluster-install
  namespace: demo-worker4
spec:
  clusterDeploymentRef:
    name: single-node # Use the clusterDeployment name from the previous step
  imageSetRef:
    name: openshift-v4.8.0 # Use the ClusterImageSet name mentioned in Cluster Prerequisites step 3
  networking:
    clusterNetwork:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
    machineNetwork:
    - cidr: 192.168.111.0/24
    serviceNetwork:
    - 172.30.0.0/16
  provisionRequirements:
    controlPlaneAgents: 1
 #sshPublicKey: ssh-rsa your-public-key-here (optional)
EOF
```

##### Result

* The cluster is fully defined in assisted-service backend (PostgreSQL DB).
* No discovery image has been created at this point.

###### AgentClusterInstall resource conditions:

```bash
kubectl -n demo-worker4 get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.metadata.name}{'\n'}{range .status.conditions[*]}{.type}{'\t'}{.message}{'\n'}"
```
```bash
test-agent-cluster-install
SpecSynced	The Spec has been successfully applied
RequirementsMet	The cluster is not ready to begin the installation
Validated	The cluster's validations are failing: Single-node clusters must have a single master node and no workers.
Completed	The installation has not yet started
Failed	The installation has not failed
Stopped	The installation is waiting to start or in progress
```

###### assisted-service log:
```go
time="2021-06-28T21:01:31Z" level=info msg="ClusterDeployment Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:115" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="Creating a new cluster single-node demo-worker4" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).createNewCluster" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:843" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="Stored OCP version: 4.8.0-rc.0" func="github.com/openshift/assisted-service/internal/versions.(*handler).AddOpenshiftVersion" file="/go/src/github.com/openshift/origin/internal/versions/versions.go:240" pkg=versions
time="2021-06-28T21:01:31Z" level=info msg="Register cluster: test-cluster with id 02a89bb9-6141-4d14-a82e-42f254217502" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).RegisterClusterInternal" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:344" cluster_id=02a89bb9-6141-4d14-a82e-42f254217502 go-id=684 pkg=Inventory request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="HA mode is None, setting UserManagedNetworking to true and VipDhcpAllocation to false" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).RegisterClusterInternal" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:387" cluster_id=02a89bb9-6141-4d14-a82e-42f254217502 go-id=684 pkg=Inventory request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="Successfully registered cluster test-cluster with id 02a89bb9-6141-4d14-a82e-42f254217502" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).RegisterClusterInternal.func1" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:351" cluster_id=02a89bb9-6141-4d14-a82e-42f254217502 go-id=684 pkg=Inventory request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="no infraEnv for the clusterDeployment single-node in namespace demo-worker4" func=github.com/openshift/assisted-service/internal/controller/controllers.getInfraEnvByClusterDeployment file="/go/src/github.com/openshift/origin/internal/controller/controllers/common.go:67" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="ClusterDeployment Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:112" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=562c4762-532a-4a2a-b99f-53fa57d0b966
time="2021-06-28T21:01:31Z" level=info msg="ClusterDeployment Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:115" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=98cac786-f3c2-4237-ae7c-f5c1a9e6e85b
time="2021-06-28T21:01:31Z" level=info msg="update cluster 02a89bb9-6141-4d14-a82e-42f254217502 with params: &{AdditionalNtpSource:<nil> APIVip:<nil> APIVipDNSName:<nil> BaseDNSDomain:<nil> ClusterNetworkCidr:<nil> ClusterNetworkHostPrefix:<nil> DisksSelectedConfig:[] HostsMachineConfigPoolNames:[] HostsNames:[] HostsRoles:[] HTTPProxy:<nil> HTTPSProxy:<nil> Hyperthreading:<nil> IngressVip:<nil> MachineNetworkCidr:0xc003c5e760 Name:<nil> NoProxy:<nil> OlmOperators:[] PullSecret:<nil> ServiceNetworkCidr:<nil> SSHPublicKey:<nil> UserManagedNetworking:<nil> VipDhcpAllocation:<nil>}" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).updateClusterInternal" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:1715" go-id=684 pkg=Inventory request_id=98cac786-f3c2-4237-ae7c-f5c1a9e6e85b
time="2021-06-28T21:01:31Z" level=info msg="no infraEnv for the clusterDeployment single-node in namespace demo-worker4" func=github.com/openshift/assisted-service/internal/controller/controllers.getInfraEnvByClusterDeployment file="/go/src/github.com/openshift/origin/internal/controller/controllers/common.go:67" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=98cac786-f3c2-4237-ae7c-f5c1a9e6e85b
time="2021-06-28T21:01:31Z" level=info msg="Updated clusterDeployment demo-worker4/single-node" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).updateIfNeeded" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:714" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=98cac786-f3c2-4237-ae7c-f5c1a9e6e85b
time="2021-06-28T21:01:31Z" level=info msg="ClusterDeployment Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:112" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=98cac786-f3c2-4237-ae7c-f5c1a9e6e85b
```

### Generate Cluster Discovery Image

#### 1. Optional - Create [NMStateConfig](README.md#NMStateConfig)
* Needed for optional host-level network configuration, such as static IP addresses and more.

```yaml
cat <<EOF | kubectl create -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: NMStateConfig
metadata:
  name: mynmstateconfig
  namespace: demo-worker4
  labels:
    demo-nmstate-label: some-value # both the label name and value must be included in InfraEnv nmStateConfigLabelSelector matchLabels
spec:
  config:
    interfaces:
      - name: eth0
        type: ethernet
        state: up
        mac-address: 02:00:00:80:12:14
        ipv4:
          enabled: true
          address:
            - ip: 192.168.111.30
              prefix-length: 24
          dhcp: false
      - name: eth1
        type: ethernet
        state: up
        mac-address: 02:00:00:80:12:15
        ipv4:
          enabled: true
          address:
            - ip: 192.168.140.30
              prefix-length: 24
          dhcp: false
    dns-resolver:
      config:
        server:
          - 192.168.126.1
    routes:
      config:
        - destination: 0.0.0.0/0
          next-hop-address: 192.168.111.1
          next-hop-interface: eth1
          table-id: 254
        - destination: 0.0.0.0/0
          next-hop-address: 192.168.140.1
          next-hop-interface: eth1
          table-id: 254
  interfaces:
    - name: "eth0"
      macAddress: "02:00:00:80:12:14"
    - name: "eth1"
      macAddress: "02:00:00:80:12:15"
EOF
```

#### 2. Create [InfraEnv](README.md#InfraEnv)
* The `InfraEnv` resource describes everything about the infrastructure environment that is needed in order to create a discovery ISO.
```yaml
cat <<EOF | kubectl create -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: myinfraenv
  namespace: demo-worker4
spec:
  clusterRef:
    name: single-node  # Use the above created clusterDeployment resource name and namespace
    namespace: demo-worker4
  pullSecretRef: # currently ignored by InfraEnv controller
    name: pull-secret
 #sshAuthorizedKey: 'your_pub_key_here' (optional) , this key will allow to connect to machines booted from the discovery ISO.
  nmStateConfigLabelSelector:
    matchLabels:
      demo-nmstate-label: some-value # both the label name and value must match the NMStateConfig label section
EOF
```

##### Result
* Discovery Image Created.
* Note that the log does specify that nmStateConfigs were included.


###### InfraEnv resource conditions:
```bash
kubectl -n demo-worker4 get infraenvs.agent-install.openshift.io myinfraenv -o=jsonpath="{.metadata.name}{'\n'}{range .status.conditions[*]}{.type}{'\t'}{.message}{'\n'}"
```
```bash
myinfraenv
ImageCreated	Image has been created
```

###### custom resource isoDownloadURL:
```bash
kubectl -n demo-worker4 get infraenvs.agent-install.openshift.io myinfraenv  -o=jsonpath="{.status.createdTime}{'\n'}{.status.isoDownloadURL}{'\n'}"
```
```bash
2021-06-28T21:42:19Z
https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A
```

###### iPXE script and artifacts URLs:
If booting hosts using [iPXE](https://github.com/openshift/assisted-service/blob/5d4d836747862f43fa2ec882e5871648bd12c780/docs/enhancements/ipxe-host-boot.md#ipxe-host-boot), the `InfraEnv` CR will update and display the download URLs for the iPXE script and its artifacts once they're ready to be downloaded.

```bash
kubectl -n demo-worker4 get infraenvs.agent-install.openshift.io myinfraenv -o=jsonpath="{'iPXE Script Download URL: '}{.status.bootArtifacts.ipxeScript}{'\n'}{'Initrd Download URL: '}{.status.bootArtifacts.initrd}{'\n'}{'Rootfs Download URL: '}{.status.bootArtifacts.rootfs}{'\n'}{'Kernel Download URL: '}{.status.bootArtifacts.kernel}{'\n'}"
```
```bash
iPXE Script Download URL: https://assisted-service.example.com/v2/infra-envs/afe293b9-e3a0-47ed-8952-f26721626497/downloads/files?file_name=ipxe-script
Initrd Download URL: https://image-service.example.com/images/afe293b9-e3a0-47ed-8952-f26721626497/pxe-initrd?version=4.10&arch=x86_64
Rootfs Download URL: https://image-service.example.com/boot-artifacts/rootfs?version=4.10&arch=x86_64
Kernel Download URL: https://image-service.example.com/boot-artifacts/kernel?version=4.10&arch=x86_64
```

###### assisted-service log:
```go
time="2021-06-28T21:42:19Z" level=info msg="InfraEnv Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:81" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:19Z" level=info msg="the amount of nmStateConfigs included in the image is: 1" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).ensureISO" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:288" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:19Z" level=info msg="prepare image for cluster 02a89bb9-6141-4d14-a82e-42f254217502" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).GenerateClusterISOInternal" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:865" go-id=654 pkg=Inventory request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:19Z" level=info msg="Successfully uploaded file 02a89bb9-6141-4d14-a82e-42f254217502/discovery.ign" func="github.com/openshift/assisted-service/pkg/s3wrapper.(*FSClient).Upload" file="/go/src/github.com/openshift/origin/pkg/s3wrapper/filesystem.go:76" go-id=654 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:19Z" level=info msg="Filesystem '/data/' usage is 3.6%" func="github.com/openshift/assisted-service/pkg/s3wrapper.(*FSClientDecorator).conditionalLog" file="/go/src/github.com/openshift/origin/pkg/s3wrapper/filesystem.go:426"
time="2021-06-28T21:42:19Z" level=info msg="Creating minimal ISO for cluster 02a89bb9-6141-4d14-a82e-42f254217502" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).generateClusterMinimalISO.func1" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:1102" go-id=654 pkg=Inventory request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="Start configuring static network for 1 hosts" func="github.com/openshift/assisted-service/pkg/staticnetworkconfig.(*StaticNetworkConfigGenerator).GenerateStaticNetworkConfigData" file="/go/src/github.com/openshift/origin/pkg/staticnetworkconfig/generator.go:45" pkg=static_network_config
time="2021-06-28T21:42:20Z" level=info msg="Adding NMConnection file <eth0.nmconnection>" func="github.com/openshift/assisted-service/pkg/staticnetworkconfig.(*StaticNetworkConfigGenerator).createNMConnectionFiles" file="/go/src/github.com/openshift/origin/pkg/staticnetworkconfig/generator.go:128" pkg=static_network_config
time="2021-06-28T21:42:20Z" level=info msg="Adding NMConnection file <eth1.nmconnection>" func="github.com/openshift/assisted-service/pkg/staticnetworkconfig.(*StaticNetworkConfigGenerator).createNMConnectionFiles" file="/go/src/github.com/openshift/origin/pkg/staticnetworkconfig/generator.go:128" pkg=static_network_config
time="2021-06-28T21:42:20Z" level=info msg="Uploading minimal ISO for cluster 02a89bb9-6141-4d14-a82e-42f254217502" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).generateClusterMinimalISO" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:1118" go-id=654 pkg=Inventory request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="Successfully uploaded file discovery-image-02a89bb9-6141-4d14-a82e-42f254217502.iso" func="github.com/openshift/assisted-service/pkg/s3wrapper.(*FSClient).UploadStream" file="/go/src/github.com/openshift/origin/pkg/s3wrapper/filesystem.go:159" go-id=654 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="Generated cluster <02a89bb9-6141-4d14-a82e-42f254217502> image with ignition config {\n  \"ignition\": {\n    \"version\": \"3.1.0\"\n  },\n  \"passwd\": {\n    \"users\": [\n      *****\n    ]\n  },\n  \"systemd\": {\n    \"units\": [{\n      \"name\": \"agent.service\",\n      \"enabled\": true,\n      \"contents\": \"[Service]\\nType=simple\\nRestart=always\\nRestartSec=3\\nStartLimitInterval=0\\nEnvironment=HTTP_PROXY=\\nEnvironment=http_proxy=\\nEnvironment=HTTPS_PROXY=\\nEnvironment=https_proxy=\\nEnvironment=NO_PROXY=\\nEnvironment=no_proxy=\\nEnvironment=PULL_SECRET_TOKEN=*****\\nTimeoutStartSec=180\\nExecStartPre=/usr/local/bin/agent-fix-bz1964591 quay.io/edge-infrastructure/assisted-installer-agent:latest\\nExecStartPre=podman run --privileged --rm -v /usr/local/bin:/hostbin quay.io/edge-infrastructure/assisted-installer-agent:latest cp /usr/bin/agent /hostbin\\nExecStart=/usr/local/bin/agent --url https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org --cluster-id 02a89bb9-6141-4d14-a82e-42f254217502 --agent-version quay.io/edge-infrastructure/assisted-installer-agent:latest --insecure=false  --cacert /etc/assisted-service/service-ca-cert.crt\\n\\n[Unit]\\nWants=network-online.target\\nAfter=network-online.target\\n\\n[Install]\\nWantedBy=multi-user.target\"\n    },\n    {\n        \"name\": \"selinux.service\",\n        \"enabled\": true,\n        \"contents\": \"[Service]\\nType=oneshot\\nExecStartPre=checkmodule -M -m -o /root/assisted.mod /root/assisted.te\\nExecStartPre=semodule_package -o /root/assisted.pp -m /root/assisted.mod\\nExecStart=semodule -i /root/assisted.pp\\n\\n[Install]\\nWantedBy=multi-user.target\"\n    }\n    ]\n  },\n  \"storage\": {\n    \"files\": [{\n      \"overwrite\": true,\n      \"path\": \"/usr/local/bin/agent-fix-bz1964591\",\n      \"mode\": 755,\n      \"user\": {\n          \"name\": \"root\"\n      },\n      \"contents\": { \"source\": \"data:,%23%21%2Fusr%2Fbin%2Fsh%0A%0A%23%20This%20script%20is%20a%20workaround%20for%20bugzilla%201964591%20where%20symlinks%20inside%20%2Fvar%2Flib%2Fcontainers%2F%20get%0A%23%20corrupted%20under%20some%20circumstances.%0A%23%0A%23%20In%20order%20to%20let%20agent.service%20start%20correctly%20we%20are%20checking%20here%20whether%20the%20requested%0A%23%20container%20image%20exists%20and%20in%20case%20%22podman%20images%22%20returns%20an%20error%20we%20try%20removing%20the%20faulty%0A%23%20image.%0A%23%0A%23%20In%20such%20a%20scenario%20agent.service%20will%20detect%20the%20image%20is%20not%20present%20and%20pull%20it%20again.%20In%20case%0A%23%20the%20image%20is%20present%20and%20can%20be%20detected%20correctly%2C%20no%20any%20action%20is%20required.%0A%0AIMAGE=$%28echo%20$1%20%7C%20sed%20%27s%2F:.%2A%2F%2F%27%29%0Apodman%20images%20%7C%20grep%20$IMAGE%20%7C%7C%20podman%20rmi%20--force%20$1%20%7C%7C%20true%0A\" }\n    },\n    {\n      \"overwrite\": true,\n      \"path\": \"/etc/motd\",\n      \"mode\": 420,\n      \"user\": {\n          \"name\": \"root\"\n      },\n      \"contents\": { \"source\": \"data:,%0A%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%0AThis%20is%20a%20host%20being%20installed%20by%20the%20OpenShift%20Assisted%20Installer.%0AIt%20will%20be%20installed%20from%20scratch%20during%20the%20installation.%0AThe%20primary%20service%20is%20agent.service.%20%20To%20watch%20its%20status%20run%20e.g%0Asudo%20journalctl%20-u%20agent.service%0A%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%20%20%2A%2A%0A\" }\n    },\n    {\n      \"overwrite\": true,\n      \"path\": \"/etc/NetworkManager/conf.d/01-ipv6.conf\",\n      \"mode\": 420,\n      \"user\": {\n          \"name\": \"root\"\n      },\n      \"contents\": { \"source\": \"data:,%0A%5Bconnection%5D%0Aipv6.dhcp-iaid=mac%0Aipv6.dhcp-duid=ll%0A\" }\n    },\n    {\n        \"overwrite\": true,\n        \"path\": \"/root/.docker/config.json\",\n        \"mode\": 420,\n        \"user\": {\n            \"name\": \"root\"\n        },\n        \"contents\": { \"source\": \"data:,*****\" }\n    },\n    {\n        \"overwrite\": true,\n        \"path\": \"/root/assisted.te\",\n        \"mode\": 420,\n        \"user\": {\n            \"name\": \"root\"\n        },\n        \"contents\": { \"source\": \"data:text/plain;base64,Cm1vZHVsZSBhc3Npc3RlZCAxLjA7CnJlcXVpcmUgewogICAgICAgIHR5cGUgY2hyb255ZF90OwogICAgICAgIHR5cGUgY29udGFpbmVyX2ZpbGVfdDsKICAgICAgICB0eXBlIHNwY190OwogICAgICAgIGNsYXNzIHVuaXhfZGdyYW1fc29ja2V0IHNlbmR0bzsKICAgICAgICBjbGFzcyBkaXIgc2VhcmNoOwogICAgICAgIGNsYXNzIHNvY2tfZmlsZSB3cml0ZTsKfQojPT09PT09PT09PT09PSBjaHJvbnlkX3QgPT09PT09PT09PT09PT0KYWxsb3cgY2hyb255ZF90IGNvbnRhaW5lcl9maWxlX3Q6ZGlyIHNlYXJjaDsKYWxsb3cgY2hyb255ZF90IGNvbnRhaW5lcl9maWxlX3Q6c29ja19maWxlIHdyaXRlOwphbGxvdyBjaHJvbnlkX3Qgc3BjX3Q6dW5peF9kZ3JhbV9zb2NrZXQgc2VuZHRvOwo=\" }\n    },\n    {\n      \"overwrite\": true,\n      \"path\": \"/etc/pki/ca-trust/source/anchors/rh-it-root-ca.crt\",\n      \"mode\": 420,\n      \"user\": {\n          \"name\": \"root\"\n      },\n      \"contents\": { \"source\": \"data:,*****\" }\n    },\n    {\n      \"path\": \"/etc/assisted-service/service-ca-cert.crt\",\n      \"mode\": 420,\n      \"overwrite\": true,\n      \"user\": {\n        \"name\": \"root\"\n      },\n      \"contents\": { \"source\": \"data:text/plain;charset=utf-8;base64,LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURkekNDQWwrZ0F3SUJBZ0lJWTVQM1lsaUt1azB3RFFZSktvWklodmNOQVFFTEJRQXdKakVrTUNJR0ExVUUKQXd3YmFXNW5jbVZ6Y3kxdmNHVnlZWFJ2Y2tBeE5qSTBORFExT0RNMU1CNFhEVEl4TURZeU16RXhNREF3TlZvWApEVEl6TURZeU16RXhNREF3Tmxvd0t6RXBNQ2NHQTFVRUF3d2dLaTVoY0hCekxtOXpkR1Z6ZEM1MFpYTjBMbTFsCmRHRnNhM1ZpWlM1dmNtY3dnZ0VpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElCRHdBd2dnRUtBb0lCQVFEbkVudGoKWHFIdjdtK3grMis5Z0JHNlJ5d2VjcU5KeUYvdkk3S1h3Y21GcWJNbkhFdXlpZysyc2NTeEdVL2psNURwK1pHNgp4KzFyL0lPNjh6UGxPYjJGTXhOYk9GVmFCTG4rODd2QXpZSUREaW9GVE5nL25RVnBDYlI1SzBOMGY5TVk5OWRtCkVHaGNXRW9IYmVMcDdLeThqL0ZYWEdxV1BmNzNaSmhFVk5wTmVpUUMxcXVpcnhNa3Nha09yQXdXaVpuVy95SnEKZkUzdlZ4K25NeFQ2SHdJZ010WURPUmxkTloxdk9MeXU1eVdiZXRZY05GM1ROSzVsZmh5OURWWUJRTFNBTkhuSQprTXJvbmg0aHhpRXJXQlQ2OVhqd29VMFNPU2QyZGFvN1p1bmdZTjZWaGNyMEpxbjVvVVdNaFR0emVkS3h6V1lXCmdlaDlwa1FRcVFWQ0hqRXhBZ01CQUFHamdhTXdnYUF3RGdZRFZSMFBBUUgvQkFRREFnV2dNQk1HQTFVZEpRUU0KTUFvR0NDc0dBUVVGQndNQk1Bd0dBMVVkRXdFQi93UUNNQUF3SFFZRFZSME9CQllFRkppVld5UU1jNUVwbWsxawpGajZocGZmUWROR3dNQjhHQTFVZEl3UVlNQmFBRkhrUEk2Q2ZQWVJrc1Z1anRPa1o1eTRCSmlJdU1Dc0dBMVVkCkVRUWtNQ0tDSUNvdVlYQndjeTV2YzNSbGMzUXVkR1Z6ZEM1dFpYUmhiR3QxWW1VdWIzSm5NQTBHQ1NxR1NJYjMKRFFFQkN3VUFBNElCQVFCUkgzOTlzNmhray94VzdwV3NsRTdXYU1icGdMTWZ4TU8rQXlFVTNKblFhQUo1aVdIVQo2NFcxZG5VWUdNY1dTZ1FUS0JRVTJOZ2kxSUFBajA5OWVuOHhtbTFFUzZoNzF6bU5NSXhsMXVrd0FPUjRha0QvCk5VMUsxUis2cG9vS3hTQ2xEaVB4UHBmK3JFNDA2ZzFjRVhqODhHOTh3TWNGcDRtdnZFcjdNU0ptSGFOMUNobjQKdHk4SVZDa0w5MzFsNjNCRStkM29QS0M2TCtsWXBIZ29pSHp5c0hIc1FaaUhhWU9aMk9tY3EvbWZQQnRSRHVTcwp4WDhjRlpGemFBR3RpTTZvZFNTOElWTDdPVkpBTldGbWI1RnBUUGxjbDZpNWEvQ2R5RWZCQkdPeDJXbFNHWHYyCmphQlBDWnNKeWdicURmMjRYZFBXckpqNW5DNTdkTGxPWklDMwotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tQkVHSU4gQ0VSVElGSUNBVEUtLS0tLQpNSUlERERDQ0FmU2dBd0lCQWdJQkFUQU5CZ2txaGtpRzl3MEJBUXNGQURBbU1TUXdJZ1lEVlFRRERCdHBibWR5ClpYTnpMVzl3WlhKaGRHOXlRREUyTWpRME5EVTRNelV3SGhjTk1qRXdOakl6TVRBMU56RTFXaGNOTWpNd05qSXoKTVRBMU56RTJXakFtTVNRd0lnWURWUVFEREJ0cGJtZHlaWE56TFc5d1pYSmhkRzl5UURFMk1qUTBORFU0TXpVdwpnZ0VpTUEwR0NTcUdTSWIzRFFFQkFRVUFBNElCRHdBd2dnRUtBb0lCQVFEbE5DOU96bVNpTnU1d01zTjNYSFlYCnhMTmpPZjFHUmFIbi9OVVdvU2JXMHhpK3ZVY05VNW0yMXQ2cjFnZ3FXekNvR3Y4L0pTZVhWTWR0bVVhOGxSaTkKa21sZTFDMUROZEJJbEM0R21rcmlDaHE3dDN2bnVpOTB5REtaYUFKVExYRzE5WGZOVHBTbHI4anFIZUJ6Z29aegpDWHZrZGNlZUVFVDg3bEgzUFBNWXhMTE1ZRG15UjNlSU9mdzNLcVlxalg5ZE1jZzhtMU1uMHFPbVg1cXJodVROCnRtRWo3Q3NEVVJUd1dVRHlCVDNDdDVsTUFsR1NKeWEwNE5EUDYwaS9pYWk1cmdsZEJrb1lCd0F6TXRBRUdoUisKUkZCajdBamk3c2JtZGpUZGhQbXBYS0Q4QjYrbmljTmNWeUw5OEora0pDQVJWQXRIVXNGdjM3elY4UnNHN2N2WApBZ01CQUFHalJUQkRNQTRHQTFVZER3RUIvd1FFQXdJQ3BEQVNCZ05WSFJNQkFmOEVDREFHQVFIL0FnRUFNQjBHCkExVWREZ1FXQkJSNUR5T2duejJFWkxGYm83VHBHZWN1QVNZaUxqQU5CZ2txaGtpRzl3MEJBUXNGQUFPQ0FRRUEKTkF1RUNUSUxBMTFiNVJDWmtqbTd4STBDMjJVcTFiTWNyRGJTaVBxaDhCaHZTWW1lVERvWmFyRXhFMGdRTDdoMQpQRWhWWHdXZDU4eVQzQ0JGbHE1Wlc0UnhPMkFtbS8zRUNsS3htZU9kRWtFR1lyczhZbzJvcEVDQ2M5YWd0WXBRCnhKZVJZQzZJMERBL0dudmNaK3Q2YXh3UHhnZHpvT3JLdmhTaDd0ZUUxeFdVSStId3p0cTY1L2VOMDl0MWMzZkcKVnVCc3ZoUkNZQytWZmNuZFJxaTBzUzJvQk5RWW1QbU02bWhxRVJxSzczeDhhT2RYSWdVZjM4bXh4bWF4b3NiYQo2QmlQMDhWemN0SFlmZGdFSVpEVElUNXlUcUhQcHgxNWVIVksrTVlTc2FaK2tJdG9YcHB0Y1FBZzhxN09BVUlkCjZ4czdjM0ZqWlhtaUVidHZlZDE1ZXc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==\" }\n    }]\n  }\n}" func="github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).getIgnitionConfigForLogging" file="/go/src/github.com/openshift/origin/internal/bminventory/inventory.go:1064" go-id=654 pkg=Inventory request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="ClusterDeployment Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:115" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=f417920f-897c-49af-ba3e-b467026c94b0
time="2021-06-28T21:42:20Z" level=info msg="ISODownloadURL changed from  to https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).updateEnsureISOSuccess" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:318" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="InfraEnv Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:78" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=5ad649fd-bf49-4b41-aa33-c35a601c1555
time="2021-06-28T21:42:20Z" level=info msg="InfraEnv Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:81" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=0d9c0da4-beab-4a41-85a3-4444728dd6f0
time="2021-06-28T21:42:20Z" level=info msg="the amount of nmStateConfigs included in the image is: 1" func="github.com/openshift/assisted-service/internal/controller/controllers.(*InfraEnvReconciler).ensureISO" file="/go/src/github.com/openshift/origin/internal/controller/controllers/infraenv_controller.go:288" go-id=654 infra_env=myinfraenv infra_env_namespace=demo-worker4 request_id=0d9c0da4-beab-4a41-85a3-4444728dd6f0
```

###### Inspect Assisted Service Cluster Events
Get the full event list here:
```bash
curl -s -k $(kubectl -n demo-worker4 get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.status.debugInfo.eventsURL}")  | jq "."
```

At this point, you should expect to see events such as:
```json
  {
    "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
    "event_time": "2021-06-28T21:01:31.938Z",
    "message": "Registered cluster \"test-cluster\"",
    "severity": "info"
  },
  {
    "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
    "event_time": "2021-06-28T21:01:31.941Z",
    "message": "Successfully registered cluster test-cluster with id 02a89bb9-6141-4d14-a82e-42f254217502",
    "severity": "info"
  },
  {
    "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
    "event_time": "2021-06-28T21:42:20.768Z",
    "message": "Generated image (Image type is \"minimal-iso\", SSH public key is not set)",
    "severity": "info"
  },
```

### Boot The Host From The Discovery Image
* The procedure described below is called: **Boot It Yourself**.
* There is an alternative automated procedure using `BMH`. For details, check the [Zero Touch Provisioning (ZTP) advanced section](#advanced-zero-touch-provisioning).

#### 1. Download the image
* Minimal-ISO is the default image type here; therefore, expect an image at the size of ~100MB.

```bash
curl --insecure -o image.iso $(kubectl -n demo-worker4 get infraenvs.agent-install.openshift.io myinfraenv -o=jsonpath="{.status.isoDownloadURL}")
```

#### 2. Boot the host from image
* Use virtual media / USB drive / other methods to boot your host using the discovery image.

### 3. Host discovery and approval
* The Agent CRD represents a Host that boot from a discovery image and registered to a cluster, and created automatically by assisted-service. [Find more details about the agent CRD here](README.md#agent).
* At first, it will look as follows:
```bash
kubectl -n demo-worker4 get agents
```
```bash
NAME                                   CLUSTER        APPROVED
07e80ea9-200c-4f82-aff4-4932acb773d4   test-cluster   false
```
* To trigger the installation, you will need to approve the agent

```bash
kubectl -n demo-worker4 patch agents.agent-install.openshift.io 07e80ea9-200c-4f82-aff4-4932acb773d4 -p '{"spec":{"approved":true}}' --type merge
```
* You should now see it got approved
```bash
NAME                                   CLUSTER        APPROVED
07e80ea9-200c-4f82-aff4-4932acb773d4   test-cluster   true
```

### 4. Cluster Installation
* Installation has started, to monitor the progress you may inspect cluster events:
```bash
curl -s -k $(kubectl -n demo-worker4 get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.status.debugInfo.eventsURL}")  | jq "."
```
* Another option is to inspect `AgentClusterInstall` conditions, [as mentioned above](#AgentClusterInstall-resource-conditions).

### 5. End Result
* Lastly, when installed successfully:
```json
{
  "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
  "event_time": "2021-06-29T16:45:21.643Z",
  "message": "Successfully finished installing cluster test-cluster",
  "severity": "info"
}
```

## Advanced: Zero Touch Provisioning
* The procedure described below is called: **Zero Touch Provisioning (ZTP)**.
* There is an alternative manual procedure, not involving `BMH`. For details, check the [Boot The Host From The Discovery Image](#boot-the-host-from-the-discovery-image).

### Utilize BareMetalHost

* Examples below generated by [dev-scripts](https://github.com/openshift-metal3/dev-scripts/) and were modified manually.
* Control the host via BMC, which allows a host remote connection and ability to power cycle independently from the host operating system.

#### 1. Create BMC Secret

```yaml
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Secret
metadata:
  name: ostest-extraworker-4-bmc-secret
  namespace: demo-worker4
type: Opaque
data:
  username: foo # Change me
  password: bar # Change me
EOF
```
#### 2. Create [BareMetalHost](baremetal-agent-controller.md#creating-baremetalhost-resources)
```yaml
cat <<EOF | kubectl create -f -
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  name: ostest-extraworker-4
  namespace: demo-worker4
  annotations:
    inspect.metal3.io: disabled
  labels:
    infraenvs.agent-install.openshift.io: "myinfraenv" # Must match the InfraEnv name and exist in the same namespace
spec:
  online: true
  automatedCleaningMode: disabled
  bootMACAddress: 00:cd:16:5c:68:47  # Change to your mac address
  bmc:
    address: redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/dd8071d3-4ab5-47c1-aa1d-b2f1e9db0d0c # Should be changed to match your machine
    credentialsName: ostest-extraworker-4-bmc-secret  # Must match the bmc secret you have just created
EOF
```

---
**NOTE**

Prior to ACM 2.9: BMAC sets the field `automatedCleaningMode` to `disabled` even if 
the `BareMetalHost` manifest specifies another value (e.g. `automatedCleaningMode: metadata`).

In ACM 2.9+: BMAC no longer sets `automatedCleaningMode` and will respect the value set
in the `BareMetalHost` manifest when created.

If the `automatedCleaningMode` field is not set when the `BareMetalHost` manifest is created, then the 
BMO's webhook will automatically set the field to `metadata`.

---


##### Result
* Host turned on.
* Image download started. This might take a while.
* Host discovery happened. An `Agent` CR got created automatically.

Having issues? try this [troubleshooting section](baremetal-agent-controller.md#Troubleshooting)

###### virsh status example:
* Relevant only for deployments that use virtual machines (managed by libvirt) for simulating bare metal hosts.
```bash
 Id   Name                   State
---------------------------------------
 5    ostest_master_1        running
 6    ostest_master_2        running
 7    ostest_master_0        running
 10   ostest_worker_1        running
 11   ostest_worker_0        running
 15   ostest_extraworker_4   running  # Our host is now up and running
```

###### custom resource events:
```bash
kubectl -n demo-worker4 describe baremetalhosts.metal3.io ostest-extraworker-4 | grep Events\: -A30
```
```bash
Events:
  Type    Reason                Age   From                         Message
  ----    ------                ----  ----                         -------
  Normal  Registered            19m   metal3-baremetal-controller  Registered new host
  Normal  BMCAccessValidated    19m   metal3-baremetal-controller  Verified access to BMC
  Normal  InspectionSkipped     19m   metal3-baremetal-controller  disabled by annotation
  Normal  ProfileSet            19m   metal3-baremetal-controller  Hardware profile set: unknown
  Normal  ProvisioningStarted   19m   metal3-baremetal-controller  Image provisioning started for https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A
  Normal  ProvisioningComplete  19m   metal3-baremetal-controller  Image provisioning completed for https://assisted-service-assisted-installer.apps.ostest.test.metalkube.org/api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A
```

###### assisted-service log:
```go
time="2021-06-29T15:45:19Z" level=info msg="BareMetalHost Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:139" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=debug msg="Started BMH reconcile for demo-worker4/ostest-extraworker-4" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:503" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=debug msg="BMH value &{{BareMetalHost metal3.io/v1alpha1} {ostest-extraworker-4  demo-worker4  261f7856-e01c-48cb-9b6c-cd6873bf38be 4580659 1 2021-06-29 15:45:19 +0000 UTC <nil> <nil> map[infraenvs.agent-install.openshift.io:myinfraenv] map[inspect.metal3.io:disabled] [] []  [{kubectl-create Update metal3.io/v1alpha1 2021-06-29 15:45:19 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:annotations\":{\".\":{},\"f:inspect.metal3.io\":{}},\"f:labels\":{\".\":{},\"f:infraenvs.agent-install.openshift.io\":{}}},\"f:spec\":{\".\":{},\"f:automatedCleaningMode\":{},\"f:bmc\":{\".\":{},\"f:address\":{},\"f:credentialsName\":{}},\"f:bootMACAddress\":{},\"f:online\":{}}}}]} {[] {redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/dd8071d3-4ab5-47c1-aa1d-b2f1e9db0d0c ostest-extraworker-4-bmc-secret false} <nil>  <nil>  00:cd:16:5c:68:47 true nil <nil> nil nil nil  false disabled} {  <nil>  <nil> {  {   <nil>} <nil>  <nil>} {nil } {nil }  false {{0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC}} 0}}" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:504" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=debug msg="BMH label infraenvs.agent-install.openshift.io value myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:472" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=debug msg="Loading InfraEnv myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:479" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=debug msg="Updating dirty BMH &{{BareMetalHost metal3.io/v1alpha1} {ostest-extraworker-4  demo-worker4  261f7856-e01c-48cb-9b6c-cd6873bf38be 4580659 1 2021-06-29 15:45:19 +0000 UTC <nil> <nil> map[infraenvs.agent-install.openshift.io:myinfraenv] map[inspect.metal3.io:disabled] [] []  [{kubectl-create Update metal3.io/v1alpha1 2021-06-29 15:45:19 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:annotations\":{\".\":{},\"f:inspect.metal3.io\":{}},\"f:labels\":{\".\":{},\"f:infraenvs.agent-install.openshift.io\":{}}},\"f:spec\":{\".\":{},\"f:automatedCleaningMode\":{},\"f:bmc\":{\".\":{},\"f:address\":{},\"f:credentialsName\":{}},\"f:bootMACAddress\":{},\"f:online\":{}}}}]} {[] {redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/dd8071d3-4ab5-47c1-aa1d-b2f1e9db0d0c ostest-extraworker-4-bmc-secret false} <nil>  <nil>  00:cd:16:5c:68:47 true nil <nil> nil nil nil  false disabled} {  <nil>  <nil> {  {   <nil>} <nil>  <nil>} {nil } {nil }  false {{0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC}} 0}}" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:152" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
time="2021-06-29T15:45:19Z" level=info msg="BareMetalHost Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:136" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=e526f6a1-77a1-4aea-8331-4872dbfe3b4a
...
time="2021-06-29T15:45:30Z" level=info msg="BareMetalHost Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:139" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=debug msg="Started BMH reconcile for demo-worker4/ostest-extraworker-4" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:503" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=debug msg="BMH value &{{BareMetalHost metal3.io/v1alpha1} {ostest-extraworker-4  demo-worker4  261f7856-e01c-48cb-9b6c-cd6873bf38be 4580764 2 2021-06-29 15:45:19 +0000 UTC <nil> <nil> map[infraenvs.agent-install.openshift.io:myinfraenv] map[inspect.metal3.io:disabled] [] [baremetalhost.metal3.io]  [{kubectl-create Update metal3.io/v1alpha1 2021-06-29 15:45:19 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:annotations\":{\".\":{},\"f:inspect.metal3.io\":{}},\"f:labels\":{\".\":{},\"f:infraenvs.agent-install.openshift.io\":{}}},\"f:spec\":{\".\":{},\"f:automatedCleaningMode\":{},\"f:bmc\":{\".\":{},\"f:address\":{},\"f:credentialsName\":{}},\"f:bootMACAddress\":{},\"f:online\":{}}}} {assisted-service Update metal3.io/v1alpha1 2021-06-29 15:45:30 +0000 UTC FieldsV1 {\"f:spec\":{\"f:image\":{\".\":{},\"f:format\":{},\"f:url\":{}}}}} {baremetal-operator Update metal3.io/v1alpha1 2021-06-29 15:45:30 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:finalizers\":{\".\":{},\"v:\\\"baremetalhost.metal3.io\\\"\":{}}},\"f:status\":{\".\":{},\"f:errorCount\":{},\"f:errorMessage\":{},\"f:goodCredentials\":{\".\":{},\"f:credentials\":{\".\":{},\"f:name\":{},\"f:namespace\":{}},\"f:credentialsVersion\":{}},\"f:hardwareProfile\":{},\"f:lastUpdated\":{},\"f:operationHistory\":{\".\":{},\"f:deprovision\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:inspect\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:provision\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:register\":{\".\":{},\"f:end\":{},\"f:start\":{}}},\"f:operationalStatus\":{},\"f:poweredOn\":{},\"f:provisioning\":{\".\":{},\"f:ID\":{},\"f:bootMode\":{},\"f:image\":{\".\":{},\"f:url\":{}},\"f:rootDeviceHints\":{\".\":{},\"f:deviceName\":{}},\"f:state\":{}},\"f:triedCredentials\":{\".\":{},\"f:credentials\":{\".\":{},\"f:name\":{},\"f:namespace\":{}},\"f:credentialsVersion\":{}}}}}]} {[] {redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/dd8071d3-4ab5-47c1-aa1d-b2f1e9db0d0c ostest-extraworker-4-bmc-secret false} <nil>  <nil>  00:cd:16:5c:68:47 true nil 0xc0013bec40 nil nil nil  false disabled} {OK  2021-06-29 15:45:30 +0000 UTC unknown <nil> {ready 5600a851-4a0f-4b9b-b9f7-c4131b63180d {   <nil>} 0xc00427fef0 UEFI <nil>} {&SecretReference{Name:ostest-extraworker-4-bmc-secret,Namespace:demo-worker4,} 4580662} {&SecretReference{Name:ostest-extraworker-4-bmc-secret,Namespace:demo-worker4,} 4580662}  false {{2021-06-29 15:45:19 +0000 UTC 2021-06-29 15:45:30 +0000 UTC} {2021-06-29 15:45:30 +0000 UTC 2021-06-29 15:45:30 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC}} 0}}" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:504" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=debug msg="BMH label infraenvs.agent-install.openshift.io value myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:472" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=debug msg="Loading InfraEnv myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:479" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=info msg="Stopping reconcileBMH: Either the InfraEnv image is not ready or there is nothing to update." func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:550" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=info msg="BareMetalHost Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:136" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=aae0e368-3e1b-44d4-a4df-047080e7cd18
time="2021-06-29T15:45:30Z" level=info msg="BareMetalHost Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:139" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=debug msg="Started BMH reconcile for demo-worker4/ostest-extraworker-4" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:503" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=debug msg="BMH value &{{BareMetalHost metal3.io/v1alpha1} {ostest-extraworker-4  demo-worker4  261f7856-e01c-48cb-9b6c-cd6873bf38be 4580765 2 2021-06-29 15:45:19 +0000 UTC <nil> <nil> map[infraenvs.agent-install.openshift.io:myinfraenv] map[inspect.metal3.io:disabled] [] [baremetalhost.metal3.io]  [{kubectl-create Update metal3.io/v1alpha1 2021-06-29 15:45:19 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:annotations\":{\".\":{},\"f:inspect.metal3.io\":{}},\"f:labels\":{\".\":{},\"f:infraenvs.agent-install.openshift.io\":{}}},\"f:spec\":{\".\":{},\"f:automatedCleaningMode\":{},\"f:bmc\":{\".\":{},\"f:address\":{},\"f:credentialsName\":{}},\"f:bootMACAddress\":{},\"f:online\":{}}}} {assisted-service Update metal3.io/v1alpha1 2021-06-29 15:45:30 +0000 UTC FieldsV1 {\"f:spec\":{\"f:image\":{\".\":{},\"f:format\":{},\"f:url\":{}}}}} {baremetal-operator Update metal3.io/v1alpha1 2021-06-29 15:45:30 +0000 UTC FieldsV1 {\"f:metadata\":{\"f:finalizers\":{\".\":{},\"v:\\\"baremetalhost.metal3.io\\\"\":{}}},\"f:status\":{\".\":{},\"f:errorCount\":{},\"f:errorMessage\":{},\"f:goodCredentials\":{\".\":{},\"f:credentials\":{\".\":{},\"f:name\":{},\"f:namespace\":{}},\"f:credentialsVersion\":{}},\"f:hardwareProfile\":{},\"f:lastUpdated\":{},\"f:operationHistory\":{\".\":{},\"f:deprovision\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:inspect\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:provision\":{\".\":{},\"f:end\":{},\"f:start\":{}},\"f:register\":{\".\":{},\"f:end\":{},\"f:start\":{}}},\"f:operationalStatus\":{},\"f:poweredOn\":{},\"f:provisioning\":{\".\":{},\"f:ID\":{},\"f:bootMode\":{},\"f:image\":{\".\":{},\"f:url\":{}},\"f:rootDeviceHints\":{\".\":{},\"f:deviceName\":{}},\"f:state\":{}},\"f:triedCredentials\":{\".\":{},\"f:credentials\":{\".\":{},\"f:name\":{},\"f:namespace\":{}},\"f:credentialsVersion\":{}}}}}]} {[] {redfish-virtualmedia+http://192.168.111.1:8000/redfish/v1/Systems/dd8071d3-4ab5-47c1-aa1d-b2f1e9db0d0c ostest-extraworker-4-bmc-secret false} <nil>  <nil>  00:cd:16:5c:68:47 true nil 0xc001068900 nil nil nil  false disabled} {OK  2021-06-29 15:45:30 +0000 UTC unknown <nil> {provisioning 5600a851-4a0f-4b9b-b9f7-c4131b63180d {   <nil>} 0xc00104acf0 UEFI <nil>} {&SecretReference{Name:ostest-extraworker-4-bmc-secret,Namespace:demo-worker4,} 4580662} {&SecretReference{Name:ostest-extraworker-4-bmc-secret,Namespace:demo-worker4,} 4580662}  false {{2021-06-29 15:45:19 +0000 UTC 2021-06-29 15:45:30 +0000 UTC} {2021-06-29 15:45:30 +0000 UTC 2021-06-29 15:45:30 +0000 UTC} {2021-06-29 15:45:30 +0000 UTC 0001-01-01 00:00:00 +0000 UTC} {0001-01-01 00:00:00 +0000 UTC 0001-01-01 00:00:00 +0000 UTC}} 0}}" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:504" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=debug msg="BMH label infraenvs.agent-install.openshift.io value myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:472" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=debug msg="Loading InfraEnv myinfraenv" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).findInfraEnvForBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:479" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=info msg="Stopping reconcileBMH: Either the InfraEnv image is not ready or there is nothing to update." func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).reconcileBMH" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:550" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:30Z" level=info msg="BareMetalHost Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*BMACReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/bmh_agent_controller.go:136" bare_metal_host=ostest-extraworker-4 bare_metal_host_namespace=demo-worker4 go-id=655 request_id=6eb8ccc7-80a6-49ea-8fa7-280489a233a7
time="2021-06-29T15:45:33Z" level=debug msg="Request: {GET /api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A HTTP/1.1 1 1 map[Accept:[*/*] Accept-Encoding:[gzip, deflate] Forwarded:[for=192.168.111.21;host=assisted-service-assisted-installer.apps.ostest.test.metalkube.org;proto=https] User-Agent:[python-requests/2.20.0] X-Forwarded-For:[192.168.111.21] X-Forwarded-Host:[assisted-service-assisted-installer.apps.ostest.test.metalkube.org] X-Forwarded-Port:[443] X-Forwarded-Proto:[https]] {} <nil> 0 [] false assisted-service-assisted-installer.apps.ostest.test.metalkube.org map[] map[] <nil> map[] 10.131.0.1:42932 /api/assisted-install/v1/clusters/02a89bb9-6141-4d14-a82e-42f254217502/downloads/image?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJjbHVzdGVyX2lkIjoiMDJhODliYjktNjE0MS00ZDE0LWE4MmUtNDJmMjU0MjE3NTAyIn0.ciL2w3g89ftu-R-Z3-KwrDtlll2kd4EMhbrE3YLZkQNQBEdCJ1gg5Sjgjfj4Ekbi7C3XsDEsIRSYTGJPL-Pu8A 0xc002f438c0 <nil> <nil> 0xc00170eff0}" func=github.com/openshift/assisted-service/internal/metrics.Handler.func1 file="/go/src/github.com/openshift/origin/internal/metrics/reporter.go:20" pkg=matched-h
time="2021-06-29T15:45:33Z" level=debug msg="Authenticating cluster 02a89bb9-6141-4d14-a82e-42f254217502 JWT" func="github.com/openshift/assisted-service/pkg/auth.(*LocalAuthenticator).AuthAgentAuth" file="/go/src/github.com/openshift/origin/pkg/auth/local_authenticator.go:82" pkg=auth
time="2021-06-29T15:45:33Z" level=debug msg="Pushing cluster event single-node demo-worker4" func="github.com/openshift/assisted-service/internal/controller/controllers.(*controllerEventsWrapper).AddEvent" file="/go/src/github.com/openshift/origin/internal/controller/controllers/controller_event_wrapper.go:36"
time="2021-06-29T15:45:33Z" level=info msg="ClusterDeployment Reconcile started" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:115" cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=9d4b6bae-b3a2-420f-ad4b-12bb4fa3d5da
time="2021-06-29T15:45:33Z" level=info msg="ClusterDeployment Reconcile ended" func="github.com/openshift/assisted-service/internal/controller/controllers.(*ClusterDeploymentsReconciler).Reconcile.func1" file="/go/src/github.com/openshift/origin/internal/controller/controllers/clusterdeployments_controller.go:112" agent_cluster_install=test-agent-cluster-install agent_cluster_install_namespace=demo-worker4 cluster_deployment=single-node cluster_deployment_namespace=demo-worker4 go-id=684 request_id=9d4b6bae-b3a2-420f-ad4b-12bb4fa3d5da
```

###### Inspect Assisted Service Cluster Events
Get the full event list here:
```bash
curl -s -k $(kubectl -n demo-worker4 get agentclusterinstalls.extensions.hive.openshift.io test-agent-cluster-install -o=jsonpath="{.status.debugInfo.eventsURL}")  | jq "."
```

Expect to see an event that indicate the image download started:
```json
  {
    "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
    "event_time": "2021-06-29T15:45:33.489Z",
    "message": "Started image download (image type is \"minimal-iso\")",
    "severity": "info"
  }
```
### Agent Discovery and Installation
* When the host boots from the discovery image, it gets registered to assisted-service. At first it will look like this:
```bash
kubectl -n demo-worker4 get agents
```
```bash
NAME                                   CLUSTER        APPROVED
07e80ea9-200c-4f82-aff4-4932acb773d4   test-cluster   false
```
* BMAC will automatically approve the agent, which will start the installation.

```bash
NAME                                   CLUSTER        APPROVED
07e80ea9-200c-4f82-aff4-4932acb773d4   test-cluster   true
```
Lastly, when installed successfully:
```json
{
  "cluster_id": "02a89bb9-6141-4d14-a82e-42f254217502",
  "event_time": "2021-06-29T16:45:21.643Z",
  "message": "Successfully finished installing cluster test-cluster",
  "severity": "info"
}
```
