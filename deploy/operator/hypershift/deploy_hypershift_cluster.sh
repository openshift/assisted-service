#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

set -x

export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-spoke-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export HYPERSHIFT_AGENT_NS="${HYPERSHIFT_AGENT_NS:-assisted}"
export HYPERSHIFT_IMAGE="${HYPERSHIFT_IMAGE:-quay.io/hypershift/hypershift-operator:4.11}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"
export ASSISTED_PULLSECRET_NAME="${ASSISTED_PULLSECRET_NAME:-assisted-pull-secret}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-${PULL_SECRET_FILE}}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export SPOKE_KUBECONFIG_SECRET="${SPOKE_KUBECONFIG_SECRET:-${ASSISTED_CLUSTER_NAME}-kubeconfig}"
export SPOKE_CONTROLPLANE_AGENTS="${SPOKE_CONTROLPLANE_AGENTS:-1}"
export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V4}"
export SERVICE_SUBNET="${SERVICE_SUBNET_V4}"
export PUBLIC_CONTAINER_REGISTRIES="${PUBLIC_CONTAINER_REGISTRIES:-quay.io}"
export EXTRA_HYPERSHIFT_INSTALL_FLAGS="${EXTRA_HYPERSHIFT_INSTALL_FLAGS}"

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

playbooks_dir=${__dir}/playbooks

# Hypershift CLI needs access to the kubeconfig, pull-secret and public SSH key
function hypershift() {
  podman run -it --net host --rm --entrypoint /usr/bin/hypershift \
    -v $KUBECONFIG:/root/.kube/config -v $ASSISTED_PULLSECRET_JSON:/root/pull-secret.json \
    -v /root/.ssh/id_rsa.pub:/root/.ssh/id_rsa.pub $HYPERSHIFT_IMAGE "$@"
}

# Get agents on spoke cluster
function get_agents() {
  oc --kubeconfig $SPOKE_KUBECONFIG get agent -n $SPOKE_NAMESPACE --no-headers
}

# Ensure namespaces exist
oc get namespace "${SPOKE_NAMESPACE}" || oc create namespace "${SPOKE_NAMESPACE}"
oc get namespace "${HYPERSHIFT_AGENT_NS}" || oc create namespace "${HYPERSHIFT_AGENT_NS}"

# We need a storage for etcd of the hosted cluster
oc patch storageclass assisted-service -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'

echo "Installing HyperShift using upstream image"
hypershift install --hypershift-image $HYPERSHIFT_IMAGE --namespace hypershift $EXTRA_HYPERSHIFT_INSTALL_FLAGS
wait_for_pods "hypershift"

echo "Creating HostedCluster"
oc get hostedcluster ${ASSISTED_CLUSTER_NAME} -n ${HYPERSHIFT_AGENT_NS} || \
    hypershift create cluster agent --name $ASSISTED_CLUSTER_NAME --base-domain redhat.example --pull-secret /root/pull-secret.json \
        --ssh-key /root/.ssh/id_rsa.pub --agent-namespace $HYPERSHIFT_AGENT_NS --namespace $HYPERSHIFT_AGENT_NS

echo "Wait for a running hypershift cluster with no worker nodes"
wait_for_pods "$SPOKE_NAMESPACE"
wait_for_condition "nodepool/$ASSISTED_CLUSTER_NAME" "Ready" "10m" "$HYPERSHIFT_AGENT_NS"
wait_for_condition "hostedcluster/$ASSISTED_CLUSTER_NAME" "Available" "10m" "$HYPERSHIFT_AGENT_NS"

echo "Extract spoke kubeconfig"
oc extract -n $HYPERSHIFT_AGENT_NS secret/$ASSISTED_CLUSTER_NAME-admin-kubeconfig --to=- > /tmp/$ASSISTED_CLUSTER_NAME-kubeconfig
export SPOKE_KUBECONFIG=/tmp/$SPOKE_KUBECONFIG_SECRET

echo "Create spoke kubeconfig secret"
oc get secret "${SPOKE_KUBECONFIG_SECRET}" -n "${SPOKE_NAMESPACE}" || \
    oc create secret generic $SPOKE_KUBECONFIG_SECRET --from-file=kubeconfig=$SPOKE_KUBECONFIG -n $SPOKE_NAMESPACE

echo "Apply hive CRDs"
oc --kubeconfig $SPOKE_KUBECONFIG apply -f ${__root}/hack/crds

echo "Apply HypershiftAgentServiceConfig on hub"
ansible-playbook "${playbooks_dir}/hasc-playbook.yaml"
oc apply -f ${playbooks_dir}/generated/hasc.yaml -n $SPOKE_NAMESPACE
wait_for_condition "hypershiftagentserviceconfigs/hypershift-agent" "DeploymentsHealthy" "20m" "$SPOKE_NAMESPACE"

