#!/bin/bash

echo "************ baremetalds assisted operator upgrade command ************"
# export INDEX_IMAGE="${INDEX_IMAGE:-quay.io/ocpmetal/assisted-service-index:latest}"

echo "## catalogsource spec.image before upgrade"
oc get catalogsource assisted-service-operator-catalog -n openshift-marketplace -o=jsonpath='{.spec.image}{"\n"}'

# Upgrade the operator
oc patch catalogsource assisted-service-operator-catalog -n openshift-marketplace --type merge -p "{\"spec\":{\"image\":\"$INDEX_IMAGE\"}}"
# wait for olm to update the operator and pods to be recreated
sleep 15m

echo "## catalogsource spec.image after upgrade"
oc get catalogsource assisted-service-operator-catalog -n openshift-marketplace -o=jsonpath='{.spec.image}{"\n"}'

echo "## Verifying if the cluster installed before upgrade is accessible..."
oc get agentclusterinstall -n assisted-spoke-cluster
oc get clusterdeployment -n assisted-spoke-cluster
oc get cd assisted-test-cluster -n assisted-spoke-cluster -o template --template 'cluster.spec.installed = {{.spec.installed}} {{"\n"}}'
oc get agentclusterinstall assisted-agent-cluster-install -n assisted-spoke-cluster  -o=jsonpath='{.status.debugInfo.eventsURL}'

echo "## Cleanup to use the same extra worker for installing a SNO after the upgrade"
virsh destroy ostest_extraworker_0
qemu-img create -f qcow2 /opt/dev-scripts/pool/ostest_extraworker_0.qcow2 120G
oc delete bmh ostest-extraworker-0 -n assisted-spoke-cluster
oc delete agent --all -n assisted-spoke-cluster
