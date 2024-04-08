#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../../..)"
source ${__dir}/../common.sh
source ${__dir}/../utils.sh

set -x

export ASSISTED_CLUSTER_NAME="${ASSISTED_CLUSTER_NAME:-assisted-test-cluster}"
export ASSISTED_CLUSTER_DEPLOYMENT_NAME="${ASSISTED_CLUSTER_DEPLOYMENT_NAME:-assisted-test-cluster}"
export ASSISTED_AGENT_CLUSTER_INSTALL_NAME="${ASSISTED_AGENT_CLUSTER_INSTALL_NAME:-assisted-agent-cluster-install}"
export ASSISTED_INFRAENV_NAME="${ASSISTED_INFRAENV_NAME:-assisted-infra-env}"
export ASSISTED_PULLSECRET_NAME="${ASSISTED_PULLSECRET_NAME:-assisted-pull-secret}"
export ASSISTED_PULLSECRET_JSON="${ASSISTED_PULLSECRET_JSON:-${PULL_SECRET_FILE}}"
export ASSISTED_PRIVATEKEY_NAME="${ASSISTED_PRIVATEKEY_NAME:-assisted-ssh-private-key}"
export EXTRA_BAREMETALHOSTS_FILE="${EXTRA_BAREMETALHOSTS_FILE:-/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json}"
export SPOKE_CONTROLPLANE_AGENTS="${SPOKE_CONTROLPLANE_AGENTS:-1}"
export ASSISTED_STOP_AFTER_AGENT_DISCOVERY="${ASSISTED_STOP_AFTER_AGENT_DISCOVERY:-false}"
export ASSISTED_UPGRADE_OPERATOR="${ASSISTED_UPGRADE_OPERATOR:-false}"
export SPAWN_NONE_PLATFORM_LOAD_BALANCER="${SPAWN_NONE_PLATFORM_LOAD_BALANCER:-false}"
export ADD_NONE_PLATFORM_LIBVIRT_DNS="${ADD_NONE_PLATFORM_LIBVIRT_DNS:-false}"
export LIBVIRT_NONE_PLATFORM_NETWORK="${LIBVIRT_NONE_PLATFORM_NETWORK:-ostestbm}"
export LOAD_BALANCER_IP="${LOAD_BALANCER_IP:-192.168.111.1}"
export HYPERSHIFT_IMAGE="${HYPERSHIFT_IMAGE:-quay.io/hypershift/hypershift-operator:latest}"
export CONTROL_PLANE_OPERATOR_IMAGE="${CONTROL_PLANE_OPERATOR_IMAGE:-}"
export PROVIDER_IMAGE="${PROVIDER_IMAGE:-}"
export EXTRA_HYPERSHIFT_INSTALL_FLAGS="${EXTRA_HYPERSHIFT_INSTALL_FLAGS:-}"
export EXTRA_HYPERSHIFT_CREATE_COMMANDS="${EXTRA_HYPERSHIFT_CREATE_COMMANDS:-}"
export EXTRA_HYPERSHIFT_CLI_MOUNTS="${EXTRA_HYPERSHIFT_CLI_MOUNTS:-}"

if [[ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ]]; then
    export USER_MANAGED_NETWORKING="true"
else
    export USER_MANAGED_NETWORKING="${USER_MANAGED_NETWORKING:-false}"
fi

