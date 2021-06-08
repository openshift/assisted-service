#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "${DISKS:-}" ]; then
    export DISKS=$(echo sd{b..f})
fi

${__dir}/libvirt_disks.sh create

if [ "${INSTALL_LSO:-true}" == "true" ]; then
    ${__dir}/setup_lso.sh install_lso
fi

${__dir}/setup_lso.sh create_local_volume
${__dir}/setup_hive.sh with_olm

export OPENSHIFT_VERSIONS="${OPENSHIFT_VERSIONS:-$(cat ${__dir}/../../data/default_ocp_versions.json)}"
OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
    jq -rc 'with_entries(.key = "4.8") | with_entries(
    {
        key: .key,
        value: {rhcos_image:   .value.rhcos_image,
                rhcos_version: .value.rhcos_version,
                rhcos_rootfs:  .value.rhcos_rootfs}
    }
    )')
${__dir}/setup_assisted_operator.sh from_index_image
