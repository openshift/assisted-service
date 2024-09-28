#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

function create() {
    case ${TARGET} in
        minikube)
            $__dir/minikube/minikube.sh create
            ;;
        kind)
            $__dir/kind/kind.sh create
            ;;
        *)
            echo "Unknown deploy target ${TARGET}!";
            exit 0
            ;;
    esac
}

function delete() {
    case ${TARGET} in
        minikube)
            $__dir/minikube/minikube.sh delete
            ;;
        kind)
            $__dir/kind/kind.sh delete
            ;;
        onprem)
            make clean-onprem
            ;;
        *)
            echo "Unknown deploy target ${TARGET}!";
            exit 0
            ;;
    esac
}

function install() {
    case ${TARGET} in
        minikube)
            $__dir/minikube/minikube.sh install
            ;;
        kind)
            $__dir/kind/kind.sh install
            ;;
        *)
            echo "Unknown deploy target ${TARGET}!";
            exit 0
            ;;
    esac
}

function load_image() {
    case ${TARGET} in
        minikube)
            $__dir/minikube/minikube.sh load_image $1 $2
            ;;
        kind)
            $__dir/kind/kind.sh load_image $1 $2
            ;;
        *)
            echo "Unknown deploy target ${TARGET}!";
            exit 0
            ;;
    esac
}

if [ $# -eq 0 ]; then
	echo "Usage: $__dir/hub_cluster.sh (install|delete|create|load_image)"
	exit 1
else
	$@
fi
