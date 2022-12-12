# Import Installed Cluster

It is possible to import an existing installed OpenShift in order to be able to add more workers to it. (Day 2 operation)

## CRDs

Here are the needed CRDs to import an existing cluster:

### InfraEnv

- [InfraEnv](crds/infraEnv.yaml) or
- [InfraEnv Late Binding](crds/infraEnvLateBinding.yaml)

### NMState Config

Optional network configuration.

- [NMState Config](crds/nmstate.yaml)

### Pull Secret

- [PullSecret Secret](crds/pullsecret.yaml)

### Cluster Image Set

- [ClusterImageSet](crds/clusterImageSet.yaml)

### AgentClusterInstall

Configure the number of control planes in the `provisionRequirements` field to be greater than `1`. This is needed because adding additional nodes to a Single Node OpenShift is not currently supported.

- [AgentClusterInstall](crds/agentClusterInstall.yaml)

### ClusterDeployment

The differences from the Day 1 ClusterDeployment [example](crds/clusterDeployment.yaml) are the `installed` field that is set to `true` and the `clusterMetadata` section as described below:

- The `adminKubeconfigSecretRef` is a reference to a secret containing the `kubeconfig`. Here is how to create it (replace `/path/to/kubeconfig` to the location of the kubeconfig file):

```bash
kubectl -n spoke-cluster create secret generic test-cluster-admin-kubeconfig --from-file=kubeconfig=/path/to/kubeconfig
```

- The `adminPasswordSecretRef` is a reference to a secret containing the admin user name and password. Here is how to create it (replace `supersecretname` and `supersecretpassword` to the actual credentials):

```bash
kubectl -n spoke-cluster create secret generic test-cluster-admin-password --from-literal=username=supersecretname --from-literal=password=supersecretpassword
```

- The `infraID` is obtained from `oc get infrastructure cluster -o json | jq .status.infrastructureName`

- The `clusterID` is obtained from `oc get clusterversion version -o json | jq .spec.clusterID`

Example:

```yaml
apiVersion: hive.openshift.io/v1
kind: ClusterDeployment
metadata:
  name: test-cluster
  namespace: spoke-cluster
spec:
  baseDomain: hive.example.com
  clusterInstallRef:
    group: extensions.hive.openshift.io
    kind: AgentClusterInstall
    name: test-agent-cluster-install
    version: v1beta1
  clusterMetadata:
    adminKubeconfigSecretRef:
      name: test-cluster-admin-kubeconfig
    adminPasswordSecretRef:
      name: test-cluster-admin-password
    clusterID: e4affb96-7382-406f-80d6-af01ea9a2c0d
    infraID: da41b06f-5b27-416b-a06c-90f83d316516
  clusterName: test-cluster
  installed: true
  platform:
    agentBareMetal:
      agentSelector:
        matchLabels:
          bla: aaa
  pullSecretRef:
    name: pull-secret
```
