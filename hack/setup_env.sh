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

  # We tried using "official" install_kustomize.sh script, but it used too much rate-limited APIs of GitHub
  curl -L --retry 5 "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.3.0/kustomize_v4.3.0_linux_amd64.tar.gz" | \
    tar -zx -C /usr/bin/
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
  curl --retry 5 -L "https://dl.k8s.io/release/${latest_kubectl_version}/bin/linux/amd64/kubectl" -o /tmp/kubectl && \
    install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
    rm -f /tmp/kubectl

  yum install -y --setopt=skip_missing_names_on_install=False \
    docker podman python3-pip genisoimage skopeo

  jq

  kustomize

  awscli

  spectral

  test_tools

  OS=$(uname | awk '{print tolower($0)}')
  OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.10.1
  curl --retry 5 -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
  chmod +x operator-sdk_${OS}_${ARCH}
  install operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk

  go get golang.org/x/tools/cmd/goimports@v0.1.5 \
        sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.2

  python3 -m pip install --upgrade pip
  python3 -m pip install -r ./dev-requirements.txt
}

function hive_from_upstream() {
  kustomize
  golang
}

declare -F $@ || (print_help && exit 1)

"$@"
