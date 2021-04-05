#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

CLUSTER_CONTEXT=$(${KUBECTL} config current-context | cut -d'-' -f1)

case "${CLUSTER_CONTEXT}" in
  "minikube")
    eval $(SHELL=${SHELL:-/bin/sh} minikube docker-env) && \
        docker build ${CONTAINER_BUILD_PARAMS} -f Dockerfile.assisted-service . -t ${SERVICE}
    ;;

  "k3d")
    docker build ${CONTAINER_BUILD_PARAMS} -f Dockerfile.assisted-service . -t ${SERVICE}
    k3d image import ${SERVICE}
    ;;

  *)
    echo "Unknown cluster context ${CLUSTER_CONTEXT}"
    ;;
esac
