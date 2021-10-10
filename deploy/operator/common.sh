__dir=${__dir:-"$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"}
__root=${__root:-"$(realpath ${__dir}/../..)"}

if [ -z "${DISKS:-}" ]; then
    export DISKS=$(echo sd{b..f})
fi

export DISCONNECTED="${DISCONNECTED:-false}"
if [ "${DISCONNECTED}" = "true" ]; then
    export LOCAL_REGISTRY="${LOCAL_REGISTRY_DNS_NAME}:${LOCAL_REGISTRY_PORT}"
fi

##############
# Deployment #
##############

export ASSISTED_DEPLOYMENT_METHOD="${ASSISTED_DEPLOYMENT_METHOD:-from_index_image}"
export HIVE_DEPLOYMENT_METHOD="${HIVE_DEPLOYMENT_METHOD:-with_olm}"

export ASSISTED_NAMESPACE="${ASSISTED_NAMESPACE:-assisted-installer}"
export SPOKE_NAMESPACE="${SPOKE_NAMESPACE:-assisted-spoke-cluster}"
export HIVE_NAMESPACE="${HIVE_NAMESPACE:-hive}"
export ASSISTED_UPGRADE_OPERATOR="${ASSISTED_UPGRADE_OPERATOR:-false}"
export ASSISTED_SERVICE_OPERATOR_CATALOG="assisted-service-operator-catalog"

############
# Versions #
############
export OPENSHIFT_VERSIONS="${OPENSHIFT_VERSIONS:-$(cat ${__root}/data/default_ocp_versions.json)}"

if [ -z "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-}" ]; then
    export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE=$(echo ${OPENSHIFT_VERSIONS} | jq -rc '[.[].release_image]|max')
    if [[ "${ASSISTED_UPGRADE_OPERATOR}" == "true" ]]; then
        # Before the AI operator upgrade, we install the version prior to the most current one of OCP. 
        # The most current version of OCP we are installing is 4.9, and the version previous to that is 4.8.
        export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE=$(echo ${OPENSHIFT_VERSIONS} | jq -rc '[.[].release_image][-2]')
    fi
fi

export ASSISTED_OPENSHIFT_VERSION="${ASSISTED_OPENSHIFT_VERSION:-openshift-v4.9.0}"

if [[ "${ASSISTED_UPGRADE_OPERATOR}" == "true" ]]; then
    VERSION=openshift-v$(echo ${OPENSHIFT_VERSIONS} | jq -rc '[.[].display_name][-2]')
    export ASSISTED_OPENSHIFT_VERSION="${ASSISTED_OPENSHIFT_VERSION:-${VERSION}}"
fi

OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
    jq -rc 'with_entries(select(.key != "4.6" and .key != "4.7")) | with_entries(
    {
        key: .key,
        value: {rhcos_image:   .value.rhcos_image,
                rhcos_version: .value.rhcos_version,
                rhcos_rootfs:  .value.rhcos_rootfs}
    }
    )')