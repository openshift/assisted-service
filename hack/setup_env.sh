#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

function get_package_manager() {
    PACKAGE_MANAGER=$( command -v dnf &> /dev/null && echo "dnf" || echo "yum")
    echo $PACKAGE_MANAGER
}

function print_help() {
  ALL_FUNCS="golang|assisted_service|hive_from_upstream|print_help"
  echo "Usage: bash ${0} (${ALL_FUNCS})"
}

function golang() {
  echo "Installing golang..."
  curl --retry 5 -L https://storage.googleapis.com/golang/getgo/installer_linux -o /tmp/golang_installer
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
  curl --retry 5 -L https://github.com/stoplightio/spectral/releases/download/v5.9.1/spectral-linux -o /usr/local/bin/spectral
  chmod +x /usr/local/bin/spectral
}

function jq() {
  echo "Installing jq..."
  curl --retry 5 -L https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 --output /usr/local/bin/jq
  chmod +x /usr/local/bin/jq
}

function awscli() {
  echo "Installing aws-cli..."
  curl --retry 5 -L "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "/tmp/awscliv2.zip" && unzip /tmp/awscliv2.zip && \
  ./aws/install && rm -f /tmp/awscliv2.zip
}

function podman_remote() {
  # podman-remote 4 cannot run against podman server 3 so installing them both side by side
  curl --retry 5 -L https://github.com/containers/podman/releases/download/v3.4.4/podman-remote-static.tar.gz -o "podman-remote3.tar.gz" && \
  tar -zxvf podman-remote3.tar.gz && \
  mv podman-remote-static /usr/local/bin/podman-remote3 && \
  rm -f podman-remote3.tar.gz

  curl --retry 5 -L https://github.com/containers/podman/releases/download/v4.1.1/podman-remote-static.tar.gz -o "podman-remote4.tar.gz" && \
  tar -zxvf podman-remote4.tar.gz && \
  mv podman-remote-static /usr/local/bin/podman-remote4 && \
  rm -f podman-remote4.tar.gz
}

function test_tools() {
  go install github.com/onsi/ginkgo/ginkgo@v1.16.4
  go install github.com/golang/mock/mockgen@v1.6.0
  go install github.com/vektra/mockery/v2@v2.12.3
  go install gotest.tools/gotestsum@v1.6.3
  go install github.com/axw/gocov/gocov@latest
  go install github.com/AlekSi/gocov-xml@v0.0.0-20190121064608-3a14fb1c4737
}

function assisted_service() {
  ARCH=$(case $(arch) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(arch) ;; esac)

  latest_kubectl_version=$(curl --retry 5 -L -s https://dl.k8s.io/release/stable.txt)
  curl --retry 5 -L "https://dl.k8s.io/release/${latest_kubectl_version}/bin/linux/amd64/kubectl" -o /tmp/kubectl && \
    install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
    rm -f /tmp/kubectl

  $(get_package_manager) install -y --setopt=skip_missing_names_on_install=False \
    unzip diffutils python3-pip genisoimage skopeo && dnf clean all && rm -rf /var/cache/yum

  jq

  awscli

  spectral

  test_tools

  OS=$(uname | awk '{print tolower($0)}')
  OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.10.1
  curl --retry 5 -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
  chmod +x operator-sdk_${OS}_${ARCH}
  install operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk

  go install golang.org/x/tools/cmd/goimports@v0.1.5
  go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.2

  python3 -m venv ${VIRTUAL_ENV:-/opt/venv}
  python3 -m pip install --upgrade pip
  python3 -m pip install --no-cache-dir -r ./dev-requirements.txt
}

function hive_from_upstream() {
  golang
}

declare -F $@ || (print_help && exit 1)

"$@"
