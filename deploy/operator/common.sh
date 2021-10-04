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

############
# Versions #
############
export DEFAULT_OS_IMAGES="${DEFAULT_OS_IMAGES:-$(cat ${__root}/data/default_os_images.json)}"
if [[ "${ASSISTED_UPGRADE_OPERATOR}" == "false" ]]; then
    export RELEASE_IMAGE=$(echo ${DEFAULT_OS_IMAGES} | jq -rc '[.[].url]|max')
    export VERSION=$(echo ${DEFAULT_OS_IMAGES} | jq -rc '[.[].openshift_version]|max')
else
    # Before the AI operator upgrade, we install the version prior to the most current one of OCP. 
    # The most current version of OCP we are installing is 4.9, and the version previous to that is 4.8.
    export RELEASE_IMAGE=$(echo ${DEFAULT_OS_IMAGES} | jq -rc '[.[].url][-2]')
    export VERSION=$(echo ${DEFAULT_OS_IMAGES} | jq -rc '[.[].openshift_version][-2]')
fi

export ASSISTED_OPENSHIFT_VERSION="${ASSISTED_OPENSHIFT_VERSION:-openshift-v${VERSION}}"
export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE="${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-${RELEASE_IMAGE}}"
export OS_IMAGES=$(echo ${DEFAULT_OS_IMAGES} | jq -rc 'map(select(.openshift_version>="4.8"))')
