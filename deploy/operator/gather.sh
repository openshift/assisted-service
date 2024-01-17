#!/usr/bin/env bash

set -x

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh

function gather_hive_data() {
  hive_dir="${LOGS_DEST}/hive"
  mkdir -p "${hive_dir}"
  oc get all -n "${HIVE_NAMESPACE}" > ${hive_dir}/oc_get_all.log

  oc logs --tail=-1 -n "${HIVE_NAMESPACE}" --selector control-plane=hive-operator > ${hive_dir}/hive-operator.log
  oc logs --tail=-1 -n "${HIVE_NAMESPACE}" --selector control-plane=controller-manager > ${hive_dir}/hive-controller-manager.log

  oc get events -n "${HIVE_NAMESPACE}" --sort-by=.metadata.creationTimestamp > ${hive_dir}/oc_get_events.log
}

function gather_olm_data() {
  oc get catalogsource "${ASSISTED_SERVICE_OPERATOR_CATALOG}" -n openshift-marketplace -o yaml > ${LOGS_DEST}/${ASSISTED_SERVICE_OPERATOR_CATALOG}.log
  oc get subscription assisted-service-operator -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/assisted-service-operator-subscription.log
  oc get installplan -n "${ASSISTED_NAMESPACE}" -o yaml  > ${LOGS_DEST}/oc_install_plan.log
}

function gather_cluster_data() {
  oc get nodes -o yaml > ${LOGS_DEST}/oc_get_nodes.yaml
  oc cluster-info > ${LOGS_DEST}/oc_cluster_info.log
  oc cluster-info dump > ${LOGS_DEST}/oc_cluster_info_dump.log
}

function gather_operator_data() {
  oc get all -n "${ASSISTED_NAMESPACE}" > ${LOGS_DEST}/oc_get_all.log
  oc get pods -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_pods.yaml
  oc get deployments -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_deployments.yaml
  oc get replicasets -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_replicasets.yaml

  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-service -c assisted-service > ${LOGS_DEST}/assisted-service.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-image-service -c assisted-image-service > ${LOGS_DEST}/assisted-image-service.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-service -c postgres > ${LOGS_DEST}/postgres.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector control-plane=infrastructure-operator > ${LOGS_DEST}/infrastructure-operator.log

  oc get events -n "${ASSISTED_NAMESPACE}" --sort-by=.metadata.creationTimestamp > ${LOGS_DEST}/oc_get_events.log
}

