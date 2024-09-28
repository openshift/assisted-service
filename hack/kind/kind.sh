#!/usr/bin/env bash

set -o nounset
set -o errexit
set -o pipefail

KIND_VERSION="0.23.0"

__dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

function check() {
	if command -v kind > /dev/null 2>&1; then
		echo "'Kind' is installed. '$(kind --version)' will be used"
		return 0
	else
		echo "'Kind' executable is not avialable in PATH"
		return 1
	fi
}

function install() {
	if check; then
		return 0
	fi

	echo "Installing kind $KIND_VERSION..."
	sudo curl --retry 5 --connect-timeout 30 -L https://kind.sigs.k8s.io/dl/v$KIND_VERSION/kind-linux-amd64 -o /usr/local/bin/kind
	sudo chmod +x /usr/local/bin/kind
	echo "Installed successfully!"
}

function create() {
	check

	if ! kind get clusters | grep assisted-installer-kind ; then
		kind create cluster --config $__dir/kind-config.yaml
	else
		echo "Cluster already existing. Skipping creation"
	fi
}

function delete() {
	check

	kind delete cluster --name=assisted-installer-kind
}

function load_kind_image() {
	check
	
    local image_path=$1
    local cluster_name=$2

    if [ -z "$image_path" ] || [ -z "$cluster_name" ]; then
        echo "Usage: load_kind_image <image_path> <cluster_name>"
        return 1
    fi

    kind load image-archive $image_path --name=$cluster_name
}

if [ $# -eq 0 ]; then
	echo "Usage: $__dir/kind.sh (install|check|create)"
	exit 1
else
	$@
fi
