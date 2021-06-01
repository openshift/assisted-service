set -o nounset
set -o pipefail
set -o errexit

function wait_for_crd() {
    crd="$1"
    namespace="${2:-}"
    echo "Waiting for CRD (${crd}) on namespace (${namespace}) to be defined..."
    for i in {1..40}; do
        oc get "crd/${crd}" -n "${namespace}" && break || sleep 10
    done

    echo "Waiting for CRD (${crd}) on namespace (${namespace}) to become ready..."
    oc wait --for condition=established --timeout=60s "crd/${crd}" -n "${namespace}" || return 1
}

function wait_for_operator() {
    subscription="$1"
    namespace="${2:-}"
    echo "Waiting for operator ${subscription} to get installed on namespace ${namespace}..."

    for _ in $(seq 1 60); do
        csv=$(oc -n "${namespace}" get subscription "${subscription}" -o jsonpath='{.status.installedCSV}' || true)
        if [[ -n "${csv}" ]]; then
            if [[ "$(oc -n "${namespace}" get csv "${csv}" -o jsonpath='{.status.phase}')" == "Succeeded" ]]; then
                echo "ClusterServiceVersion (${csv}) is ready"
                return 0
            fi
        fi

        sleep 10
    done

    echo "Timed out waiting for csv to become ready!"
    return 1
}

function wait_for_pod() {
    pod="$1"
    namespace="${2:-}"
    selector="${3:-}"

    echo "Waiting for pod (${pod}) on namespace (${namespace}) with labels (${selector}) to be created..."
    for i in {1..40}; do
        oc get pod --selector=${selector} --namespace=${namespace} |& grep -ivE "(no resources found|not found)" && break || sleep 10
    done

    echo "Waiting for pod (${pod}) on namespace (${namespace}) with labels (${selector}) to become ready..."
    oc wait -n "$namespace" --for=condition=Ready pod --selector "$selector" --timeout=10m
}

function hash() {
    input=$1
    length=$2
    echo "${input}" | md5sum - | cut -c -${length}
}

function disk_to_wwn() {
    # The WWN token has to begin with a number, so it was prefixed with an
    # hardcoded 0 and afterwards 15 bytes of a digested hash from the original
    # disk name.
    echo "0x0$(hash ${1} 15)"
}
