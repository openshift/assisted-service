__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh
source ${__dir}/mirror_utils.sh

STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"
OCP_RELEASE="${OCP_RELEASE:-4.8}"
DISCONNECTED="${DISCONNECTED:-false}"

function print_help() {
  ALL_FUNCS="install_lso|create_local_volume|print_help"
  if [ "${DISCONNECTED}" == "true" ]; then
    echo "Usage: DISKS=\$(echo sd{b..f}) DISCONNECTED=true LOCAL_REGISTRY=... AUTHFILE=... bash ${0} (${ALL_FUNCS})"
  else
    echo "Usage: DISKS=\$(echo sd{b..f}) bash ${0} (${ALL_FUNCS})"
  fi
}

if [ -z "${DISKS:-}" ]; then
  echo "You must provide DISKS env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${AUTHFILE}" ]; then
  echo "On disconnected mode, you must provide AUTHFILE env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${LOCAL_REGISTRY}" ]; then
  echo "On disconnected mode, you must provide LOCAL_REGISTRY env-var."
  print_help
  exit 1
fi

function install_lso() {
  oc adm new-project openshift-local-storage || true

  oc annotate project openshift-local-storage openshift.io/node-selector='' --overwrite=true

  catalog_source="redhat-operators"
  if [ "${DISCONNECTED}" = true ]; then
    if ! which opm; then
        install_opm
    fi

    disable_default_indexes
    mirror_package "local-storage-operator" "redhat-operator-index" \
        "${OCP_RELEASE}" "${LOCAL_REGISTRY}" "${AUTHFILE}"
    catalog_source="mirror-catalog-for-local-storage-operator"
  fi

  tee << EOCR >(oc apply -f -)
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: local-operator-group
  namespace: openshift-local-storage
spec:
  targetNamespaces:
  - openshift-local-storage
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: local-storage-operator
  namespace: openshift-local-storage
spec:
  installPlanApproval: Automatic
  name: local-storage-operator
  source: ${catalog_source}
  sourceNamespace: openshift-marketplace
EOCR

  wait_for_operator "local-storage-operator" "openshift-local-storage"
  echo "Done installing local-storage-operator!"
}

function create_local_volume() {
  wait_for_crd "localvolumes.local.storage.openshift.io" "openshift-local-storage"

  tee << EOCR >(oc apply -f -)
apiVersion: local.storage.openshift.io/v1
kind: LocalVolume
metadata:
  name: assisted-service
  namespace: openshift-local-storage
spec:
  logLevel: Normal
  managementState: Managed
  storageClassDevices:
    - devicePaths:
$(printf '        - /dev/%s\n' ${DISKS})
      storageClassName: ${STORAGE_CLASS_NAME}
      volumeMode: Filesystem
EOCR

  echo "Done creating local volume for assisted-service!"
}

declare -F $@ || (print_help && exit 1)

"$@"
