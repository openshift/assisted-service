#!/usr/bin/env bash

set -x

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${__dir}/../common.sh"
source "${__dir}/../utils.sh"

export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"

export DAY2_LATE_BINDING=${DAY2_LATE_BINDING:-}
export DAY2_MASTERS=${DAY2_MASTERS:-}

# If performing late binding then we need to generate an infraenv for this.
# Generation is handled within "add-remote-nodes-playbook"
if [[ "${DAY2_LATE_BINDING}" != "" ]]; then
  export LATE_BINDING_ASSISTED_CLUSTER_DEPLOYMENT_NAME=${ASSISTED_CLUSTER_DEPLOYMENT_NAME}
  export ASSISTED_CLUSTER_DEPLOYMENT_NAME=""
  export ASSISTED_INFRAENV_NAME=${ASSISTED_INFRAENV_NAME}-latebinding
fi

echo "Adding remote nodes to spoke cluster"
ansible-playbook "${__dir}/add-remote-nodes-playbook.yaml"

comma_sep_host_names=$(jq -r '[.[].name] | join(",")' "${REMOTE_BAREMETALHOSTS_FILE}")
export comma_sep_host_names

if [ -z "${comma_sep_host_names}" ] ; then
  echo "Missing bmhs names"
  exit 1
fi

export -f wait_for_cmd_amount

node_count=$(jq -r '[.[].name] | length' "${REMOTE_BAREMETALHOSTS_FILE}")

timeout 20m bash -c "wait_for_cmd_amount ${node_count} 30 remote_agents  ${SPOKE_NAMESPACE} ${comma_sep_host_names}"
echo "Remote worker agents were discovered!"

# If we are performing late binding then we to bind the discovered agents by setting their clusterRef
if [[ "${DAY2_LATE_BINDING}" != "" ]]; then
  clusterDeploymentName=${LATE_BINDING_ASSISTED_CLUSTER_DEPLOYMENT_NAME}

  # Generate a patch to assign the correct cluster name.
  agentPatch=$(cat <<PATCH
  {
    "spec":
    {
      "clusterDeploymentName":{
          "name":"${clusterDeploymentName}",
          "namespace":"${SPOKE_NAMESPACE}"
      }
    }
  }
PATCH
  )

  # Apply this patch to our Day2 agents.
  for agentHostName in $(cat "${REMOTE_BAREMETALHOSTS_FILE}"  | jq '.[].name' --raw-output) ; do
    agentName=$(oc get agents -n ${SPOKE_NAMESPACE} -ojson | jq ".items[] | select(.spec.hostname==\"${agentHostName}\").metadata.name" --raw-output)
    oc patch agent -n ${SPOKE_NAMESPACE} ${agentName} -p "${agentPatch}" --type=merge
  done

fi

timeout 60m bash -c "wait_for_cmd_amount ${node_count} 30 installed_remote_agents  ${SPOKE_NAMESPACE} ${comma_sep_host_names}"

echo "Remote worker agents installation completed successfully!"
