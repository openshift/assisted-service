set -o nounset
set -o pipefail
set -o errexit

function wait_for_crd() {
    crd="$1"
    namespace="${2:-}"

    wait_for_condition "crd/${crd}" "condition=Established" "60s" "${namespace}"
}


function remote_agents() {
	namespace="$1"
	hostnames="$2"
	oc get agent -n ${namespace} --no-headers -l "agent-install.openshift.io/bmh in ( ${hostnames} )"
}

export -f remote_agents

function installed_remote_agents() {
        namespace="$1"
        hostnames="$2"
        remote_agents ${namespace} ${hostnames} | grep Done
}

export -f installed_remote_agents

function wait_for_operator() {
   subscription="$1"
   namespace="${2:-}"
   set -x
   
   timeout=10m
   wait_for_resource "subscriptions.operators.coreos.com/${subscription}" "${namespace}"
   
    
   wait_for_field "subscriptions.operators.coreos.com/${subscription}" "${namespace}" '{..status.state}'
   oc wait -n "${namespace}" --for=jsonpath='{..status.state}'=AtLatestKnown "subscriptions.operators.coreos.com/${subscription}" --timeout=${timeout} -o json


   wait_for_field "subscriptions.operators.coreos.com/${subscription}" "${namespace}" '{..status.installedCSV}'
   csv=$(oc get subscriptions.operators.coreos.com/${subscription} --namespace=${namespace} -o jsonpath='{..status.installedCSV}')
   wait_for_condition "clusterserviceversions.operators.coreos.com/${csv}"  jsonpath='{.status.phase}'="Succeeded" "5m" "${namespace}"
 }

 function wait_for_field() {
   object="$1"
   namespace="$2"
   jsonPath="$3"
   retry="${4:-120}"
  
  counter=1
  echo "Waiting for ${object} in namespace ${namespace} with field ${jsonPath}"
  until [[ $(oc get "${object}"  --namespace="${namespace}" -o jsonpath="${jsonPath}" 2> /dev/null ) ]]
  do
    if [[ "${counter}" -eq "${retry}" ]]
    then
      echo "$(date --rfc-3339=seconds) ERROR: failed Waiting for ${object} in namespace ${namespace} with field ${jsonPath}"
      oc get ${object}  --namespace="${namespace}" -o json
      exit 1
      break 
    fi
    ((counter++)) && sleep 5
  done
 }


function wait_for_pod() {
    pod="$1"
    namespace="${2:-}"
    selector="${3:-}"

    oc wait -n "${namespace}" --all --for=condition=Ready pod --timeout=15m --output json --selector="${selector}"
}

function wait_for_pods(){
  namespace=$1

  if [[ $(oc wait --namespace "${namespace}" --all --for=condition=Ready pod --timeout 1m) ]]; then
    echo "All Pods in namespace ${namespace} are ready"}
  else
    echo "ERROR: Failed waiting for pods"
    # debug output
    oc get pods --namespace  "${namespace}"
    exit 1
  fi
}

