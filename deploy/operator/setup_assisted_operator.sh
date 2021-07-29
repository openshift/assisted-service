__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

set -xeo pipefail

ASSISTED_NAMESPACE="${ASSISTED_NAMESPACE:-assisted-installer}"
INDEX_IMAGE="${INDEX_IMAGE:-quay.io/ocpmetal/assisted-service-index:latest}"
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"

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
}

tee << EOCR >(oc apply -f -)
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: assisted-service-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${INDEX_IMAGE}
  displayName: Assisted Test Registry
  publisher: Assisted Developer
---
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
  source: assisted-service-catalog
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
 name: agent-asd
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

echo "Installation of Assisted Installer operator passed successfully!"
