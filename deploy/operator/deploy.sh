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
${__dir}/setup_assisted_operator.sh
