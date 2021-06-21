#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../..)"

source ${__dir}/mirror_utils.sh

function setup_disconnected_parameters() {
    # Some of the variables over here can be sourced from dev-scripts
    # source common.sh utils.sh
    # set +x
    # export -f wrap_if_ipv6 ipversion

    if [ "${OPENSHIFT_CI:-false}" = "false" ] then
        export ASSISTED_DEPLOYMENT_METHOD="from_community_operators"
    fi

    export HIVE_DEPLOYMENT_METHOD="from_upstream"

    export MIRROR_BASE_URL="http://$(wrap_if_ipv6 ${PROVISIONING_HOST_IP})/images"
    export AUTHFILE="${XDG_RUNTIME_DIR}/containers/auth.json"
    mkdir -p $(dirname ${AUTHFILE})

    export LOCAL_REGISTRY="${LOCAL_REGISTRY_DNS_NAME}:${LOCAL_REGISTRY_PORT}"

    merge_authfiles "${PULL_SECRET_FILE}" "${REGISTRY_CREDS}" "${AUTHFILE}"

    ${__root}/hack/setup_env.sh hive_from_upstream
}

set -o xtrace

if [ -z "${DISKS:-}" ]; then
    export DISKS=$(echo sd{b..f})
fi

DISCONNECTED="${DISCONNECTED:-false}"
ASSISTED_DEPLOYMENT_METHOD="${ASSISTED_DEPLOYMENT_METHOD:-from_index_image}"
HIVE_DEPLOYMENT_METHOD="${HIVE_DEPLOYMENT_METHOD:-with_olm}"

if [ "${DISCONNECTED}" = "true" ]; then
    setup_disconnected_parameters
fi

#######
# LSO #
#######

${__dir}/libvirt_disks.sh create

if [ "${INSTALL_LSO:-true}" = "true" ]; then
    ${__dir}/setup_lso.sh install_lso
fi

${__dir}/setup_lso.sh create_local_volume

########
# Hive #
########

${__dir}/setup_hive.sh "${HIVE_DEPLOYMENT_METHOD}"

############
# Assisted #
############

export OPENSHIFT_VERSIONS="${OPENSHIFT_VERSIONS:-$(cat ${__root}/data/default_ocp_versions.json)}"
OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
    jq -rc 'with_entries(.key = "4.8") | with_entries(
    {
        key: .key,
        value: {rhcos_image:   .value.rhcos_image,
                rhcos_version: .value.rhcos_version,
                rhcos_rootfs:  .value.rhcos_rootfs}
    }
    )')

${__dir}/setup_assisted_operator.sh "${ASSISTED_DEPLOYMENT_METHOD}"
