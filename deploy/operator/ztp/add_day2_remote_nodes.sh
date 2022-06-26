#!/usr/bin/env bash

set -x

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"

export DAY2_LATE_BINDING=${DAY2_LATE_BINDING:-}

# If performing late binding then we need to generate an infraenv for this.
# Generation is handled within "add-remote-nodes-playbook"
if [[ "${DAY2_LATE_BINDING}" != "" ]]; then
  export LATE_BINDING_ASSISTED_CLUSTER_DEPLOYMENT_NAME=${ASSISTED_CLUSTER_DEPLOYMENT_NAME}
  export ASSISTED_CLUSTER_DEPLOYMENT_NAME=""
  export ASSISTED_INFRAENV_NAME=${ASSISTED_INFRAENV_NAME}-latebinding
fi

echo "Adding remote nodes to spoke cluster"
ansible-playbook "${__dir}/add-remote-nodes-playbook.yaml"

pattern=""
amount=0
for name in $(cat ${REMOTE_BAREMETALHOSTS_FILE}  | jq -rc '.[].name') ; do
  pattern="${pattern}${name},"
  let amount=amount+1
done

if [ -z "${pattern}" ] ; then
  echo "Missing bmhs names"
  exit 1
fi

pattern="${pattern::-1}"
export pattern


function remote_agents() {
	oc get agent -n ${SPOKE_NAMESPACE} --no-headers -l "agent-install.openshift.io/bmh in ( ${pattern} )"
}

export -f wait_for_cmd_amount
export -f remote_agents
timeout 20m bash -c "wait_for_cmd_amount ${amount} 30 remote_agents"
echo "Remote worker agents were discovered!"

function remote_done_agents() {
        remote_agents | grep Done
}

export -f remote_done_agents

# If we are performing late binding then each of the agents needs to have the correct clusterDeploymentRef applied.
# This needs to happen after the agents are available. They cannot move to "done" until the clusterDeploymentRef is applied.
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
    agentName=$(oc get agents -n ${SPOKE_NAMESPACE} -ojson | jq ".items[] | select(.spec.hostname==\"${agentHostName}\")).metadata.name" --raw-output)
    oc patch agent -n ${SPOKE_NAMESPACE} ${agentName} -p "${agentPatch}" --type=merge
  done

fi

timeout 60m bash -c "wait_for_cmd_amount ${amount} 30 remote_done_agents"

echo "Remote worker agents installation completed successfully!"
