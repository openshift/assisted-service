__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"

if [ -z "${DISKS:-}" ]; then
    echo "You must provide DISKS env-var. For example:"
    print_help
    exit 1
fi

function install_lso() {
    oc adm new-project openshift-local-storage || true

    oc annotate project openshift-local-storage openshift.io/node-selector='' --overwrite=true

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
  source: redhat-operators
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
