
#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "${DISKS:-}" ]; then
    export DISKS=$(echo sd{b..f})
fi

kubectl delete namespace assisted-installer
kubectl delete agentserviceconfigs.agent-install.openshift.io agent
kubectl patch localvolume -n openshift-local-storage assisted-service -p '{"metadata":{"finalizers": null}}' --type=merge
kubectl delete localvolume -n openshift-local-storage assisted-service
kubectl delete catalogsource assisted-service-catalog -n openshift-marketplace

${__dir}/libvirt_disks.sh destroy
kubectl get pv -o=name | xargs -r kubectl delete
