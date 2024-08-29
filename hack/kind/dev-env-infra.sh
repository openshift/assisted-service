#!/usr/bin/env bash

CERTMANAGER_VERSION="1.15.1"

# cert-manager installation
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/v${CERTMANAGER_VERSION}/cert-manager.yaml"
echo "Waiting for cert-manager ..."
for app in webhook cainjector cert-manager; do
  while ! kubectl wait -n cert-manager -l app="${app}" --for=condition=Ready pods --timeout=300s > /dev/null 2>&1; do
    sleep 10
  done
done

# Create required CRDs
kubectl apply -f "$(dirname "${0}")/../crds/"

# Deploy nginx ingress controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
# label nodes as ingress-ready
while read -r node; do
  kubectl label node "${node}" ingress-ready='true'
done < <(kubectl get nodes -o 'jsonpath={.items[0].metadata.name}{"\n"}')

# assisted-service installation
kubectl apply -k https://github.com/openshift/assisted-service//config/default
kubectl apply -f deploy/kind/agent-svc-config.yaml
kubectl apply -f deploy/kind/assisted-service-portmap.yaml
