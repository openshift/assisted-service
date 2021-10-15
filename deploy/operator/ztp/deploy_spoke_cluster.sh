#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

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

if [[ "${IP_STACK}" == "v4" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V4}"
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
if [ ${SPOKE_CONTROLPLANE_AGENTS} -ne 1 ]; then
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
export -f wait_for_object_amount
timeout 20m bash -c "wait_for_object_amount agent ${SPOKE_CONTROLPLANE_AGENTS} 30 ${SPOKE_NAMESPACE}"
echo "All ${SPOKE_CONTROLPLANE_AGENTS} agents have been discovered!"

if [[ "${ASSISTED_STOP_AFTER_AGENT_DISCOVERY}" == "true" ]]; then
    echo "Agents have been discovered, do not wait for the cluster installtion to finish."
    exit
fi

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Stopped" "90m" "${SPOKE_NAMESPACE}"
echo "Cluster installation has been stopped (either for good or bad reasons)"

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Completed" "1m" "${SPOKE_NAMESPACE}"
echo "Cluster has been installed successfully!"

wait_for_boolean_field "clusterdeployment/${ASSISTED_CLUSTER_DEPLOYMENT_NAME}" spec.installed "${SPOKE_NAMESPACE}"
echo "Hive acknowledged cluster installation!"
