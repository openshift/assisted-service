#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"

echo "Adding remote nodes to spoke cluster"
ansible-playbook "${__dir}/add-remote-nodes-playbook.yaml"

wait_for_condition "agentserviceconfigs/agent" "Completed" "20m" "${SPOKE_NAMESPACE}" "role=worker"
echo "Remote worker agent completed successfully!"

sleep 1h