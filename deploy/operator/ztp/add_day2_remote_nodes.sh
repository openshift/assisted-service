#!/usr/bin/env bash

set -x

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"

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

timeout 60m bash -c "wait_for_cmd_amount ${amount} 30 remote_done_agents"

echo "Remote worker agents installation completed successfully!"
