#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

function print_help() {
  ALL_FUNCS="kustomize|golang|assisted_service|hive_from_upstream|print_help"
  echo "Usage: bash ${0} (${ALL_FUNCS})"
}

function kustomize() {
  if which kustomize; then
    return
  fi

  (cd /usr/bin &&
    curl --retry 5 -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" \
      | bash -s -- 3.8.8)
}

function golang() {
  echo "Installing golang..."
  curl -L https://storage.googleapis.com/golang/getgo/installer_linux -o /tmp/golang_installer
  chmod u+x /tmp/golang_installer
  /tmp/golang_installer
  rm /tmp/golang_installer

  echo "Activating go command on current shell..."
  set +u
  source /root/.bash_profile
  set -u
}

function assisted_service() {
  latest_kubectl_version=$(curl --retry 5 -L -s https://dl.k8s.io/release/stable.txt)
  curl --retry 5 -L "https://dl.k8s.io/release/${latest_kubectl_version}/bin/linux/amd64/kubectl" -o /tmp/kubectl && \
    install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
    rm -f /tmp/kubectl
  yum install -y docker podman libvirt-clients awscli python3-pip postgresql genisoimage skopeo && \
    yum clean all

  kustomize

  curl --retry 5 -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b $(go env GOPATH)/bin v1.36.0

  ARCH=$(case $(arch) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(arch) ;; esac)
  OS=$(uname | awk '{print tolower($0)}')
  OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.7.2
  curl --retry 5 -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
  chmod +x operator-sdk_${OS}_${ARCH}
  install operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk

  go get github.com/onsi/ginkgo/ginkgo@v1.16.1 \
    golang.org/x/tools/cmd/goimports@v0.1.0 \
    github.com/golang/mock/mockgen@v1.4.3 \
    github.com/vektra/mockery/.../@v1.1.2 \
    gotest.tools/gotestsum@v1.6.3 \
    github.com/axw/gocov/gocov \
    sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.0 \
    github.com/AlekSi/gocov-xml@v0.0.0-20190121064608-3a14fb1c4737

  python3 -m pip install --upgrade pip
  python3 -m pip install -r ./dev-requirements.txt
}

function hive_from_upstream() {
  kustomize
  golang
}

declare -F $@ || (print_help && exit 1)

"$@"
