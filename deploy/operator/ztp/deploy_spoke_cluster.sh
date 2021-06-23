#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/../utils.sh

export ASSISTED_NAMESPACE="${ASSISTED_NAMESPACE:-assisted-installer}"
export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-assisted-test-cluster}"
export DS_OPENSHIFT_VERSION="${DS_OPENSHIFT_VERSION:-openshift-v4.8.0}"
export OPENSHIFT_INSTALL_RELEASE_IMAGE="${OPENSHIFT_INSTALL_RELEASE_IMAGE:-quay.io/openshift-release-dev/ocp-release:4.8.0-fc.3-x86_64}"
export ASSISTED_CLUSTER_DEPLOYMENT_NAME="${ASSISTED_CLUSTER_DEPLOYMENT_NAME:-assisted-test-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"
export ASSISTED_PULLSECRET_NAME="${ASSISTED_PULLSECRET_NAME:-assisted-pull-secret}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-/home/test/dev-scripts/pull_secret.json}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export CONTROL_PLANE_COUNT="${CONTROL_PLANE_COUNT:-1}"
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
ansible-playbook assisted-installer-crds-playbook.yaml

oc create secret generic ${ASSISTED_PULLSECRET_NAME} --from-file=.dockerconfigjson=${ASSISTED_PULLSECRET_JSON} --type=kubernetes.io/dockerconfigjson -n ${ASSISTED_NAMESPACE}
oc create secret generic ${ASSISTED_PRIVATEKEY_NAME} --from-file=ssh-privatekey=/root/.ssh/id_rsa --type=kubernetes.io/ssh-auth -n ${ASSISTED_NAMESPACE}

oc create -f generated/clusterImageSet.yaml
oc create -f generated/clusterDeployment.yaml
oc create -f generated/infraEnv.yaml
oc create -f generated/agentClusterInstall.yaml
oc create -f generated/baremetalHost.yaml

echo "Waiting until at least ${CONTROL_PLANE_COUNT} agents are available..."
export -f wait_for_object_amount
timeout 10m bash -c "wait_for_object_amount agent ${CONTROL_PLANE_COUNT} 10 ${ASSISTED_NAMESPACE}"
echo "All ${CONTROL_PLANE_COUNT} agents have joined!"

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Completed" "60m" "${ASSISTED_NAMESPACE}"
echo "Cluster has been installed successfully!"
