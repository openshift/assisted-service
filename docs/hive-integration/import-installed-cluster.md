# Import an existing OCP cluster to add additional nodes

It is possible to import an existing OCP cluster so that additional nodes may be added.

### Import the cluster and prepare it for the addition of further nodes:

#### 1: Create a namespace to which the imported cluster will belong.

```
cat << EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: spoke-cluster
EOF
```

#### 2: It is important to ensure that a ClusterImageSet matching the imported cluster is present.

Ensure that the infrastructure owner has made this available.

```
cat << EOF | oc apply -f -
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
  name: openshift-v4.11.18
spec:
  releaseImage: quay.io/openshift-release-dev/ocp-release@sha256:22e149142517dfccb47be828f012659b1ccf71d26620e6f62468c264a7ce7863
EOF
```

#### 3: Set up a pull secret, use a valid openshift pull secret.

```
cat << EOF | oc apply -f -
apiVersion: v1
kind: Secret
type: kubernetes.io/dockerconfigjson
metadata:
  name: pull-secret
  namespace: spoke-cluster
stringData:
  .dockerconfigjson: 'YOUR_PULL_SECRET_JSON_GOES_HERE'
EOF
```

#### 4: Copy the kubeconfig from the OCP cluster into the hub.

To obtain the KubeConfig from the OCP cluster, _make sure that KUBECONFIG is set to the cluster being imported_ and then use:
```
oc get secret -n openshift-kube-apiserver node-kubeconfigs -ojson | jq '.data["lb-ext.kubeconfig"]' --raw-output | base64 -d > /tmp/kubeconfig.some-other-cluster
```

Then _make sure that KUBECONFIG is set to the hub_ and use:
```
oc -n spoke-cluster create secret generic some-other-cluster-admin-kubeconfig --from-file=/tmp/kubeconfig.some-other-cluster
```

#### 5: Create an AgentClusterInstall and a ClusterDeployment, these should reference each other.

* Note that in the ClusterDeployment that it is very important to set `installed` to `true` so that the cluster will automatically be imported as a Day 2 cluster.
* Additionally, it is important to ensure that the `kubeconfig` created in the previous step is referenced here in `adminKubeconfigSecretRef`.
* Adding `adminKubeconfigSecretRef` requires the `clusterMetaData` stanza which requires definition of `clusterID` and `infraID`, for day2 purposes these may be empty strings.
* `spec.baseDomain` should match the domain being used for the cluster.

```
cat << EOF | oc apply -f -
apiVersion: extensions.hive.openshift.io/v1beta1
kind: AgentClusterInstall
metadata:
  name: some-other-cluster-install
  namespace: spoke-cluster
spec:
  networking:
    userManagedNetworking: true
  clusterDeploymentRef:
    name: some-other-cluster
  imageSetRef:
    name: openshift-v4.11.18
  provisionRequirements:
    controlPlaneAgents: 1
  sshPublicKey: "ssh-rsa ..." # This field is optional but if you want to be able to log into nodes for troubleshooting purposes then this is handy.
EOF

cat << EOF | oc apply -f -
apiVersion: hive.openshift.io/v1
kind: ClusterDeployment
metadata:
  name: some-other-cluster
  namespace: spoke-cluster
spec:
  baseDomain: redhat.com
  installed: true
  clusterMetadata:
      adminKubeconfigSecretRef:
        name: some-other-cluster-admin-kubeconfig
      clusterID: ""
      infraID: ""
  clusterInstallRef:
    group: extensions.hive.openshift.io
    kind: AgentClusterInstall
    name: some-other-cluster-install
    version: v1beta1
  clusterName: some-other-cluster
  platform:
    agentBareMetal:
  pullSecretRef:
    name: pull-secret
EOF

```

#### 6: Add an infraenv.

If using late binding, there is no need to add a clusterRef at this stage, otherwise it should be added here.

```
cat << EOF | oc apply -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: some-other-infraenv
  namespace: spoke-cluster
spec:
  clusterRef:
    name: some-other-cluster
    namespace: spoke-cluster
  pullSecretRef:
    name: pull-secret
  sshAuthorizedKey: "..." # Optional but it can be handy to be able to log into nodes to troubleshoot.
EOF
```

#### 7: If all done correctly, an ISO download URL should quickly become available, dowload the ISO using this.

```
oc get infraenv -n spoke-cluster some-other-infraenv -ojson | jq ".status.isoDownloadURL" --raw-output | xargs curl -k -o /storage0/isos/some-other.iso
```

### Adding a Day 2 worker to the cluster:

#### 1: Boot the machine that will be used as a worker from the ISO.

Ensure that the node being used for this meets the requirements for an Openshift worker node.

#### 2: Wait for an agent to register.

```
watch -n 5 "oc get agent -n spoke-cluster"
```

If agent registration is succesful, after a short time, you should see an agent listed, this agent will need to be approved for installation.
This can take a few minutes to show up.

If for any reason this step does not work, try logging into the booted node to see if the nature of the problem may be determined.

_(use ctrl-c to exit the watch command once the node shows up)_

#### 3: Make sure any pending unbound agents are associated with the cluster. (this step is only required for late binding)

```
oc get agent -n spoke-cluster -ojson | jq -r '.items[] | select(.spec.approved==false) |select(.spec.clusterDeploymentName==null) | .metadata.name'| xargs oc -n spoke-cluster patch -p '{"spec":{"clusterDeploymentName":{"name":"some-other-cluster","namespace":"spoke-cluster"}}}' --type merge agent
```

#### 4: Approve any pending agents for installation.

```
oc get agent -n spoke-cluster -ojson | jq -r '.items[] | select(.spec.approved==false) | .metadata.name'| xargs oc -n spoke-cluster patch -p '{"spec":{"approved":true}}' --type merge agent
```

#### 5 Await the installation of the worker. 

On completion of node installation, the worker node should contact the spoke cluster with a Certificate Signing Request to begin the joining process. The CSRs should be automatically signed after a short while.

