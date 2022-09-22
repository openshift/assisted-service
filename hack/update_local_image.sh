#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

source hack/utils.sh
CLUSTER_CONTEXT=$(${KUBECTL} config current-context | cut -d'-' -f1)

# This file is responsible to the update the local k8s (for example minikube) with the new image.
# For minikube the faster option is to build the image after changing the docker environment to the minikube docker.
# For k3d we can just import the image.
# TODO Create a local docker registry push the image to it and remove this file
case "${CLUSTER_CONTEXT}" in
  "minikube")

    # The kubectl current-context (CLUSTER_CONTEXT) indicates we're pointing at a minikube cluster, but there's
    # no available minikube binary. This typically happens when running on a skipper container running on top
    # of macOS where the minikube command cannot be mapped from the host to the skipper container (since the
    # skipper container expects a binary compiled for Linux).
    if ! command -v minikube &> /dev/null
    then
      # The SKIPPER_UID environment variable is an indication that we are running on a skipper container.
      # We don't want to change the user's host but to ask the user to install.
      if ! running_from_skipper; then
        echo "ERROR: minikube command is not installed or not in your PATH"
        exit 1
      fi

      # If it's in skipper, it's fine to install minikube for them since it's just a disposable container
      # We download and install the latest minikube RPM on the skipper container.
      # TODO: This may cause problems when the latest minikube version doesn't match the one installed on the
      # mac host, but it's better than nothing for now
      echo "minikube is not mapped on skipper (probably a MACOS) - installing"
      curl --retry 5 -LO https://storage.googleapis.com/minikube/releases/latest/minikube-latest.x86_64.rpm
      rpm -ivh minikube-latest.x86_64.rpm
    fi

    make update-${DEBUG_SERVICE:+debug-}minimal
    minikube image load ${SERVICE}
    ;;

  "k3d")
    make update-${DEBUG_SERVICE:+debug-}minimal
    k3d image import ${SERVICE}
    ;;

  *)
    echo "Unknown cluster context ${CLUSTER_CONTEXT}"
    ;;
esac
