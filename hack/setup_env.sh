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

function envtest() {
  # Branch 'release-0.17' is the newest version that can be installed with Go 1.21. This should be updated when we
  # update the version of Go.
  go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.17

  # The unit tests will try to use the 'setup-envtest' tool to download and locate the required assets. But that doesn't
  # work in the CI environment because that tool saves the assets to a directory in the home of the user, which may not
  # be writeable. To avoid that we download them here, and we move them to the default directory where the unit tests
  # expect them.
  src=$(setup-envtest use --print path 1.30.0)
  dst="/usr/local/kubebuilder/bin"
  mkdir -p "${dst}"
  mv "${src}"/* "${dst}"/.
  setup-envtest cleanup
}

function test_tools() {
  go install github.com/onsi/ginkgo/ginkgo@v1.16.4
  go install github.com/golang/mock/mockgen@v1.6.0
  go install github.com/vektra/mockery/v2@v2.12.3
  go install gotest.tools/gotestsum@v1.6.3
  go install github.com/axw/gocov/gocov@v1.1.0
  go install github.com/AlekSi/gocov-xml@v1.1.0
  envtest
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
  go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0

  python3 -m venv ${VIRTUAL_ENV:-/opt/venv}
  source ${VIRTUAL_ENV:-/opt/venv}/bin/activate
  python3 -m pip install --upgrade pip
  python3 -m pip install --no-cache-dir -r ./dev-requirements.txt
}


declare -F $@ || (print_help && exit 1)

"$@"
