__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

set -xeo pipefail

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
