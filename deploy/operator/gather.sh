#!/usr/bin/env bash

set -xeo pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh

function gather_hive_data() {
  hive_dir="${LOGS_DEST}/hive"
  mkdir -p "${hive_dir}"
  oc get all -n "${HIVE_NAMESPACE}" > ${hive_dir}/oc_get_all.log || true

  oc logs --tail=-1 -n "${HIVE_NAMESPACE}" --selector control-plane=hive-operator > ${hive_dir}/hive-operator.log
  oc logs --tail=-1 -n "${HIVE_NAMESPACE}" --selector control-plane=controller-manager > ${hive_dir}/hive-controller-manager.log

  oc get events -n "${HIVE_NAMESPACE}" --sort-by=.metadata.creationTimestamp > ${hive_dir}/oc_get_events.log || true
}

function gather_olm_data() {
  oc get catalogsource "${ASSISTED_SERVICE_OPERATOR_CATALOG}" -n openshift-marketplace -o yaml > ${LOGS_DEST}/${ASSISTED_SERVICE_OPERATOR_CATALOG}.log
  oc get subscription assisted-service-operator -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/assisted-service-operator-subscription.log
  oc get installplan -n "${ASSISTED_NAMESPACE}" -o yaml  > ${LOGS_DEST}/oc_install_plan.log
}

function gather_cluster_data() {
  oc get nodes -o yaml > ${LOGS_DEST}/oc_get_nodes.yaml || true
  oc cluster-info > ${LOGS_DEST}/oc_cluster_info.log || true
  oc cluster-info dump > ${LOGS_DEST}/oc_cluster_info_dump.log || true
}

function gather_operator_data() {
  oc get all -n "${ASSISTED_NAMESPACE}" > ${LOGS_DEST}/oc_get_all.log || true
  oc get pods -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_pods.yaml || true
  oc get deployments -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_deployments.yaml || true
  oc get replicasets -n "${ASSISTED_NAMESPACE}" -o yaml > ${LOGS_DEST}/oc_get_replicasets.yaml || true

  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-service -c assisted-service > ${LOGS_DEST}/assisted-service.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-image-service -c assisted-image-service > ${LOGS_DEST}/assisted-image-service.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector app=assisted-service -c postgres > ${LOGS_DEST}/postgres.log
  oc logs --tail=-1 -n "${ASSISTED_NAMESPACE}" --selector control-plane=infrastructure-operator > ${LOGS_DEST}/infrastructure-operator.log

  oc get events -n "${ASSISTED_NAMESPACE}" --sort-by=.metadata.creationTimestamp > ${LOGS_DEST}/oc_get_events.log || true
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

    oc_items=$(oc get ${cr} -A -o json | jq -c '.items[]') || true
    readarray -t objects < <(echo $oc_items)
    for obj in "${objects[@]}"; do
      obj_name=$(echo ${obj} | jq -r .metadata.name)
      obj_namespace=$(echo ${obj} | jq -r .metadata.namespace)
      oc get "${cr}" -n "${obj_namespace}" "${obj_name}" -o yaml > "${work_dir}/${obj_name}_${obj_namespace}.yaml" || true
    done
  done

  oc logs --tail=-1 deployment/operator -n hypershift > "${capi_dir}/hypershift.log" || true

  # Detect namespace where the capi-provider lives. The specific name depends whether deployed standalone or via
  # Hypershift, that's why we grep for both.
  capi_provider_ns=$(oc get pods --no-headers -A -o jsonpath='{range .items[*]}{@.metadata.name}{" "}{@.metadata.namespace}{"\n"}' | egrep "capi-provider|cluster-api-provider-agent" | awk -F " " '{print $2}' | head -n1) || true
  capi_provider_dir="${capi_dir}/${capi_provider_ns}"
  mkdir -p "${capi_provider_dir}"

  oc get pods -n ${capi_provider_ns} -o=custom-columns=NAME:.metadata.name --no-headers | xargs -r -I {} sh -c "oc logs --tail=-1 {} -n ${capi_provider_ns} --all-containers > ${capi_provider_dir}/logs_{}.log" || true
  oc get pods -n ${capi_provider_ns} -o=custom-columns=NAME:.metadata.name --no-headers | xargs -r -I {} sh -c "oc get pods -o yaml {} -n ${capi_provider_ns} > ${capi_provider_dir}/pods_{}.yaml" || true

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
}

gather_all
