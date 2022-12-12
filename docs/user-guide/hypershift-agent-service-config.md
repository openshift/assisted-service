# Hypershift zero worker deployment

## Prerequisites

1. Deploy hub cluster. For development setup you can use [dev-scripts](<(https://github.com/javipolo/openshift-assisted-installer-tests/tree/4515edfc9d60a587cabd3bcfdd77826cab8cc02d/ai-in-zero-worker-hypershift#deploy-an-l0-hub-cluster-using-dev-scripts)>)
2. Hive [installation](https://github.com/javipolo/openshift-assisted-installer-tests/tree/4515edfc9d60a587cabd3bcfdd77826cab8cc02d/ai-in-zero-worker-hypershift#install-hypershift)
3. Create [hypershift cluster](https://github.com/javipolo/openshift-assisted-installer-tests/tree/4515edfc9d60a587cabd3bcfdd77826cab8cc02d/ai-in-zero-worker-hypershift#create-hypershift-cluster)

## Deploying Hyperhisft on the hub cluster

### Set up our environment

```bash
AGENT_NAMESPACE=myclusters
CLUSTERNAME=acm-1
CLUSTER_NAMESPACE=${AGENT_NAMESPACE}-${CLUSTERNAME}
BASEDOMAIN=redhat.com
SSHKEY=~/.ssh/id_rsa.pub
```

### Deploy Hypershift

```bash
export HYPERSHIFT_IMAGE=quay.io/hypershift/hypershift-operator:4.11

alias hypershift="podman run --net host --rm --entrypoint /usr/bin/hypershift -e KUBECONFIG=/credentials/kubeconfig -v /root/.ssh:/root/.ssh -v /tmp/kubeconfig:/credentials/kubeconfig -v /tmp/pull-secret.json:/tmp/pull-secret.json $HYPERSHIFT_IMAGE"

hypershift install --hypershift-image $HYPERSHIFT_IMAGE
oc -n hypershift get pods -w
```

Also refer to the official documentation of the hypershift project at https://hypershift-docs.netlify.app/how-to/agent/create-agent-cluster/.

### Deploy Hyperhisft cluster

```bash
# Create namespace
oc create namespace $AGENT_NAMESPACE

# Create hypershift cluster
hypershift create cluster agent --name $CLUSTER_NAMESPACE --base-domain $BASEDOMAIN --pull-secret /tmp/pull-secret.json  --ssh-key $SSHKEY --agent-namespace $AGENT_NAMESPACE --namespace $AGENT_NAMESPACE

# Wait for hypershift cluster (wait for available)
oc get po -n $CLUSTER_NAMESPACE -w
oc get hostedclusters -A -w

# Generate kubeconfig for the generated spoke (hypershift) cluster
hypershift create kubeconfig --name $CLUSTER_NAMESPACE --namespace $AGENT_NAMESPACE | sed s/admin/$CLUSTER_NAMESPACE/g > /tmp/kubeconfig.$CLUSTER_NAMESPACE

# save the generated kubeconfig in a secret on the hub cluster
oc -n $CLUSTER_NAMESPACE create secret generic $CLUSTER_NAMESPACE-kubeconfig --from-file=kubeconfig=/tmp/kubeconfig.$CLUSTER_NAMESPACE
```

## Install CRDS

```bash
# install hive CRDs on the hub cluster
oc apply -f hive.openshift.io_clusterdeployments.yaml
oc apply -f hive.openshift.io_clusterimagesets.yaml
oc apply -f metal3.io_baremetalhosts.yaml

# install hive CRDs on the spoke cluster
oc --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE apply -f hive.openshift.io_clusterdeployments.yaml
oc --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE apply -f hive.openshift.io_clusterimagesets.yaml
oc --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE apply -f metal3.io_baremetalhosts.yaml
```

## Infrastructure Operator

### Install the operator on the hub cluster

OLM is available by default on OCP installations. However, for development environment please refer to
[OLM's official installation guide](https://github.com/operator-framework/operator-lifecycle-manager/blob/master/doc/install/install.md#openshift) or use the following installation shell script:

```bash
# download the installation script (change versions as they progress)
https://github.com/operator-framework/operator-lifecycle-manager/releases/download/v0.22.0/install.sh -o install.sh

# run the installation script
chmod +x install.sh
./install.sh v0.22.0

# install assisted infrastructure operator
oc create namespace $CLUSTER_NAMESPACE
operator-sdk run bundle -n assisted-installer $BUNDLE_IMAGE
```

### Define HypershiftAgentServiceConfig

```bash
cat <<EOF | kubectl create -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: HypershiftAgentServiceConfig
metadata:
 name: agent-$CLUSTER_NAMESPACE
 namespace: $CLUSTER_NAMESPACE
spec:
  KubeconfigSecretRef:
    name: $CLUSTER_NAMESPACE-kubeconfig
  databaseStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  filesystemStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 20Gi
  imageStorage:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 10Gi
  osImages:
    - cpuArchitecture: x86_64
      openshiftVersion: '4.11'
      url: https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.0/rhcos-4.11.0-x86_64-live.x86_64.iso
      version: 411.86.202207150124-0
EOF
```

Note that the secret this CR refers to was generated early on in the `deploy hyperhisft` step. In production environment this secret will be deployed by ACM. For more details on the other parameters please refer to the [Infrastructure operator documentation](https://github.com/openshift/assisted-service/blob/master/docs/operator.md).

# Sanity test

## Create an infraenv on the spoke cluster

```bash
oc create --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE ns test

# create pull secret
oc create --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE secret generic pull-secret -n test --from-file=.dockerconfigjson=pull-secret.json --type=kubernetes.io/dockerconfigjson

# create infraenv
cat << EOF | oc apply --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE -f -
apiVersion: agent-install.openshift.io/v1beta1
kind: InfraEnv
metadata:
  name: test
  namespace: test
spec:
  pullSecretRef:
    name: pull-secret
EOF

# Check that discovery ISO is generated and downloadable
oc get infraenv --kubeconfig /tmp/kubeconfig.$CLUSTER_NAMESPACE -n test test -ojsonpath={.status.isoDownloadURL} | xargs curl -kI

# Look in agentinstalladmission logs that validations have been done for new infraenv
oc logs -n $CLUSTER_NAMESPACE -l app=agentinstalladmission
```
