source utils.sh

set -xeo pipefail

if [ -z "${DISKS}" ]; then
    echo "You must provide DISKS env-var. For example:"
    echo "    bash DISKS=\$(echo sd{b..f}) ./create_libvirt_disks.sh"
    exit 1
fi

function install_lso() {
    oc adm new-project openshift-local-storage || true

    oc annotate project openshift-local-storage openshift.io/node-selector='' --overwrite=true

    cat <<EOCR | oc apply -f -
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
}

function create_local_volume() {
    wait_for_crd "localvolumes.local.storage.openshift.io" "openshift-local-storage"

    cat <<EOCR | oc apply -f -
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
      storageClassName: assisted-service
      volumeMode: Filesystem
EOCR
}

install_lso
create_local_volume
echo "Done configuring local-storage-operator!"
