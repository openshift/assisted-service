__dir=${__dir:-"$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"}
__root=${__root:-"$(realpath ${__dir}/../..)"}

if [ -z "${DISKS:-}" ]; then
    export DISKS=$(echo sd{b..f})
fi

export DISCONNECTED="${DISCONNECTED:-false}"
export ASSISTED_DEPLOYMENT_METHOD="${ASSISTED_DEPLOYMENT_METHOD:-from_index_image}"
export HIVE_DEPLOYMENT_METHOD="${HIVE_DEPLOYMENT_METHOD:-with_olm}"

export OPENSHIFT_VERSIONS="${OPENSHIFT_VERSIONS:-$(cat ${__root}/data/default_ocp_versions.json)}"

if [ -z "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE:-}" ]; then
    export ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE=$(echo ${OPENSHIFT_VERSIONS} | jq -rc '[.[].release_image]|max')
fi

export LOCAL_REGISTRY="${LOCAL_REGISTRY_DNS_NAME}:${LOCAL_REGISTRY_PORT}"
export RELEASE_IMAGE_REPOSITORY_TAG=${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE#*/}
export RELEASE_IMAGE_REPOSITORY=$(echo ${RELEASE_IMAGE_REPOSITORY_TAG} | cut -d':' -f1)
export ASSISTED_OPENSHIFT_VERSION="${ASSISTED_OPENSHIFT_VERSION:-openshift-v4.8.0}"

OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
    jq -rc 'with_entries(.key = "4.8") | with_entries(
    {
        key: .key,
        value: {rhcos_image:   .value.rhcos_image,
                rhcos_version: .value.rhcos_version,
                rhcos_rootfs:  .value.rhcos_rootfs}
    }
    )')

export ASSISTED_NAMESPACE="${ASSISTED_NAMESPACE:-assisted-installer}"
export SPOKE_NAMESPACE="${SPOKE_NAMESPACE:-assisted-spoke-cluster}"
export HIVE_NAMESPACE="${HIVE_NAMESPACE:-hive}"
