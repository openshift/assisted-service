#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(realpath ${__dir}/../..)"

source ${__dir}/common.sh
source ${__dir}/utils.sh
source ${__dir}/mirror_utils.sh

#########
# Setup #
#########

function setup_disconnected_parameters() {
    # Some of the variables over here can be sourced from dev-scripts
    # source common.sh
    # source utils.sh
    # source network.sh
    # set +x
    # export -f wrap_if_ipv6 ipversion

    if [ "${OPENSHIFT_CI:-false}" = "false" ]; then
        export ASSISTED_DEPLOYMENT_METHOD="from_community_operators"
    fi

    export HIVE_DEPLOYMENT_METHOD="from_upstream"

    export MIRROR_BASE_URL="http://$(wrap_if_ipv6 ${PROVISIONING_HOST_IP})/images"
    export AUTHFILE="${XDG_RUNTIME_DIR}/containers/auth.json"
    mkdir -p $(dirname ${AUTHFILE})

    merge_authfiles "${PULL_SECRET_FILE}" "${REGISTRY_CREDS}" "${AUTHFILE}"

    ${__root}/hack/setup_env.sh hive_from_upstream

    ocp_mirror_release \
        "${PULL_SECRET_FILE}" \
        "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" \
        "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})"
}

set -o xtrace

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

### XXX(chocobomb)
export HIVE_DEPLOYMENT_METHOD="from_upstream"
${__dir}/setup_hive.sh "${HIVE_DEPLOYMENT_METHOD}"

############
# Assisted #
############

${__dir}/setup_assisted_operator.sh "${ASSISTED_DEPLOYMENT_METHOD}"