echo "Create assisted secrets"
oc --kubeconfig $SPOKE_KUBECONFIG get secret "${ASSISTED_PULLSECRET_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc --kubeconfig $SPOKE_KUBECONFIG create secret generic "${ASSISTED_PULLSECRET_NAME}" --from-file=.dockerconfigjson="${ASSISTED_PULLSECRET_JSON}" --type=kubernetes.io/dockerconfigjson -n "${SPOKE_NAMESPACE}"
oc --kubeconfig $SPOKE_KUBECONFIG get secret "${ASSISTED_PRIVATEKEY_NAME}" -n "${SPOKE_NAMESPACE}" || \
    oc --kubeconfig $SPOKE_KUBECONFIG create secret generic "${ASSISTED_PRIVATEKEY_NAME}" --from-file=ssh-privatekey=/root/.ssh/id_rsa --type=kubernetes.io/ssh-auth -n "${SPOKE_NAMESPACE}"

echo "Apply spoke CRs"
ansible-playbook "${playbooks_dir}/spoke-crs-playbook.yaml"
oc --kubeconfig $SPOKE_KUBECONFIG apply -f ${playbooks_dir}/generated/clusterImageSet.yaml -n $SPOKE_NAMESPACE
oc --kubeconfig $SPOKE_KUBECONFIG apply -f ${playbooks_dir}/generated/clusterDeployment.yaml -n $SPOKE_NAMESPACE
oc --kubeconfig $SPOKE_KUBECONFIG apply -f ${playbooks_dir}/generated/agentClusterInstall.yaml -n $SPOKE_NAMESPACE
oc --kubeconfig $SPOKE_KUBECONFIG apply -f ${playbooks_dir}/generated/infraEnv.yaml -n $SPOKE_NAMESPACE

echo "Wait for InfraEnv ImageCreated"
KUBECONFIG=$SPOKE_KUBECONFIG wait_for_condition "infraenv/${ASSISTED_INFRAENV_NAME}" "ImageCreated" "5m" "${SPOKE_NAMESPACE}"
export ISO_DOWNLOAD_URL=$(oc get --kubeconfig $SPOKE_KUBECONFIG -n $SPOKE_NAMESPACE infraenv $ASSISTED_INFRAENV_NAME -o jsonpath='{.status.isoDownloadURL}')

echo "Apply BareMetalHost on hub"
ansible-playbook "${playbooks_dir}/bmh-playbook.yaml"
oc apply -f ${playbooks_dir}/generated/baremetalHost.yaml -n $SPOKE_NAMESPACE

echo "Waiting until an agent is available"
export -f wait_for_cmd_amount
export -f get_agents
timeout --signal=SIGKILL 20m bash -c "wait_for_cmd_amount 1 30 get_agents" || \
    (echo "Timed-out waiting for agents to be ready" && exit 124)
agent_name=$(oc --kubeconfig $SPOKE_KUBECONFIG get -n ${SPOKE_NAMESPACE} agent -ojson | jq -r '.items[] | .metadata.name')
oc --kubeconfig $SPOKE_KUBECONFIG -n $SPOKE_NAMESPACE patch agent $agent_name -p '{"spec":{"approved":true}}' --type merge


echo "Waiting until cluster is installed"

KUBECONFIG=$SPOKE_KUBECONFIG wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Stopped" "90m" "${SPOKE_NAMESPACE}"
echo "Cluster installation has been stopped (either for good or bad reasons)"

KUBECONFIG=$SPOKE_KUBECONFIG wait_for_condition "agentclusterinstall/${ASSISTED_AGENT_CLUSTER_INSTALL_NAME}" "Completed" "90m" "${SPOKE_NAMESPACE}"
echo "Cluster has been installed successfully!"


# Test webhooks

function validate_by_pattern() {
  log="$1"
  pattern="$2"
  success_msg="$3"
  failure_msg="$4"

  if [[ $log == *$pattern* ]];
  then
    echo "SUCCESS:" $success_msg
  else
    echo "FAILURE:" $failure_msg
    exit 1
  fi
}
export -f validate_by_pattern

echo "Test webhooks for InfraEnv successful update"
oc --kubeconfig $SPOKE_KUBECONFIG -n $SPOKE_NAMESPACE patch infraenv $ASSISTED_INFRAENV_NAME \
    -p '{"metadata": {"annotations":{"foo":"bar"}}}' --type merge
webhook_log=$(oc logs -n $SPOKE_NAMESPACE -l app=agentinstalladmission --since=5s | grep -E 'UPDATE(.)*infraenvs' | tail -1)
validate_by_pattern "$webhook_log" "Successful validation" \
    "Webhook successful validation for InfraEnv update" \
    "Missing webhook validation"

echo "Test webhooks for InfraEnv failing update"
error_msg=$(oc --kubeconfig $SPOKE_KUBECONFIG -n $SPOKE_NAMESPACE patch infraenv  $ASSISTED_INFRAENV_NAME \
    -p '{"spec": {"clusterRef":{"name":"test"}}}' --type merge 2>&1) || true
validate_by_pattern "$error_msg" "Attempted to change Spec.ClusterRef which is immutable" \
    "Webhook failed validation for InfraEnv invalid update" \
    "Missing webhook validation"
