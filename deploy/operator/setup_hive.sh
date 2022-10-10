#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh
source ${__dir}/utils.sh

set -o xtrace

HIVE_IMAGE="${HIVE_IMAGE:-registry.ci.openshift.org/openshift/hive-v4.0:hive}"

function print_help() {
    ALL_FUNCS="with_olm|from_upstream|enable_agent_install_strategy|print_help"
    if [ "${DISCONNECTED}" = "true" ]; then
        echo "Usage: DISCONNECTED=true AUTHFILE=... LOCAL_REGISTRY=... bash ${0} (${ALL_FUNCS})"
    else
        echo "Usage: bash ${0} (${ALL_FUNCS})"
    fi
}

function with_olm() {
    if [ "${DISCONNECTED}" = "true" ]; then
        echo "Not yet implemented"
        return 1
    fi

    cat <<EOCR | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${HIVE_NAMESPACE}
  labels:
    name: ${HIVE_NAMESPACE}
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: hive-group
  namespace: ${HIVE_NAMESPACE}
spec:
  targetNamespaces:
    - ${HIVE_NAMESPACE}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: hive-operator
  namespace: ${HIVE_NAMESPACE}
spec:
  installPlanApproval: Automatic
  name: hive-operator
  source: community-operators
  sourceNamespace: openshift-marketplace
  channel: alpha
EOCR

    wait_for_operator "hive-operator" "${HIVE_NAMESPACE}"
    wait_for_crd "clusterdeployments.hive.openshift.io"

    echo "Hive installed successfully!"

    enable_agent_install_strategy
}

function from_upstream() {
    HIVE_DIR="${HIVE_DIR:-${HOME}/go/src/github.com/openshift/hive}"
    HIVE_BRANCH="${HIVE_BRANCH:-master}"

    if [ ! -d "${HIVE_DIR}" ]; then
        git clone https://github.com/openshift/hive.git "${HIVE_DIR}"
    fi

    pushd ${HIVE_DIR}
    git fetch origin "${HIVE_BRANCH}"
    git reset --hard FETCH_HEAD

    if [ "${DISCONNECTED}" = "true" ]; then
        export IMG="${LOCAL_REGISTRY}/localimages/hive:latest"
        oc image mirror \
            -a ${AUTHFILE} \
            ${HIVE_IMAGE} \
            ${IMG}
    else
        export IMG="${HIVE_IMAGE}"
    fi

    echo "Installing kustomize as a dependency for hive makefile..."
    GOBIN=/usr/local/bin/ GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v4@latest
    export IMG="quay.io/mold/hive:ai-test-fix"
    
    make deploy HIVE_OPERATOR_NS="${HIVE_NAMESPACE}" HIVE_NS="${HIVE_NAMESPACE}"
    wait_for_pod "hive-operator" "${HIVE_NAMESPACE}" "control-plane=hive-operator"
    wait_for_pod "hive-controllers" "${HIVE_NAMESPACE}" "control-plane=controller-manager"

    echo "Hive installed successfully!"
    popd

    enable_agent_install_strategy
}

function enable_agent_install_strategy() {
    tee >(oc apply -f -) <<EOF
apiVersion: hive.openshift.io/v1
kind: HiveConfig
metadata:
  name: hive
spec:
  logLevel: debug
  targetNamespace: ${HIVE_NAMESPACE}
  featureGates:
    custom:
      enabled:
      - AlphaAgentInstallStrategy
    featureSet: Custom
EOF

    wait_for_condition "hiveconfig.hive.openshift.io/hive" "Ready" "10m"
}

if [ -z "$@" ] || ! declare -F "$@"; then
    print_help
    exit 1
fi

"$@"
