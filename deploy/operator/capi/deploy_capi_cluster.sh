#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

set -x

export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-assisted-test-cluster}"
export ASSISTED_CLUSTER_DEPLOYMENT_NAME="${ASSISTED_CLUSTER_DEPLOYMENT_NAME:-assisted-test-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"
export ASSISTED_PULLSECRET_NAME="${ASSISTED_PULLSECRET_NAME:-assisted-pull-secret}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-${PULL_SECRET_FILE}}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export SPOKE_CONTROLPLANE_AGENTS="${SPOKE_CONTROLPLANE_AGENTS:-1}"
export SPOKE_API_VIP="${SPOKE_API_VIP:-}"
export SPOKE_INGRESS_VIP="${SPOKE_INGRESS_VIP:-}"
export ASSISTED_STOP_AFTER_AGENT_DISCOVERY="${ASSISTED_STOP_AFTER_AGENT_DISCOVERY:-false}"
export ASSISTED_UPGRADE_OPERATOR="${ASSISTED_UPGRADE_OPERATOR:-false}"
export SPAWN_NONE_PLATFORM_LOAD_BALANCER="${SPAWN_NONE_PLATFORM_LOAD_BALANCER:-false}"
export ADD_NONE_PLATFORM_LIBVIRT_DNS="${ADD_NONE_PLATFORM_LIBVIRT_DNS:-false}"
export LIBVIRT_NONE_PLATFORM_NETWORK="${LIBVIRT_NONE_PLATFORM_NETWORK:-ostestbm}"
export LOAD_BALANCER_IP="${LOAD_BALANCER_IP:-192.168.111.1}"
export HYPERSHIFT_IMAGE="${HYPERSHIFT_IMAGE:-quay.io/hypershift/hypershift-operator:latest}"
export PROVIDER_IMAGE="${PROVIDER_IMAGE:-}"

if [[ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ]]; then
    export USER_MANAGED_NETWORKING="true"
else
    export USER_MANAGED_NETWORKING="${USER_MANAGED_NETWORKING:-false}"
fi

if [[ "${IP_STACK}" == "v4" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    if [ "${USER_MANAGED_NETWORKING}" != "true" ] || [ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ] ; then
        export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V4}"
    else
        unset EXTERNAL_SUBNET
    fi
    export SERVICE_SUBNET="${SERVICE_SUBNET_V4}"
elif [[ "${IP_STACK}" == "v6" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V6}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V6}"
    export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V6}"
    export SERVICE_SUBNET="${SERVICE_SUBNET_V6}"
