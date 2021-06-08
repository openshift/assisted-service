__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

DISCONNECTED="${DISCONNECTED:-false}"
HIVE_IMAGE="${HIVE_IMAGE:-registry.ci.openshift.org/openshift/hive-v4.0:hive}"

function print_help() {
  ALL_FUNCS="with_olm|from_upstream|enable_agent_install_strategy|print_help"
  if [ "${DISCONNECTED}" == "true" ]; then
    echo "Usage: DISCONNECTED=true AUTHFILE=... LOCAL_REGISTRY=... bash ${0} (${ALL_FUNCS})"
  else
    echo "Usage: bash ${0} (${ALL_FUNCS})"
  fi
}

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

function with_olm() {
  if [ "${DISCONNECTED}" = "true" ]; then
    echo "Not yet implemented"
    return 1
  fi

  cat <<EOCR | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: hive-operator
  namespace: openshift-operators
spec:
  installPlanApproval: Automatic
  name: hive-operator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOCR

  wait_for_operator "hive-operator" "openshift-operators"
  wait_for_crd "clusterdeployments.hive.openshift.io"

  echo "Hive installed successfully!"

  enable_agent_install_strategy
}

function from_upstream() {
  HIVE_DIR="${HIVE_DIR:-${HOME}/go/src/github.com/openshift/hive}"
  HIVE_BRANCH="${HIVE_BRANCH:-master}"

  if [ ! -d "${HIVE_DIR}" ]; then
    git clone https://github.com/openshift/hive.git "${HIVE_DIR}"
  fi

  pushd ${HIVE_DIR}
  git fetch origin "${HIVE_BRANCH}"
  git reset --hard FETCH_HEAD

  if [ "${DISCONNECTED}" = "true" ]; then
    export IMG="${LOCAL_REGISTRY}/localimages/hive:latest"
    oc image mirror \
       -a ${AUTHFILE} \
       ${HIVE_IMAGE} \
       ${IMG}
  else
    export IMG="${HIVE_IMAGE}"
  fi

  make deploy
  wait_for_pod "hive-operator" "hive" "control-plane=hive-operator"
  wait_for_pod "hive-controllers" "hive" "control-plane=controller-manager"

  echo "Hive installed successfully!"
  popd

  enable_agent_install_strategy
}

function enable_agent_install_strategy() {
  tee << EOF >(oc apply -f -)
apiVersion: hive.openshift.io/v1
kind: HiveConfig
metadata:
  name: hive
spec:
  logLevel: debug
  targetNamespace: hive
  featureGates:
    custom:
      enabled:
      - AlphaAgentInstallStrategy
    featureSet: Custom
EOF
}

if [ -z "$@" ] || ! declare -F "$@"; then
  print_help
  exit 1
fi

"$@"
