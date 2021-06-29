#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/../utils.sh

export SPOKE_NAMESPACE="${SPOKE_NAMESPACE:-assisted-spoke-cluster}"
export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-assisted-test-cluster}"
export ASSISTED_OPENSHIFT_VERSION="${ASSISTED_OPENSHIFT_VERSION:-openshift-v4.8.0}"
export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE="${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-quay.io/openshift-release-dev/ocp-release:4.8.0-fc.3-x86_64}"
export ASSISTED_CLUSTER_DEPLOYMENT_NAME="${ASSISTED_CLUSTER_DEPLOYMENT_NAME:-assisted-test-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"
export ASSISTED_PULLSECRET_NAME="${ASSISTED_PULLSECRET_NAME:-assisted-pull-secret}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-/home/test/dev-scripts/pull_secret.json}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export CONTROL_PLANE_COUNT="${CONTROL_PLANE_COUNT:-1}"

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
fi

# TODO: make SSH public key configurable

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

echo "Extract information about extra worker nodes..."
config=$(jq --raw-output '.[] | .name + " " + .ports[0].address + " " + .driver_info.username + " " + .driver_info.password + " " + .driver_info.address' "${EXTRA_BAREMETALHOSTS_FILE}")
IFS=" " read BMH_NAME MAC_ADDRESS username password ADDRESS <<<"${config}"
ENCODED_USERNAME=$(echo -n "${username}" | base64)
ENCODED_PASSWORD=$(echo -n "${password}" | base64)

echo "Running Ansible playbook to create kubernetes objects"
export BMH_NAME MAC_ADDRESS ENCODED_USERNAME ENCODED_PASSWORD ADDRESS
ansible-playbook "${__dir}/assisted-installer-crds-playbook.yaml"

oc create namespace "${SPOKE_NAMESPACE}"

oc get secret "${ASSISTED_PULLSECRET_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc create secret generic "${ASSISTED_PULLSECRET_NAME}" --from-file=.dockerconfigjson="${ASSISTED_PULLSECRET_JSON}" --type=kubernetes.io/dockerconfigjson -n "${SPOKE_NAMESPACE}"
oc get secret "${ASSISTED_PRIVATEKEY_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc create secret generic "${ASSISTED_PRIVATEKEY_NAME}" --from-file=ssh-privatekey=/root/.ssh/id_rsa --type=kubernetes.io/ssh-auth -n "${SPOKE_NAMESPACE}"

for manifest in $(find ${__dir}/generated -type f); do
    tee < "${manifest}" >(oc apply -f -)
done

echo "Waiting until at least ${CONTROL_PLANE_COUNT} agents are available..."
export -f wait_for_object_amount
timeout 10m bash -c "wait_for_object_amount agent ${CONTROL_PLANE_COUNT} 10 ${SPOKE_NAMESPACE}"
echo "All ${CONTROL_PLANE_COUNT} agents have joined!"

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Completed" "60m" "${SPOKE_NAMESPACE}"
echo "Cluster has been installed successfully!"

wait_for_boolean_field "clusterdeployment/${ASSISTED_CLUSTER_DEPLOYMENT_NAME}" spec.installed "${SPOKE_NAMESPACE}"
echo "Hive acknowledged cluster installation!"