elif [[ "${IP_STACK}" == "v4v6" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V4}"
    export SERVICE_SUBNET="${SERVICE_SUBNET_V4}"
    export CLUSTER_SUBNET_ADDITIONAL="${CLUSTER_SUBNET_V6}"
    export CLUSTER_HOST_PREFIX_ADDITIONAL="${CLUSTER_HOST_PREFIX_V6}"
    export EXTERNAL_SUBNET_ADDITIONAL="${EXTERNAL_SUBNET_V6}"
    export SERVICE_SUBNET_ADDITIONAL="${SERVICE_SUBNET_V6}"
fi

#  If spoke is a multi cluster then we need to pick IPs for API and Ingress
if [ ${SPOKE_CONTROLPLANE_AGENTS} -ne 1 ] && [ "${USER_MANAGED_NETWORKING}" != "true" ] ; then
    export SPOKE_API_VIP=${SPOKE_API_VIP:-$(nth_ip $EXTERNAL_SUBNET 85)}
    export SPOKE_INGRESS_VIP=${SPOKE_INGRESS_VIP:-$(nth_ip $EXTERNAL_SUBNET 87)}
fi

if [ "${DISCONNECTED}" = "true" ]; then
    ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE="${LOCAL_REGISTRY}/$(get_image_without_registry ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})"
fi

# TODO: make SSH public key configurable

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

echo "Running Ansible playbook to create kubernetes objects"
ansible-playbook "${__dir}/assisted-installer-crds-playbook.yaml"

oc get namespace "${SPOKE_NAMESPACE}" || oc create namespace "${SPOKE_NAMESPACE}"

oc get secret "${ASSISTED_PULLSECRET_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc create secret generic "${ASSISTED_PULLSECRET_NAME}" --from-file=.dockerconfigjson="${ASSISTED_PULLSECRET_JSON}" --type=kubernetes.io/dockerconfigjson -n "${SPOKE_NAMESPACE}"
oc get secret "${ASSISTED_PRIVATEKEY_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc create secret generic "${ASSISTED_PRIVATEKEY_NAME}" --from-file=ssh-privatekey=/root/.ssh/id_rsa --type=kubernetes.io/ssh-auth -n "${SPOKE_NAMESPACE}"

for manifest in $(find ${__dir}/generated -type f); do
    tee < "${manifest}" >(oc apply -f -)
done

wait_for_condition "infraenv/${ASSISTED_INFRAENV_NAME}" "ImageCreated" "5m" "${SPOKE_NAMESPACE}"

echo "Waiting until at least ${SPOKE_CONTROLPLANE_AGENTS} agents are available..."

function get_agents() {
  oc get agent -n ${SPOKE_NAMESPACE} --no-headers
}

export -f wait_for_cmd_amount
export -f get_agents
timeout 20m bash -c "wait_for_cmd_amount ${SPOKE_CONTROLPLANE_AGENTS} 30 get_agents"
echo "All ${SPOKE_CONTROLPLANE_AGENTS} agents have been discovered!"

if [[ "${ASSISTED_STOP_AFTER_AGENT_DISCOVERY}" == "true" ]]; then
    echo "Agents have been discovered, do not wait for the cluster installtion to finish."
    exit
fi

# We need a storage for etcd of the hosted cluster
oc patch storageclass assisted-service -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'

### Hypershift CLI needs access to the kubeconfig, pull-secret and public SSH key
function hypershift() {
  podman run -it --net host --rm --entrypoint /usr/bin/hypershift -v $KUBECONFIG:/root/.kube/config -v $ASSISTED_PULLSECRET_JSON:/root/pull-secret.json -v /root/.ssh/id_rsa.pub:/root/.ssh/id_rsa.pub $HYPERSHIFT_IMAGE "$@"
}

echo "Installing HyperShift using upstream image"
hypershift install --hypershift-image $HYPERSHIFT_IMAGE --namespace hypershift
wait_for_pods "hypershift"

if [ -z "$PROVIDER_IMAGE" ]
then
  echo "PROVIDER_IMAGE override not set"
  export PROVIDER_FLAG_FOR_CREATE_COMMAND=""
else
  echo "PROVIDER_IMAGE override: $PROVIDER_IMAGE"
  export PROVIDER_FLAG_FOR_CREATE_COMMAND=" --annotations hypershift.openshift.io/capi-provider-agent-image=$PROVIDER_IMAGE"
fi

# Since the default registry.ci.openshift.org/hypershift/cluster-api:v1.0.0 image no longer exists, we get the CAPI image from the release
CLUSTER_API_IMAGE=oc adm release info --image-for=cluster-capi-controllers ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-${RELEASE_IMAGE}}

echo "Creating HostedCluster"
hypershift create cluster agent --name $ASSISTED_CLUSTER_NAME --base-domain redhat.example --pull-secret /root/pull-secret.json \
 --ssh-key /root/.ssh/id_rsa.pub --agent-namespace $SPOKE_NAMESPACE --namespace $SPOKE_NAMESPACE \
 --control-plane-operator-image $HYPERSHIFT_IMAGE \
 --release-image ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-${RELEASE_IMAGE}} \
  --annotations hypershift.openshift.io/capi-manager-image=$CLUSTER_API_IMAGE" \
  $PROVIDER_FLAG_FOR_CREATE_COMMAND

# Wait for a running hypershift cluster with no worker nodes
wait_for_pods "$SPOKE_NAMESPACE-$ASSISTED_CLUSTER_NAME"
wait_for_condition "nodepool/$ASSISTED_CLUSTER_NAME" "Ready" "10m" "$SPOKE_NAMESPACE"
wait_for_condition "hostedcluster/$ASSISTED_CLUSTER_NAME" "Available" "10m" "$SPOKE_NAMESPACE"

# Scale up
echo "Scaling the hosted cluster up to contain ${SPOKE_CONTROLPLANE_AGENTS} worker nodes"
oc scale nodepool/$ASSISTED_CLUSTER_NAME -n $SPOKE_NAMESPACE --replicas=${SPOKE_CONTROLPLANE_AGENTS}

# Wait for node to appear in the CAPI-deployed cluster
oc extract -n $SPOKE_NAMESPACE secret/$ASSISTED_CLUSTER_NAME-admin-kubeconfig --to=- > /tmp/$ASSISTED_CLUSTER_NAME-kubeconfig
export KUBECONFIG=/tmp/$ASSISTED_CLUSTER_NAME-kubeconfig

wait_for_object_amount node ${SPOKE_CONTROLPLANE_AGENTS} 10
echo "Worker nodes have been detected successfuly in the created cluster!"
