#!/usr/bin/env bash

set -x

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "${__dir}/../common.sh"
source "${__dir}/../utils.sh"

export REMOTE_BAREMETALHOSTS_FILE="${REMOTE_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/remote_baremetalhosts.json}"
export LOCAL_CLUSTER_NAMESPACE="${LOCAL_CLUSTER_NAMESPACE:-local-agent-cluster}"

echo "Adding day2 local cluster nodes"
ansible-playbook "${__dir}/add-worker-nodes-to-local-cluster-playbook.yaml"

comma_sep_host_names=$(jq -r '[.[].name] | join(",")' "${REMOTE_BAREMETALHOSTS_FILE}")
export comma_sep_host_names

if [ -z "${comma_sep_host_names}" ] ; then
  echo "Missing bmhs names"
  exit 1
fi

export -f wait_for_cmd_amount

node_count=$(jq -r '[.[].name] | length' "${REMOTE_BAREMETALHOSTS_FILE}")

timeout 20m bash -c "wait_for_cmd_amount ${node_count} 30 remote_agents ${LOCAL_CLUSTER_NAMESPACE} ${comma_sep_host_names}"
echo "Remote worker agents were discovered!"

timeout 60m bash -c "wait_for_cmd_amount ${node_count} 30 installed_remote_agents ${LOCAL_CLUSTER_NAMESPACE} ${comma_sep_host_names}"

echo "Local cluster agents installation completed successfully!"
