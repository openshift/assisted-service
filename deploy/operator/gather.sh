#!/usr/bin/env bash

set -xeo pipefail

function gather_operator_data() {
  oc cluster-info > ${LOGS_DEST}/k8s_cluster_info.log
  oc get all -n assisted-installer > ${LOGS_DEST}/k8s_get_all.log || true

  oc logs -n assisted-installer --selector app=assisted-service -c assisted-service > ${LOGS_DEST}/assisted-service.log
  oc logs -n assisted-installer --selector app=assisted-service -c postgres > ${LOGS_DEST}/postgres.log
  oc logs -n assisted-installer --selector control-plane=assisted-service-operator > ${LOGS_DEST}/assisted-service-operator.log

  oc get events -n assisted-installer --sort-by=.metadata.creationTimestamp > ${LOGS_DEST}/k8s_events.log || true
}

function gather_agentclusterinstall_data() {
  readarray -t agentclusterinstall_objects < <(oc get agentclusterinstall -A -o json | jq -c '.items[]')
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
      curl "${logs_url}" -o "${cluster_dir}/logs.tar.gz"
    fi
  done
}

function gather_bmh_data() {
  bmh_dir="${LOGS_DEST}/baremetalhosts"
  mkdir -p "${bmh_dir}"

  readarray -t bmh_objects < <(oc get baremetalhost -n assisted-installer -o json | jq -c '.items[]')
  for bmh in "${bmh_objects[@]}"; do
    host_name=$(echo ${bmh} | jq -r .metadata.name)
    oc get baremetalhost -n assisted-installer "${host_name}" -o yaml > "${bmh_dir}/${host_name}.yaml"
  done
}

function gather_infraenv_data() {
  infraenv_dir="${LOGS_DEST}/infraenvs"
  mkdir -p "${infraenv_dir}"

  readarray -t infraenv_objects < <(oc get infraenv -n assisted-installer -o json | jq -c '.items[]')
  for infraenv in "${infraenv_objects[@]}"; do
    infraenv_name=$(echo ${infraenv} | jq -r .metadata.name)
    oc get infraenv -n assisted-installer "${infraenv_name}" -o yaml > "${infraenv_dir}/${infraenv_name}.yaml"
  done
}

function gather_all() {
  gather_operator_data
  gather_agentclusterinstall_data
  gather_bmh_data
  gather_infraenv_data
}

gather_all
