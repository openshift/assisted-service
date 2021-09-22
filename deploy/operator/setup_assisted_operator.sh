__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh
source ${__dir}/utils.sh
source ${__dir}/mirror_utils.sh

set -x

INDEX_IMAGE="${INDEX_IMAGE:-quay.io/ocpmetal/assisted-service-index:latest}"
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"

INDEX_TAG="${INDEX_TAG:-latest}"

function print_help() {
  ALL_FUNCS="from_community_operators|from_index_image|print_help"
  if [ "${DISCONNECTED}" = "true" ]; then
    echo "Usage: DISCONNECTED=true LOCAL_REGISTRY=... AUTHFILE=... IRONIC_IMAGES_DIR=... MIRROR_BASE_URL=... bash ${0} (${ALL_FUNCS})"
  else
    echo "Usage: OPENSHIFT_VERSIONS=... bash ${0} (${ALL_FUNCS})"
  fi
}

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

function mirror_config() {
  if [ "${DISCONNECTED}" = "true" ]; then
cat <<EOF
 mirrorRegistryRef:
  name: 'assisted-mirror-config'
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
  source: ${CATALOG_SOURCE_NAME:-${catalog_source_name}}
  channel: ${CHANNEL:-alpha}
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

  if [ "${DISCONNECTED}" = "true" ]; then
    deploy_mirror_config_map
  fi

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
$(mirror_config)
EOCR

  wait_for_operator "assisted-service-operator" "${ASSISTED_NAMESPACE}"
  wait_for_condition "agentserviceconfigs/agent" "ReconcileCompleted" "5m"
  wait_for_pod "assisted-service" "${ASSISTED_NAMESPACE}" "app=assisted-service"

  echo "Enabling configuration of BMH resources outside of openshift-machine-api namespace"
  oc patch provisioning provisioning-configuration --type merge -p '{"spec":{"watchAllNamespaces": true}}'

  echo "Installation of Assisted Installer operator passed successfully!"
}

function registry_config() {
  src_image=${1}
  mirrored_image=${2}
  printf '
    [[registry]]
      location = "%s"
      insecure = false
      mirror-by-digest-only = false

      [[registry.mirror]]
        location = "%s"
  ' ${src_image} ${mirrored_image}
}

function deploy_mirror_config_map() {
  # The mirror should point all the release images and not just the OpenShift release image itself.
  # An arbitrary image (cli) is chosen to retreive its pull spec, in order to mirror its repository.
  cli_image=$(podman run --quiet --rm --net=none "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" image cli)

  assisted_index_image=$(get_image_without_registry $(get_image_repository_only ${INDEX_IMAGE}))

  cat << EOCR > ./assisted-mirror-config
apiVersion: v1
kind: ConfigMap
metadata:
  name: assisted-mirror-config
  namespace: ${ASSISTED_NAMESPACE}
  labels:
    app: assisted-service
data:
  registries.conf: |
    unqualified-search-registries = ["registry.access.redhat.com", "docker.io"]

    $(registry_config "$(get_image_without_tag ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})" "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})")
    $(registry_config "$(get_image_without_tag ${cli_image})" "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})")
    $(for row in $(kubectl get imagecontentsourcepolicy -o json |
        jq -rc ".items[] | select(.metadata.name | test(\"${assisted_index_image}\")).spec.repositoryDigestMirrors[] | [.mirrors[0], .source]"); do
      row=$(echo ${row} | tr -d '[]"');
      source=$(echo ${row} | cut -d',' -f2);
      mirror=$(echo ${row} | cut -d',' -f1);
      registry_config ${source} ${mirror};
    done)
EOCR

  python ${__dir}/set_ca_bundle.py "${WORKING_DIR}/registry/certs/registry.2.crt" "./assisted-mirror-config"
  tee < ./assisted-mirror-config >(oc apply -f -)
}

function from_index_image() {
  if [ "${DISCONNECTED}" = "true" ]; then
    catalog_source_name="mirror-catalog-for-assisted-service-operator"
    mirror_package "assisted-service-operator" \
        "${INDEX_IMAGE}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"
    mirror_rhcos
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
  if [ "${DISCONNECTED}" = "true" ]; then
    INDEX_IMAGE="registry.redhat.io/redhat/community-operator-index:${INDEX_TAG}"
    catalog_source_name="mirror-catalog-for-assisted-service-operator"
    mirror_package "assisted-service-operator" \
      "${INDEX_IMAGE}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"
    mirror_rhcos
  else
    catalog_source_name="community-operators"
  fi

  install_from_catalog_source "${catalog_source_name}"
}

function mirror_rhcos() {
    rhcos_image=$(echo ${OPENSHIFT_VERSIONS} | jq -r '.[].rhcos_image')
    mirror_rhcos_image=$(mirror_file "${rhcos_image}" "${IRONIC_IMAGES_DIR}" "${MIRROR_BASE_URL}")

    rhcos_rootfs=$(echo ${OPENSHIFT_VERSIONS} | jq -r '.[].rhcos_rootfs')
    mirror_rhcos_rootfs=$(mirror_file "${rhcos_rootfs}" "${IRONIC_IMAGES_DIR}" "${MIRROR_BASE_URL}")

    OPENSHIFT_VERSIONS=$(echo ${OPENSHIFT_VERSIONS} |
      jq ".[].rhcos_image=\"${mirror_rhcos_image}\" | .[].rhcos_rootfs=\"${mirror_rhcos_rootfs}\"")
}

if [ -z "$@" ]; then
  from_index_image
fi

if ! declare -F "$@"; then
  print_help
  exit 1
fi

"$@"
