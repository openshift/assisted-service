#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

CACHE_DIR=/tmp/.cache

mkdir -p ${CACHE_DIR}

function get_package_manager() {
    PACKAGE_MANAGER=$( command -v dnf &> /dev/null && echo "dnf" || echo "yum")
    echo $PACKAGE_MANAGER
}

function print_help() {
  ALL_FUNCS="golang|assisted_service|hive_from_upstream|print_help"
  echo "Usage: bash ${0} (${ALL_FUNCS})"
}

function download() {
    # This function will leverage buildkit cache, and avoid downloading the file again
    # if we already have a file with the same name in the cache directory
    # Buildkit mounts /tmp/.cache as cache dir
    url=$1
    output=$2
    cached_filename=$(echo -n "$url" | sha256sum | awk '{ print $1 }')
    cache_full_path="${CACHE_DIR}/${cached_filename}"
    [ -f "${cache_full_path}" ] || curl --retry 5 -L "$url" -o "$cache_full_path"
    cp "$cache_full_path" "${output}"
}

function golang() {
  echo "Installing golang..."

  download https://storage.googleapis.com/golang/getgo/installer_linux /tmp/golang_installer

  chmod u+x /tmp/golang_installer
  /tmp/golang_installer
  rm /tmp/golang_installer

  echo "Activating go command on current shell..."
  set +u
  source /root/.bash_profile
  set -u
}

function spectral() {
  echo "Installing spectral..."
  download https://github.com/stoplightio/spectral/releases/download/v5.9.1/spectral-linux /usr/local/bin/spectral
  chmod +x /usr/local/bin/spectral
}

function jq() {
  echo "Installing jq..."
  download https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 /usr/local/bin/jq
  chmod +x /usr/local/bin/jq
}

function awscli() {
  echo "Installing aws-cli..."
  download "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" "/tmp/awscliv2.zip" && unzip /tmp/awscliv2.zip && \
  ./aws/install && rm -f /tmp/awscliv2.zip
  rm -rf ./aws
}

function podman_remote() {
  # podman-remote 4 cannot run against podman server 3 so installing them both side by side
  download https://github.com/containers/podman/releases/download/v3.4.4/podman-remote-static.tar.gz "podman-remote3.tar.gz" && \
  tar -zxvf podman-remote3.tar.gz && \
  mv podman-remote-static /usr/local/bin/podman-remote3 && \
  rm -f podman-remote3.tar.gz

  download https://github.com/containers/podman/releases/download/v4.1.1/podman-remote-static.tar.gz "podman-remote4.tar.gz" && \
  tar -zxvf podman-remote4.tar.gz && \
  mv podman-remote-static /usr/local/bin/podman-remote4 && \
  rm -f podman-remote4.tar.gz
}

function test_tools() {
  go get github.com/onsi/ginkgo/ginkgo@v1.16.4 \
      github.com/golang/mock/mockgen@v1.6.0 \
      github.com/vektra/mockery/.../@v1.1.2 \
      gotest.tools/gotestsum@v1.6.3 \
      github.com/axw/gocov/gocov \
      github.com/AlekSi/gocov-xml@v0.0.0-20190121064608-3a14fb1c4737
}

function assisted_service() {
  ARCH=$(case $(arch) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(arch) ;; esac)

  latest_kubectl_version=$(curl --retry 5 -L -s https://dl.k8s.io/release/stable.txt)
  download "https://dl.k8s.io/release/${latest_kubectl_version}/bin/linux/amd64/kubectl" /tmp/kubectl && \
    install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
    rm -f /tmp/kubectl

  $(get_package_manager) install -y --setopt=skip_missing_names_on_install=False \
    unzip diffutils python3-pip genisoimage skopeo

  jq

  awscli

  spectral

  test_tools

  OS=$(uname | awk '{print tolower($0)}')
  OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.10.1
  download ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk
  chmod +x /usr/local/bin/operator-sdk

  go get golang.org/x/tools/cmd/goimports@v0.1.5 \
        sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.2

  python3 -m venv ${VIRTUAL_ENV:-/opt/venv}
  python3 -m pip install --upgrade pip
  python3 -m pip install -r ./dev-requirements.txt
}

function hive_from_upstream() {
  golang
}

declare -F $@ || (print_help && exit 1)

"$@"
