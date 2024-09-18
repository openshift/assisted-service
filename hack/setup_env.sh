#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

function print_help() {
  ALL_FUNCS="assisted_service|print_help"
  echo "Usage: bash ${0} (${ALL_FUNCS})"
}

function spectral() {
  echo "Installing spectral..."
  curl --retry 5 --connect-timeout 30 -sL https://github.com/stoplightio/spectral/releases/download/v5.9.1/spectral-linux --output /usr/local/bin/spectral
  chmod +x /usr/local/bin/spectral
}

function jq() {
  echo "Installing jq..."
  curl --retry 5 --connect-timeout 30 -sL https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 --output /usr/local/bin/jq
  chmod +x /usr/local/bin/jq
}

function awscli() {
  echo "Installing aws-cli..."
  curl --retry 5 --connect-timeout 30 -sL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" --output "/tmp/awscliv2.zip"
  unzip -q /tmp/awscliv2.zip
  ./aws/install
  rm -f /tmp/awscliv2.zip
}

function podman_remote() {
  # podman-remote 4 cannot run against podman server 3 so installing them both side by side
  curl --retry 5 --connect-timeout 30 -L https://github.com/containers/podman/releases/download/v3.4.4/podman-remote-static.tar.gz -o "podman-remote3.tar.gz"
  tar -zxvf podman-remote3.tar.gz
  mv podman-remote-static /usr/local/bin/podman-remote3
  rm -f podman-remote3.tar.gz

  curl --retry 5 --connect-timeout 30 -L https://github.com/containers/podman/releases/download/v4.1.1/podman-remote-static.tar.gz -o "podman-remote4.tar.gz"
  tar -zxvf podman-remote4.tar.gz
  mv podman-remote-static /usr/local/bin/podman-remote4
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

  OPERATOR_SDK_VERSION=v1.10.1
  curl --retry 5 --connect-timeout 30 -sL "https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/operator-sdk_$(uname)_${ARCH}" --output /usr/local/bin/operator-sdk
  curl --retry 5 --connect-timeout 30 -sL "https://mirror.openshift.com/pub/openshift-v4/multi/clients/ocp/latest/${ARCH}/openshift-client-linux.tar.gz" | tar -C /usr/local/bin -xz

  chmod +x /usr/local/bin/kubectl /usr/local/bin/oc  /usr/local/bin/operator-sdk

  dnf install -y unzip diffutils python3-pip genisoimage skopeo
  dnf clean all && rm -rf /var/cache/yum

  jq

  awscli

  spectral

  test_tools


  go install golang.org/x/tools/cmd/goimports@v0.1.5
  go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.2

  python3 -m venv ${VIRTUAL_ENV:-/opt/venv}
  python3 -m pip install --upgrade pip
  python3 -m pip install --no-cache-dir -r ./dev-requirements.txt
}


declare -F $@ || (print_help && exit 1)

"$@"