function gather_agentclusterinstall_data() {
  readarray -t agentclusterinstall_objects < <(oc get agentclusterinstall -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[]')
  for agentclusterinstall in "${agentclusterinstall_objects[@]}"; do
    agentclusterinstall_name=$(echo ${agentclusterinstall} | jq -r .metadata.name)
    agentclusterinstall_namespace=$(echo ${agentclusterinstall} | jq -r .metadata.namespace)

    cluster_dir="${LOGS_DEST}/${agentclusterinstall_namespace}_${agentclusterinstall_name}"
    mkdir -p "${cluster_dir}"

    oc get agentclusterinstall -n ${agentclusterinstall_namespace} ${agentclusterinstall_name} -o yaml > "${cluster_dir}/agentclusterinstall.yaml"

    events_url=$(echo ${agentclusterinstall} | jq -r .status.debugInfo.eventsURL)
    if [ -n "${events_url}" ] && [ "${events_url}" != null ]; then
      curl -ks "${events_url}" | jq '.' > "${cluster_dir}/cluster_events.json"
    fi

    logs_url=$(echo ${agentclusterinstall} | jq -r .status.debugInfo.logsURL)
    if [ -n "${logs_url}" ] && [ "${logs_url}" != null ]; then
      curl "${logs_url}" -k -o "${cluster_dir}/logs.tar.gz"
    fi
  done
}

function gather_bmh_data() {
  bmh_dir="${LOGS_DEST}/baremetalhosts"
  mkdir -p "${bmh_dir}"

  readarray -t bmh_objects < <(oc get baremetalhost -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[]')
  for bmh in "${bmh_objects[@]}"; do
    host_name=$(echo ${bmh} | jq -r .metadata.name)
    oc get baremetalhost -n "${SPOKE_NAMESPACE}" "${host_name}" -o yaml > "${bmh_dir}/${host_name}.yaml"
    # in ocp 4.11 and above, each BMH has a  matching preprovisoning image CR that contains the image information
    oc get preprovisioningimage -n "${SPOKE_NAMESPACE}" "${host_name}" -o yaml > "${bmh_dir}/ppi_${host_name}.yaml"
  done
}

function gather_infraenv_data() {
  infraenv_dir="${LOGS_DEST}/infraenvs"
  mkdir -p "${infraenv_dir}"

  readarray -t infraenv_objects < <(oc get infraenv -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[]')
  for infraenv in "${infraenv_objects[@]}"; do
    infraenv_name=$(echo ${infraenv} | jq -r .metadata.name)
    oc get infraenv -n "${SPOKE_NAMESPACE}" "${infraenv_name}" -o yaml > "${infraenv_dir}/${infraenv_name}.yaml"
  done
}

function gather_agent_data() {
  agent_dir="${LOGS_DEST}/agents"
  mkdir -p "${agent_dir}"

  readarray -t agent_objects < <(oc get agents.agent-install.openshift.io -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[]')
  for agent in "${agent_objects[@]}"; do
    agent_name=$(echo ${agent} | jq -r .metadata.name)
    oc get agents.agent-install.openshift.io -n "${SPOKE_NAMESPACE}" "${agent_name}" -o yaml > "${agent_dir}/${agent_name}.yaml"
  done
}

function gather_clusterdeployment_data() {
  cd_dir="${LOGS_DEST}/clusterdeployment"
  mkdir -p "${cd_dir}"

  readarray -t cd_objects < <(oc get clusterdeployments.hive.openshift.io -n ${SPOKE_NAMESPACE} -o json | jq -c '.items[]')
  for cd in "${cd_objects[@]}"; do
    cd_name=$(echo ${cd} | jq -r .metadata.name)
    oc get clusterdeployments.hive.openshift.io -n "${SPOKE_NAMESPACE}" "${cd_name}" -o yaml > "${cd_dir}/${cd_name}.yaml"
  done
}

function gather_imageset_data() {
  imageset_dir="${LOGS_DEST}/imageset"
  mkdir -p "${imageset_dir}"

  readarray -t imageset_objects < <(oc get clusterimagesets.hive.openshift.io -o json | jq -c '.items[]')
  for is in "${imageset_objects[@]}"; do
    is_name=$(echo ${is} | jq -r .metadata.name)
    oc get clusterimagesets.hive.openshift.io "${is_name}" -o yaml > "${imageset_dir}/${is_name}.yaml"
  done
}

function gather_capi_data() {
  CAPI_CRS=(agentmachine agentcluster cluster machine machinedeployment machineset nodepool hostedcluster hostedcontrolplane)
  capi_dir="${LOGS_DEST}/capi"
  mkdir -p "${capi_dir}"

  for cr in "${CAPI_CRS[@]}"; do
    work_dir="${capi_dir}/${cr}"
    mkdir -p "${work_dir}"

    oc_items=$(oc get ${cr} -A -o json | jq -c '.items[]')
    readarray -t objects < <(echo $oc_items)
    for obj in "${objects[@]}"; do
      obj_name=$(echo ${obj} | jq -r .metadata.name)
      obj_namespace=$(echo ${obj} | jq -r .metadata.namespace)
      oc get "${cr}" -n "${obj_namespace}" "${obj_name}" -o yaml > "${work_dir}/${obj_name}_${obj_namespace}.yaml"
    done
  done

  oc get po -n hypershift -o yaml > "${capi_dir}/hypershift_pods.yaml"
  oc logs --tail=-1 deployment/operator -n hypershift > "${capi_dir}/hypershift.log"

  # Detect namespace where the capi-provider lives. The specific name depends whether deployed standalone or via
  # Hypershift, that's why we grep for both.
  capi_provider_ns=$(oc get pods --no-headers -A -o jsonpath='{range .items[*]}{@.metadata.name}{" "}{@.metadata.namespace}{"\n"}' | egrep "capi-provider|cluster-api-provider-agent" | awk -F " " '{print $2}' | head -n1)
  capi_provider_dir="${capi_dir}/${capi_provider_ns}"
  mkdir -p "${capi_provider_dir}"

  oc get pods -n ${capi_provider_ns} -o=custom-columns=NAME:.metadata.name --no-headers | xargs -r -I {} sh -c "oc logs --tail=-1 {} -n ${capi_provider_ns} --all-containers > ${capi_provider_dir}/logs_{}.log"
  oc get pods -n ${capi_provider_ns} -o=custom-columns=NAME:.metadata.name --no-headers | xargs -r -I {} sh -c "oc get pods -o yaml {} -n ${capi_provider_ns} > ${capi_provider_dir}/pods_{}.yaml"

}

function gather_hypershift_data() {
  hypershift_dir="${LOGS_DEST}/hypershift"
  mkdir -p "${hypershift_dir}"

  oc get all -n "${SPOKE_NAMESPACE}" > ${hypershift_dir}/oc_get_all.log
  oc get configmap -n "${SPOKE_NAMESPACE}" assisted-service -o yaml > ${hypershift_dir}/oc_get_configmap.yaml
  oc get events -n "${SPOKE_NAMESPACE}" --sort-by=.metadata.creationTimestamp > ${hypershift_dir}/oc_get_events.log

  oc logs --tail=-1 -n "${SPOKE_NAMESPACE}" --selector app=assisted-service -c assisted-service > ${hypershift_dir}/assisted-service.log
  oc logs --tail=-1 -n "${SPOKE_NAMESPACE}" --selector app=assisted-image-service -c assisted-image-service > ${hypershift_dir}/assisted-image-service.log
  oc logs --tail=-1 -n "${SPOKE_NAMESPACE}" --selector app=assisted-service -c postgres > ${hypershift_dir}/postgres.log
  oc logs --tail=-1 -n "${SPOKE_NAMESPACE}" --selector app=agentinstalladmission > ${hypershift_dir}/agentinstalladmission.log
  oc logs --tail=-1 -n "${SPOKE_NAMESPACE}" --selector control-plane=infrastructure-operator > ${hypershift_dir}/infrastructure-operator.log

  hub_dir="${hypershift_dir}/hub_cluster"
  mkdir -p "${hub_dir}"
  CRS=(pods baremetalhosts deployments clusterdeployments replicasets hypershiftagentserviceconfigs)
  for cr in "${CRS[@]}"; do
    oc get "${cr}" -n "${SPOKE_NAMESPACE}" -o yaml > "${hub_dir}/oc_get_${cr}.yaml"
  done

  spoke_dir="${hypershift_dir}/spoke_cluster"
  mkdir -p "${spoke_dir}"

  SPOKE_KUBECONFIG=/tmp/spoke-cluster-kubeconfig
  SPOKE_CLUSTER_NAME=$(oc get hostedcluster -n "${SPOKE_NAMESPACE}" -o custom-columns=:.metadata.name --no-headers)
  oc extract -n "${SPOKE_NAMESPACE}" secret/"${SPOKE_CLUSTER_NAME}"-admin-kubeconfig --to=- > "${SPOKE_KUBECONFIG}"
  if [ -s "${SPOKE_KUBECONFIG}" ]; then
    CRS=(agents infraenvs clusterdeployments agentclusterinstalls clusterimagesets)
    for cr in "${CRS[@]}"; do
      oc --kubeconfig "${SPOKE_KUBECONFIG}" get "${cr}" -n "${SPOKE_NAMESPACE}" -o yaml > "${spoke_dir}/oc_get_${cr}.yaml"
    done

    oc --kubeconfig "${SPOKE_KUBECONFIG}" get nodes -o yaml > "${spoke_dir}/oc_get_nodes.yaml"
    mkdir -p "${spoke_dir}"/pods
    oc --kubeconfig "${SPOKE_KUBECONFIG}" get po -A > "${spoke_dir}"/pods/oc_get_pods.log
    oc --kubeconfig "${SPOKE_KUBECONFIG}" get po -A -o custom-columns=:metadata.namespace,:metadata.name --no-headers | while read po ; do
      ns=${echo $po | awk '{print $1}'}
      spoke_pod_name=${echo $po | awk '{print $2}'}
      oc --kubeconfig "${SPOKE_KUBECONFIG}" get po -o yaml -n $ns $spoke_pod_name > "${spoke_dir}/pods/pod_${spoke_pod_name}.yaml"
      oc --kubeconfig "${SPOKE_KUBECONFIG}" logs -n $ns $spoke_pod_name > "${spoke_dir}/pods/logs_${spoke_pod_name}.log"
    done
  fi
  
  if [ ! -z "${SPOKE_CLUSTER_NAME}" ] ; then
    hypershift_cli
    hypershift dump cluster --name "${SPOKE_CLUSTER_NAME}" --namespace "${SPOKE_NAMESPACE}" --dump-guest-cluster=true --artifact-dir "${spoke_dir}/artifacts"
  fi
}

# Download Hypershift CLI
function hypershift_cli() {
  HYPERSHIFT_IMAGE="${HYPERSHIFT_IMAGE:-quay.io/hypershift/hypershift-operator:4.11}"
  id=$(podman create $HYPERSHIFT_IMAGE)
  mkdir -p ./hypershift-cli
  podman cp $id:/usr/bin/hypershift ./hypershift-cli
  export PATH="$PATH":"$PWD"/hypershift-cli
}

function gather_all() {
  gather_cluster_data
  gather_hive_data
  gather_olm_data
  gather_operator_data
  gather_agentclusterinstall_data
  gather_bmh_data
  gather_infraenv_data
  gather_agent_data
  gather_clusterdeployment_data
  gather_imageset_data
  gather_capi_data
  gather_hypershift_data
}

gather_all