if [[ "${IP_STACK}" == "v4" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    if [ "${USER_MANAGED_NETWORKING}" != "true" ] || [ ${SPOKE_CONTROLPLANE_AGENTS} -eq 1 ] ; then
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
    # IPv6 requires hypershift create cluster and service cidr overrides
    export EXTRA_HYPERSHIFT_CREATE_COMMANDS="$EXTRA_HYPERSHIFT_CREATE_COMMANDS --cluster-cidr fd01::/48 --service-cidr fd02::/112"
elif [[ "${IP_STACK}" == "v4v6" ]]; then
    export CLUSTER_SUBNET="${CLUSTER_SUBNET_V4}"
    export CLUSTER_HOST_PREFIX="${CLUSTER_HOST_PREFIX_V4}"
    export EXTERNAL_SUBNET="${EXTERNAL_SUBNET_V4}"
    export SERVICE_SUBNET="${SERVICE_SUBNET_V4}"
    export CLUSTER_SUBNET_ADDITIONAL="${CLUSTER_SUBNET_V6}"
    export CLUSTER_HOST_PREFIX_ADDITIONAL="${CLUSTER_HOST_PREFIX_V6}"
    export EXTERNAL_SUBNET_ADDITIONAL="${EXTERNAL_SUBNET_V6}"
    export SERVICE_SUBNET_ADDITIONAL="${SERVICE_SUBNET_V6}"
    # IPv6 requires hypershift create cluster and service cidr overrides
    export EXTRA_HYPERSHIFT_CREATE_COMMANDS="$EXTRA_HYPERSHIFT_CREATE_COMMANDS --cluster-cidr fd01::/48 --service-cidr fd02::/112"
fi

if [ "${DISCONNECTED}" = "true" ]; then
    # Disconnected hypershift requires:
    # 1. pull secret in hypershift namespace for the hypershift operator
    oc get namespace hypershift || oc create namespace hypershift
    oc get secret "${ASSISTED_PULLSECRET_NAME}" -n hypershift || \
      oc create secret generic "${ASSISTED_PULLSECRET_NAME}" --from-file=.dockerconfigjson="${ASSISTED_PULLSECRET_JSON}" --type=kubernetes.io/dockerconfigjson -n hypershift
    # 2. mirrored hypershift operator image to local registry
    HYPERSHIFT_LOCAL_IMAGE="${LOCAL_REGISTRY}/localimages/hypershift:latest"
    oc image mirror -a "${PULL_SECRET_FILE}" "${HYPERSHIFT_IMAGE}" "${HYPERSHIFT_LOCAL_IMAGE}"
    export HYPERSHIFT_IMAGE="${HYPERSHIFT_LOCAL_IMAGE}"
    export CONTROL_PLANE_OPERATOR_IMAGE=$(oc adm release info "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" --image-for hypershift)
    # 3. the hypershift cli must be available on the local environment
    #id=$(podman create $HYPERSHIFT_LOCAL_IMAGE)
    #mkdir -p ./hypershift-cli
    #podman cp $id:/usr/bin/hypershift ./hypershift-cli
    #export PATH="$PATH":"$PWD"/hypershift-cli
    # 4. mirrored capi agent image to local registry
    if [ ! -z "$PROVIDER_IMAGE" ]
    then
      export PROVIDER_LOCAL_IMAGE="${LOCAL_REGISTRY}/localimages/cluster-api-provider-agent:latest"
      oc image mirror -a "${PULL_SECRET_FILE}" "${PROVIDER_IMAGE}" "${PROVIDER_LOCAL_IMAGE}"
      export PROVIDER_IMAGE="${PROVIDER_LOCAL_IMAGE}"
    fi
    # 5. ImageContentPolicy for local mirror registry (prerequisite is the openshift release is mirrored to the local registry)
    export OCP_MIRROR_REGISTRY="${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})"
    cat << EOM >> icsp.yaml
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: example
spec:
  repositoryDigestMirrors:
  - mirrors:
    - ${OCP_MIRROR_REGISTRY}
    source: quay.io/openshift-release-dev/ocp-release
  - mirrors:
    - ${OCP_MIRROR_REGISTRY}
    source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
EOM
    oc apply -f icsp.yaml
    # 6. Image content source for hosted cluster to be passed in through the hypershift create command
  cat << EOM >> /tmp/icsp-hc.yaml
- mirrors:
  - ${OCP_MIRROR_REGISTRY}
  source: quay.io/openshift-release-dev/ocp-release
- mirrors:
  - ${OCP_MIRROR_REGISTRY}
  source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
EOM
    export EXTRA_HYPERSHIFT_CREATE_COMMANDS="$EXTRA_HYPERSHIFT_CREATE_COMMANDS --image-content-sources /tmp/icsp-hc.yaml"
    export EXTRA_HYPERSHIFT_CLI_MOUNTS="$EXTRA_HYPERSHIFT_CLI_MOUNTS -v /tmp/icsp-hc.yaml:/tmp/icsp-hc.yaml"
    # 7. Machine config operator image must be added to hypershift operator's pod's arguments
    export MCO_IMAGE=$(oc adm release info "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" --image-for machine-config-operator)
    export LOCAL_MCO_IMAGE="${OCP_MIRROR_REGISTRY}@$(oc image info $MCO_IMAGE -ojson | jq -r '.digest')"
    # the disconnected openshift release image will also be used as release-image flag for hypershift create cluster command
    export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE="${LOCAL_REGISTRY}/$(get_image_without_registry ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})"
    # disconnected requires the additional trust bundle containing the local registry certificate
    export EXTRA_HYPERSHIFT_CREATE_COMMANDS="$EXTRA_HYPERSHIFT_CREATE_COMMANDS --additional-trust-bundle /etc/pki/ca-trust/source/anchors/${REGISTRY_CRT}"
    export EXTRA_HYPERSHIFT_CLI_MOUNTS="$EXTRA_HYPERSHIFT_CLI_MOUNTS -v ${REGISTRY_DIR}/certs/${REGISTRY_CRT}:/etc/pki/ca-trust/source/anchors/${REGISTRY_CRT}"
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

function get_agents() {
  oc get agent -n ${SPOKE_NAMESPACE} --no-headers
}

export -f wait_for_cmd_amount
export -f get_agents
timeout 20m bash -c "wait_for_cmd_amount ${SPOKE_CONTROLPLANE_AGENTS} 30 get_agents"
echo "All ${SPOKE_CONTROLPLANE_AGENTS} agents have been discovered!"

if [[ "${ASSISTED_STOP_AFTER_AGENT_DISCOVERY}" == "true" ]]; then
    echo "Agents have been discovered, do not wait for the cluster installtion to finish."
    exit
fi

# We need a storage for etcd of the hosted cluster
oc patch storageclass assisted-service -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'

### Hypershift CLI needs access to the kubeconfig, pull-secret and public SSH key
function hypershift_cli() {
  full_cmd="update-ca-trust;$@"
  podman run -it --net host --rm --entrypoint /bin/bash -v $KUBECONFIG:/root/.kube/config -v $ASSISTED_PULLSECRET_JSON:$ASSISTED_PULLSECRET_JSON -v /root/.ssh/id_rsa.pub:/root/.ssh/id_rsa.pub $EXTRA_HYPERSHIFT_CLI_MOUNTS $HYPERSHIFT_IMAGE -c "$full_cmd"
}

echo "Installing HyperShift using upstream image"
hypershift_cli hypershift install --hypershift-image $HYPERSHIFT_IMAGE --namespace hypershift $EXTRA_HYPERSHIFT_INSTALL_FLAGS
if [ "${DISCONNECTED}" = "true" ]; then
  # disconnected hypershift requires patching the operator deployment with the local image mirror of the capi agent and the machine config operator image (registry override flag)
  oc patch deploy/operator -n hypershift --type=strategic \
    --patch="{\"spec\": {\"template\": {\"spec\": {\"containers\": [{\"name\": \"operator\",\"env\":[{\"name\":\"IMAGE_AGENT_CAPI_PROVIDER\",\"value\":\"${PROVIDER_IMAGE}\"}], \"args\":[\"run\",\"--namespace=\$(MY_NAMESPACE)\",\"--pod-name=\$(MY_NAME)\",\"--metrics-addr=:9000\",\"--enable-ocp-cluster-monitoring=false\",\"--enable-ci-debug-output=false\",\"--private-platform=None\",\"--cert-dir=/var/run/secrets/serving-cert\",\"--enable-uwm-telemetry-remote-write\",\"--registry-overrides=${MCO_IMAGE}=${LOCAL_MCO_IMAGE}\"]}]}}}}"
  # delete all rs since patching the deployment doesn't actually remove the running rs
  oc delete rs --all -n hypershift
fi
wait_for_pods "hypershift"

if [ -z "$PROVIDER_IMAGE" ]
then
  echo "PROVIDER_IMAGE override not set"
  export PROVIDER_FLAG_FOR_CREATE_COMMAND=""
else
  echo "PROVIDER_IMAGE override: $PROVIDER_IMAGE"
  export PROVIDER_FLAG_FOR_CREATE_COMMAND=" --annotations hypershift.openshift.io/capi-provider-agent-image=$PROVIDER_IMAGE"
fi

if [ -z "$CONTROL_PLANE_OPERATOR_IMAGE" ]
then
  echo "CONTROL_PLANE_OPERATOR_IMAGE override not set"
  export CONTROL_PLANE_OPERATOR_FLAG_FOR_CREATE_COMMAND=""
else
  echo "CONTROL_PLANE_OPERATOR_IMAGE override: $CONTROL_PLANE_OPERATOR_IMAGE"
  export CONTROL_PLANE_OPERATOR_FLAG_FOR_CREATE_COMMAND=" --control-plane-operator-image $CONTROL_PLANE_OPERATOR_IMAGE"
fi

echo "Creating HostedCluster"
hypershift_cli hypershift create cluster agent --name $ASSISTED_CLUSTER_NAME --base-domain redhat.example --pull-secret $ASSISTED_PULLSECRET_JSON \
 --ssh-key /root/.ssh/id_rsa.pub --agent-namespace $SPOKE_NAMESPACE --namespace $SPOKE_NAMESPACE \
 --release-image ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-${RELEASE_IMAGE}} \
  $CONTROL_PLANE_OPERATOR_FLAG_FOR_CREATE_COMMAND \
  $PROVIDER_FLAG_FOR_CREATE_COMMAND \
  $EXTRA_HYPERSHIFT_CREATE_COMMANDS

# Wait for a hypershift hostedcontrolplane to report ready status
wait_for_resource "hostedcontrolplane/${ASSISTED_CLUSTER_NAME}" "${SPOKE_NAMESPACE}-${ASSISTED_CLUSTER_NAME}"
wait_for_boolean_field "hostedcontrolplane/${ASSISTED_CLUSTER_NAME}" status.ready "${SPOKE_NAMESPACE}-${ASSISTED_CLUSTER_NAME}"
wait_for_condition "nodepool/$ASSISTED_CLUSTER_NAME" "Ready" "10m" "$SPOKE_NAMESPACE"
wait_for_condition "hostedcluster/$ASSISTED_CLUSTER_NAME" "Available" "10m" "$SPOKE_NAMESPACE"

# Scale up
echo "Scaling the hosted cluster up to contain ${SPOKE_CONTROLPLANE_AGENTS} worker nodes"
oc scale nodepool/$ASSISTED_CLUSTER_NAME -n $SPOKE_NAMESPACE --replicas=${SPOKE_CONTROLPLANE_AGENTS}

# Wait for node to appear in the CAPI-deployed cluster
oc extract -n $SPOKE_NAMESPACE secret/$ASSISTED_CLUSTER_NAME-admin-kubeconfig --to=- > /tmp/$ASSISTED_CLUSTER_NAME-kubeconfig
export HUB_KUBECONFIG=${KUBECONFIG}
export KUBECONFIG=/tmp/$ASSISTED_CLUSTER_NAME-kubeconfig

wait_for_object_amount node ${SPOKE_CONTROLPLANE_AGENTS} 10
echo "Worker nodes have been detected successfuly in the created cluster!"

echo "verify the BMH on the HUB cluster is detached"
export KUBECONFIG=${HUB_KUBECONFIG}
if [ $(oc get baremetalhost -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[].metadata.annotations."baremetalhost.metal3.io/detached"| select("assisted-service-controller")' | wc -l) -ne ${SPOKE_CONTROLPLANE_AGENTS} ]; then
  echo "The amount of detached BMHs on the HUB cluster doesn't match the amount of expected installed nodes in the spoke: ${SPOKE_CONTROLPLANE_AGENTS}"
  echo "HUB cluster BMHs: "
  oc get baremetalhost -n "${SPOKE_NAMESPACE}"
  return 1
fi

echo "Destroy the hosted cluster"
hypershift_cli destroy cluster agent --name $ASSISTED_CLUSTER_NAME --namespace $SPOKE_NAMESPACE --cluster-grace-period 60m
echo "Successfully destroyed the hosted cluster"
