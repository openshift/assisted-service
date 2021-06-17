__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh
source ${__dir}/mirror_utils.sh

set -xeo pipefail

ASSISTED_NAMESPACE="${ASSISTED_NAMESPACE:-assisted-installer}"
INDEX_IMAGE="${INDEX_IMAGE:-quay.io/ocpmetal/assisted-service-index:latest}"
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"

INDEX_TAG="${INDEX_TAG:-latest}"
DISCONNECTED="${DISCONNECTED:-false}"

function print_help() {
  ALL_FUNCS="from_community_operators|from_index_image|print_help"
  if [ "${DISCONNECTED}" == "true" ]; then
    echo "Usage: DISCONNECTED=true LOCAL_REGISTRY=... AUTHFILE=... IRONIC_IMAGES_DIR=... MIRROR_BASE_URL=... bash ${0} (${ALL_FUNCS})"
  else
    echo "Usage: OPENSHIFT_VERSIONS=... bash ${0} (${ALL_FUNCS})"
  fi
}

if [ -z "${OPENSHIFT_VERSIONS:-}" ]; then
  echo "You must provide OPENSHIFT_VERSIONS env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${AUTHFILE:-}" ]; then
  echo "On disconnected mode, you must provide AUTHFILE env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${LOCAL_REGISTRY:-}" ]; then
  echo "On disconnected mode, you must provide LOCAL_REGISTRY env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${IRONIC_IMAGES_DIR:-}" ]; then
  echo "On disconnected mode, you must provide IRONIC_IMAGES_DIR env-var."
  print_help
  exit 1
fi

if [ "${DISCONNECTED}" = "true" ] && [ -z "${MIRROR_BASE_URL:-}" ]; then
  echo "On disconnected mode, you must provide MIRROR_BASE_URL env-var."
  print_help
  exit 1
fi

function subscription_config() {
    # Notice that this list of env variables is alphabetically ordered due to OLM bug

    if [ -n "${AGENT_IMAGE:-}" ]; then
cat <<EOF
    - name: AGENT_IMAGE
      value: '${AGENT_IMAGE}'
EOF
    fi

    if [ -n "${CONTROLLER_IMAGE:-}" ]; then
cat <<EOF
    - name: CONTROLLER_IMAGE
      value: '${CONTROLLER_IMAGE}'
EOF
    fi

    if [ -n "${DATABASE_IMAGE:-}" ]; then
cat <<EOF
    - name: DATABASE_IMAGE
      value: '${DATABASE_IMAGE}'
EOF
    fi

    if [ -n "${INSTALLER_IMAGE:-}" ]; then
cat <<EOF
    - name: INSTALLER_IMAGE
      value: '${INSTALLER_IMAGE}'
EOF
    fi

    if [ -n "${SERVICE_IMAGE:-}" ]; then
cat <<EOF
    - name: SERVICE_IMAGE
      value: '${SERVICE_IMAGE}'
EOF
    fi
}

function configmap_config() {
    if [ -n "${OPENSHIFT_VERSIONS:-}" ]; then
cat <<EOF
  OPENSHIFT_VERSIONS: '${OPENSHIFT_VERSIONS}'
EOF
    fi

    if [ -n "${SERVICE_BASE_URL:-}" ]; then
cat <<EOF
  SERVICE_BASE_URL: '${SERVICE_BASE_URL}'
EOF
    fi

    if [ -n "${PUBLIC_CONTAINER_REGISTRIES:-}" ]; then
cat <<EOF
  PUBLIC_CONTAINER_REGISTRIES: 'quay.io,${PUBLIC_CONTAINER_REGISTRIES}'
EOF
    fi
}

function install_from_catalog_source() {
  catalog_source_name="${1}"
  tee << EOCR >(oc apply -f -)
apiVersion: v1
kind: Namespace
metadata:
  name: ${ASSISTED_NAMESPACE}
  labels:
    name: ${ASSISTED_NAMESPACE}
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: assisted-installer-group
  namespace: ${ASSISTED_NAMESPACE}
spec:
  targetNamespaces:
    - ${ASSISTED_NAMESPACE}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: assisted-service-operator
  namespace: ${ASSISTED_NAMESPACE}
spec:
  config:
    env:
$(subscription_config)
  installPlanApproval: Automatic
  name: assisted-service-operator
  source: ${catalog_source_name}
  sourceNamespace: openshift-marketplace
EOCR

  wait_for_crd "agentserviceconfigs.agent-install.openshift.io"

  tee << EOCR >(oc apply -f -)
apiVersion: v1
kind: ConfigMap
metadata:
  name: assisted-service-config
  namespace: ${ASSISTED_NAMESPACE}
data:
  LOG_LEVEL: "debug"
$(configmap_config)
EOCR

tee << EOCR >(oc apply -f -)
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
 name: agent
 annotations:
  unsupported.agent-install.openshift.io/assisted-service-configmap: "assisted-service-config"
spec:
 databaseStorage:
  storageClassName: ${STORAGE_CLASS_NAME}
  accessModes:
  - ReadWriteOnce
  resources:
   requests:
    storage: 8Gi
 filesystemStorage:
  storageClassName: ${STORAGE_CLASS_NAME}
  accessModes:
  - ReadWriteOnce
  resources:
   requests:
    storage: 8Gi
EOCR

  wait_for_operator "assisted-service-operator" "${ASSISTED_NAMESPACE}"
  wait_for_pod "assisted-service" "${ASSISTED_NAMESPACE}" "app=assisted-service"

  echo "Enabling configuration of BMH resources outside of openshift-machine-api namespace"
  oc patch provisioning provisioning-configuration --type merge -p '{"spec":{"watchAllNamespaces": true}}'

  echo "Installation of Assisted Installer operator passed successfully!"
}

function from_index_image() {
  if [ "${DISCONNECTED}" = true ]; then
    catalog_source_name="mirror-catalog-for-assisted-service-operator"
    mirror_package "assisted-service-operator" \
        "${INDEX_IMAGE}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"

    rhcos_image=$(echo ${OPENSHIFT_VERSIONS} | jq -r '.[].rhcos_image')
    mirror_rhcos_image=$(mirror_file "${rhcos_image}" "${IRONIC_IMAGES_DIR}" "${MIRROR_BASE_URL}")

    rhcos_rootfs=$(echo ${OPENSHIFT_VERSIONS} | jq -r '.[].rhcos_rootfs')
    mirror_rhcos_rootfs=$(mirror_file "${rhcos_rootfs}" "${IRONIC_IMAGES_DIR}" "${MIRROR_BASE_URL}")

    OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
      jq ".[].rhcos_image=\"${mirror_rhcos_image}\" | .[].rhcos_rootfs=\"${mirror_rhcos_rootfs}\"")

  else
    catalog_source_name="assisted-service-operator-catalog"
    tee << EOCR >(oc apply -f -)
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${catalog_source_name}
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${INDEX_IMAGE}
  displayName: Assisted Test Registry
  publisher: Assisted Developer
EOCR
  fi

  install_from_catalog_source "${catalog_source_name}"
}

function from_community_operators() {
  if [ "${DISCONNECTED}" = true ]; then
    catalog_source_name="mirror-catalog-for-assisted-service-operator"
    mirror_package_from_official_index "assisted-service-operator" "community-operator-index" \
        "${INDEX_TAG}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"
  else
    catalog_source_name="community-operators"
  fi

  install_from_catalog_source "${catalog_source_name}"
}

if [ -z "$@" ]; then
  from_index_image
fi

if ! declare -F "$@"; then
  print_help
  exit 1
fi

"$@"
