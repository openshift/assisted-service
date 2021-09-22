#!/bin/bash
source ./common.sh
source ./utils.sh

echo "************ baremetalds assisted operator upgrade command ************"
export CHANNEL="${CHANNEL_UPGRADE_OVERRIDE:-alpha}"

echo "## subscription spec.channel before upgrade"
oc get subscription assisted-service-operator -n "${ASSISTED_NAMESPACE}" -o=jsonpath='{.spec.channel}{"\n"}'

# Upgrade the operator via OLM
oc patch subscription assisted-service-operator -n "${ASSISTED_NAMESPACE}" --type merge -p "{\"spec\":{\"channel\":\"$CHANNEL\"}}"

# wait for OLM to update the operator and pods to be recreated
wait_for_condition "pod" "Ready" "22m" "${ASSISTED_NAMESPACE}"  "app=assisted-image-service"
wait_for_condition "pod" "Ready" "22m" "${ASSISTED_NAMESPACE}"  "app=assisted-service"

echo "## subscription spec.channel after upgrade"
oc get subscription assisted-service-operator -n "${ASSISTED_NAMESPACE}" -o=jsonpath='{.spec.channel}{"\n"}'

echo 'Assisted Installer operator successfully upgraded!'

echo "## Verifying if the cluster installed before upgrade is accessible..."
oc get agentclusterinstall -n "${SPOKE_NAMESPACE}"
oc get clusterdeployment -n "${SPOKE_NAMESPACE}"
oc get cd assisted-test-cluster -n "${SPOKE_NAMESPACE}" -o template --template 'cluster.spec.installed = {{.spec.installed}} {{"\n"}}'
oc get agentclusterinstall assisted-agent-cluster-install -n "${SPOKE_NAMESPACE}"  -o=jsonpath='{.status.debugInfo.eventsURL}'

echo "## Cleanup to use the same extra worker for installing a SNO after the upgrade"
virsh destroy ostest_extraworker_0
qemu-img create -f qcow2 /opt/dev-scripts/pool/ostest_extraworker_0.qcow2 120G
oc delete bmh ostest-extraworker-0 -n "${SPOKE_NAMESPACE}"
oc delete agent --all -n "${SPOKE_NAMESPACE}"
