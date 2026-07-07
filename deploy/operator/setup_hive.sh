#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh
source ${__dir}/utils.sh

set -o xtrace

HIVE_IMAGE="${HIVE_IMAGE:-registry.ci.openshift.org/openshift/hive-v4.0:hive}"
HIVE_CATALOG_IMAGE="${HIVE_CATALOG_IMAGE:-quay.io/operatorhubio/catalog:latest}"

function configure_hive_catalog_source() {
    if [[ -z "${HIVE_CATALOG_SOURCE:-}" ]]; then
        local oc_major
        oc_major=$(oc version -o json | jq --raw-output '.openshiftVersion' | cut -d'.' -f1)
        if [[ "${oc_major}" -ge 5 ]]; then
            # hive-operator was removed from community-operators starting with OCP 5.0.
            HIVE_CATALOG_SOURCE="upstream-community-operators"
        else
            HIVE_CATALOG_SOURCE="community-operators"
        fi
        echo "Selected hive-operator catalog source ${HIVE_CATALOG_SOURCE} for OpenShift ${oc_major}.x"
    fi

    if [[ "${HIVE_CATALOG_SOURCE}" == "upstream-community-operators" ]]; then
        ensure_upstream_community_operators_catalog
    fi

    export HIVE_CATALOG_SOURCE
}

function ensure_upstream_community_operators_catalog() {
    local catalog_namespace="openshift-marketplace"

    if ! oc get catalogsource "${HIVE_CATALOG_SOURCE}" -n "${catalog_namespace}" &>/dev/null; then
        cat <<EOCR | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${HIVE_CATALOG_SOURCE}
  namespace: ${catalog_namespace}
spec:
  displayName: Upstream Community Operators
  image: ${HIVE_CATALOG_IMAGE}
  publisher: OperatorHub.io
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 10m0s
EOCR
    fi

    wait_for_catalogsource "${HIVE_CATALOG_SOURCE}" "${catalog_namespace}"
}

function wait_for_catalogsource() {
    local catalog_source="$1"
    local namespace="$2"
    local counter=1
    local retry=120

    echo "Waiting for catalogsource ${catalog_source} in namespace ${namespace} to become ready..."
    until [[ "$(oc get catalogsource "${catalog_source}" -n "${namespace}" -o jsonpath='{.status.connectionState.lastObservedState}' 2>/dev/null)" == "READY" ]]; do
        if [[ "${counter}" -eq "${retry}" ]]; then
            echo "$(date --rfc-3339=seconds) ERROR: catalogsource ${catalog_source} is not ready"
            oc get catalogsource "${catalog_source}" -n "${namespace}" -o yaml
            exit 1
        fi
        ((counter++)) && sleep 5
    done
}

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

    configure_hive_catalog_source

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
  source: ${HIVE_CATALOG_SOURCE}
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

    wait_for_condition "hiveconfig.hive.openshift.io/hive" "condition=Ready" "10m"
}

if [ -z "$@" ] || ! declare -F "$@"; then
    print_help
    exit 1
fi

"$@"
