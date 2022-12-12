#!/usr/bin/env bash

set -o nounset

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh

function destroy_spoke() {
    kubectl delete namespace "${SPOKE_NAMESPACE}"
    kubectl delete clusterimageset "${ASSISTED_OPENSHIFT_VERSION}"
}

function destroy_hub() {
    kubectl delete namespace "${ASSISTED_NAMESPACE}"
    kubectl delete agentserviceconfigs.agent-install.openshift.io agent
    kubectl delete localvolume -n openshift-local-storage assisted-service
    kubectl delete catalogsource assisted-service-catalog -n openshift-marketplace

    ${__dir}/libvirt_disks.sh destroy
    kubectl get pv -o=name | xargs -r kubectl delete
}

if [ -z "$@" ]; then
    destroy_spoke
    destroy_hub
fi

"$@"
