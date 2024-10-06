#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

non_root=$(if [ "${EUID}" != "0" ]; then echo "true"; else echo "false"; fi)

ROOTLESS_DEPLOYMENT="${ROOTLESS_DEPLOYMENT:-${non_root}}"
HUB_CLUSTER_NAME="${HUB_CLUSTER_NAME:-assisted-hub-cluster}"
MINIKUBE_DRIVER="${MINIKUBE_DRIVER:-podman}"
MINIKUBE_CPUS="${MINIKUBE_CPUS:-4}"
MINIKUBE_MEMORY="${MINIKUBE_MEMORY:-8192}"
MINIKUBE_DISK_SIZE="${MINIKUBE_DISK_SIZE:-50g}"
MINIKUBE_VERSION="${MINIKUBE_VERSION:-v1.33.1}"
SUDO=$(if [ -x "$(command -v sudo)" ]; then echo "sudo"; else echo ""; fi)

__dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

function check() {
	if command -v minikube > /dev/null 2>&1; then
		echo "'minikube' is installed. '$(minikube version | grep version: | cut -d " " -f 3)' will be used"
		return 0
	else
		echo "'minikube' executable is not avialable in PATH"
		return 1
	fi
}

function configure_minikube() {
    echo "Configuring minikube..."
    minikube config set WantUpdateNotification false
    minikube config set rootless $ROOTLESS_DEPLOYMENT
    if [ "$ROOTLESS_DEPLOYMENT" == "true" ]; then
        minikube config set container-runtime containerd
    fi
    minikube config set profile $HUB_CLUSTER_NAME
    minikube config set driver $MINIKUBE_DRIVER
    minikube config set cpus $MINIKUBE_CPUS
    minikube config set memory $MINIKUBE_MEMORY
    minikube config set disk-size $MINIKUBE_DISK_SIZE
}

function install() {
    if check; then
		return 0
	fi

    curl --retry 3 --connect-timeout 30 -Lo minikube https://storage.googleapis.com/minikube/releases/${MINIKUBE_VERSION}/minikube-linux-amd64
    ${SUDO} install minikube /usr/local/bin/minikube
    ${SUDO} chmod +x /usr/local/bin/minikube
    echo "Make sure /usr/local/bin is in your PATH"
    minikube version
    rm -f minikube
}

function create() {
    check

    if minikube profile list | grep $HUB_CLUSTER_NAME; then
        echo "Cluster already existing. Skipping creation"
        return 0
    fi

    configure_minikube
    minikube start -p $HUB_CLUSTER_NAME --force --wait-timeout=15m0s
    minikube status -p $HUB_CLUSTER_NAME
    minikube tunnel -p $HUB_CLUSTER_NAME --cleanup &> /dev/null &
}

function delete() {
    check

    minikube delete -p $HUB_CLUSTER_NAME --purge
}

function load_image() {
	check
	
    local image_path=$1
    local cluster_name=$2

    if [ -z "$image_path" ] || [ -z "$cluster_name" ]; then
        echo "Usage: load_image <image_path> <cluster_name>"
        return 1
    fi

    minikube image load -p $cluster_name $image_path
}

if [ $# -eq 0 ]; then
	echo "Usage: $__dir/minikube.sh (install|check|create|load_image)"
	exit 1
else
	$@
fi
