#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh
source ${__dir}/none_platform_utils.sh

set -x

export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-assisted-test-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-${PULL_SECRET_FILE}}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export SPOKE_CONTROLPLANE_AGENTS="${SPOKE_CONTROLPLANE_AGENTS:-1}"
export SPOKE_DUAL_STACK_VIPS="${SPOKE_DUAL_STACK_VIPS:-false}"
export SPOKE_API_VIP="${SPOKE_API_VIP:-}"
export SPOKE_API_VIP_SECONDARY="${SPOKE_API_VIP_SECONDARY:-}"
export SPOKE_INGRESS_VIP="${SPOKE_INGRESS_VIP:-}"
export SPOKE_INGRESS_VIP_SECONDARY="${SPOKE_INGRESS_VIP_SECONDARY:-}"
export ASSISTED_STOP_AFTER_AGENT_DISCOVERY="${ASSISTED_STOP_AFTER_AGENT_DISCOVERY:-false}"
export ASSISTED_UPGRADE_OPERATOR="${ASSISTED_UPGRADE_OPERATOR:-false}"
export SPAWN_NONE_PLATFORM_LOAD_BALANCER="${SPAWN_NONE_PLATFORM_LOAD_BALANCER:-false}"
export ADD_NONE_PLATFORM_LIBVIRT_DNS="${ADD_NONE_PLATFORM_LIBVIRT_DNS:-false}"
export LIBVIRT_NONE_PLATFORM_NETWORK="${LIBVIRT_NONE_PLATFORM_NETWORK:-ostestbm}"
export LOAD_BALANCER_IP="${LOAD_BALANCER_IP:-192.168.111.1}"
export API_IP=${LOAD_BALANCER_IP}

if [[ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ]]; then
    export USER_MANAGED_NETWORKING="true"
else
    export USER_MANAGED_NETWORKING="${USER_MANAGED_NETWORKING:-false}"
fi

if [[ "${IP_STACK}" == "v4" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    if [ "${USER_MANAGED_NETWORKING}" != "true" ] || [ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ]; then
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
    # If spoke is dual-stack and uses dual-stack VIPs then we need to pick IPs for IPv6 API and Ingress
    if [ "${IP_STACK}" = "v4v6" ] && [ "${SPOKE_DUAL_STACK_VIPS}" = "True" ]; then
        export SPOKE_API_VIP_SECONDARY=${SPOKE_API_VIP_SECONDARY:-$(nth_ip $EXTERNAL_SUBNET_V6 85)}
        export SPOKE_INGRESS_VIP_SECONDARY=${SPOKE_INGRESS_VIP_SECONDARY:-$(nth_ip $EXTERNAL_SUBNET_V6 87)}
    fi
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
    echo "Applying $manifest"
    cat "${manifest}"
    oc apply -f "${manifest}"
done

wait_for_condition "infraenv/${ASSISTED_INFRAENV_NAME}" "ImageCreated" "5m" "${SPOKE_NAMESPACE}"

echo "Waiting until at least ${SPOKE_CONTROLPLANE_AGENTS} agents are available..."

function get_agents_with_role() {
  oc get agent -n ${SPOKE_NAMESPACE} --no-headers | awk '{print $4}' | grep $role
}

export role=master
export -f wait_for_cmd_amount
export -f get_agents_with_role
timeout --signal=SIGKILL 20m bash -c "wait_for_cmd_amount ${SPOKE_CONTROLPLANE_AGENTS} 30 get_agents_with_role" || \
    (echo "Timed-out waiting for agents to be ready" && exit 124)

echo "All ${SPOKE_CONTROLPLANE_AGENTS} agents have been discovered!"

if [[ "${ASSISTED_STOP_AFTER_AGENT_DISCOVERY}" == "true" ]]; then
    echo "Agents have been discovered, do not wait for the cluster installation to finish."
    exit
fi

if [ ${SPOKE_CONTROLPLANE_AGENTS} -ne 1 ] && [ "${USER_MANAGED_NETWORKING}" == "true" ] ; then
    if [ "${SPAWN_NONE_PLATFORM_LOAD_BALANCER}" == "true" ] ; then
        setup_and_run_load_balancer
    fi
    if [ "${ADD_NONE_PLATFORM_LIBVIRT_DNS}" == "true" ] ; then
        setup_libvirt_dns
        open_firewall_ports
    fi
fi

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Stopped" "90m" "${SPOKE_NAMESPACE}"
echo "Cluster installation has been stopped (either for good or bad reasons)"

wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Completed" "1m" "${SPOKE_NAMESPACE}"
echo "Cluster has been installed successfully!"

wait_for_boolean_field "clusterdeployment/${ASSISTED_CLUSTER_DEPLOYMENT_NAME}" spec.installed "${SPOKE_NAMESPACE}"
echo "Hive acknowledged cluster installation!"

# For SNO we derive API IP from .status.apiVIP of the agentclusterinstall as this is the address of the single node.
#
# For multi-node without User Managed Networking we derive API IP in the same way as for SNO, because there is no dedicated
# loadbalancer deployed.
#
# For multi-node with User Managed Networking we do not do anything as this is covered just in the condition above.
if [ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ] || [ "${USER_MANAGED_NETWORKING}" == "false" ] ; then
    export API_IP=$(oc get -n ${SPOKE_NAMESPACE} agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME} -ojson | jq '.status.apiVIP' --raw-output)
    if [ -z "$API_IP" ]; then
        echo "Fatal:"
        echo "No value found in the agentclusterinstall for .status.apiVIP"
        echo "Cannot determine the address of the API"
        exit
    fi
fi

setup_libvirt_dns
open_firewall_ports