function wait_for_deployment() {
    deployment="$1"
    namespace="${2}"
    timeout="${3}"

    echo "Waiting for (deployment) on namespace (${namespace}) with name (${deployment}) to be created..."
    for i in {1..40}; do
        oc get deployments.apps "${deployment}" --namespace="${namespace}" |& grep -ivE "(no resources found|not found)" && break || sleep 10
    done
    if [ $i -eq 40 ]; then
   echo "ERROR: failed Waiting for (deployment) on namespace (${namespace}) with name (${deployment}) to be created..."
      exit 1
    fi

    echo "Waiting for (deployment) on namespace (${namespace}) with name (${deployment}) to rollout..."

    wait_for_field "deployments.apps/${deployment}" "${namespace}" '{..status.availableReplicas}' "600" 
    REPLICAS=$(oc get deployments.apps --namespace="${namespace}" "${deployment}"  -o jsonpath='{..status.replicas}')
    wait_for_condition "deployments.apps/${deployment}"  jsonpath='{..status.availableReplicas}'="${REPLICAS}" "5m" "${namespace}"
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

function wait_for_condition() {
    object="$1"
    condition="$2"
    timeout="$3"
    namespace="${4:-}"
    selector="${5:-}"
    
    wait_for_resource "${object}" "${namespace}"

    echo "Waiting for (${object}) on namespace (${namespace}) with labels (${selector}) to become (${condition})..."
    oc wait -n "${namespace}" --for="${condition}"  "${object}" --timeout="${timeout}" --selector "${selector}" -o json
}

function wait_for_object_amount() {
    object="$1"
    amount="$2"
    interval="$3"
    namespace="${4:-}"

    until [ $(oc get ${object} -n "${namespace}" --no-headers | wc -l) -eq ${amount} ]; do
        sleep ${interval}
    done
    echo "done" $(oc get ${object} -n "${namespace}" --no-headers | wc -l)
}

function wait_for_cmd_amount() {
    amount="$1"
    interval="$2"
    shift 2
    cmd=$@

    until [ $(${cmd} "${@:4}" | wc -l) -eq ${amount} ]; do
        sleep ${interval}
    done
    echo "done" $(${cmd} "${@:4}" | wc -l)
}

function wait_for_boolean_field() {
    object="$1"
    field="$2"
    namespace="${3:-}"
    interval="${4:-10}"

    for i in {1..50}; do
        date --rfc-3339=seconds
        value=$(oc get -n ${namespace} ${object} -o custom-columns=field:${field} --no-headers)
        if [ "${value}" = "true" ]; then
            return 0
        fi

        sleep ${interval}
    done

    echo "Value of field ${field} of object ${object} under namespace ${namespace} has never become true"
    return 1
}

function wait_for_resource() {
    object="$1"
    namespace="$2"
    set +e

    counter=1
    echo "$(date --rfc-3339=seconds) Waiting for resource ${object} to be create in namespace ${namespace}"
    until [[ $(oc get "${object}"  --namespace="${namespace}" 2> /dev/null ) ]]
    do
      if [[ "${counter}" -eq 30 ]];
      then
        echo "$(date --rfc-3339=seconds) ERROR: failed Waiting for ${object} on namespace ${namespace}"
        oc get ${object}  --namespace="${namespace}" -o json
        exit 1
        break 
      fi
      ((counter++)) && sleep 2
    done
}

function get_image_without_tag() {
    # given "<registry>/<repository>/<project>:<tag>" or
    #       "<registry>/<repository>/<project>@sha256:<sha>" or
    #       "<registry>:<port>/<repository>/<project>:<tag>"
    # return "<registry>:<port>/<repository>/<project>"
    echo "${1%:*}" | cut -d@ -f1
}

function get_image_namespace() {
    # given "<registry>/<repository>/<project>:<tag>"
    # return "<registry>/<repository>"
    dirname "${1}"
}

function get_image_without_registry() {
    # given "<registry>/<repository>/<project>:<tag>"
    # return "<repository>/<project>:<tag>"
    echo "${1#*/}"
}

function get_image_namespace_without_registry() {
    # given "<registry>/<repository>/<project>:<tag>"
    # return "<repository>"
    get_image_namespace $(get_image_without_registry "${1}")
}

function get_image_repository_only() {
    # given "<registry>/<repository>/<project>:<tag>"
    # return "<repository>/<project>"
    get_image_without_registry $(get_image_without_tag "${1}")
}

function nth_ip() {
  network=$1
  idx=$2

  python -c "from ansible_collections.ansible.utils.plugins.filter import nthhost; print(nthhost.nthhost('"$network"', $idx))"
}

function retry() {
    attempts=5
    interval=1

    local OPTIND
    while getopts "a:i:" opt ; do
      case "${opt}" in
          a )
              attempts="${OPTARG}"
              ;;
          i )
              interval="${OPTARG}"
              ;;
          * )
              ;;
      esac
    done
    shift $((OPTIND-1))

    rc=0
    for attempt in $(seq "${attempts}")
    do
        echo "Attempt ${attempt}/${attempts} to execute \"$*\"..."

        if "$@"; then
            return 0
        else
            rc=$?
            echo "Failed with exit code ${rc}, retrying \"$*\"..."
            sleep "${interval}"
        fi
    done

    return ${rc}
}
