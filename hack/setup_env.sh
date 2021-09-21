#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace
set -o verbose

if [[ $(id -u) -eq 0 ]]; then
        BINDIR="/usr/local/bin"
elif command -v systemd-detect-virt; then
    if systemd-detect-virt -cq; then
        if [ -n "${TOOLBOX_PATH+x}" ]; then
            BINDIR="$(systemd-path user-binaries)"
        fi
    else
        BINDIR="$(systemd-path user-binaries)"
    fi
else
    BINDIR="/usr/local/bin"
fi

function print_help() {
  ALL_FUNCS="kustomize|golang|assisted_service|hive_from_upstream|print_help"
  echo "Usage: bash ${0} (${ALL_FUNCS})"
}

function kustomize() {
  if ! type -f kustomize; then
    # We tried using "official" install_kustomize.sh script, but it used too much rate-limited APIs of GitHub
    curl -L --retry 5 "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.3.0/kustomize_v4.3.0_linux_amd64.tar.gz" | \
    tar -zx -C "$BINDIR"
  fi
}

function golang() {
  echo "Installing golang..."
  local tmpdir="$(mktemp -d)"
  curl -L https://storage.googleapis.com/golang/getgo/installer_linux -o "${tmpdir}/golang_installer"
  chmod u+x ${tmpdir}/golang_installer
  "${tmpdir}/golang_installer"
  rm /tmp/golang_installer
  rmdir "$tmpdir"

  echo "Activating go command on current shell..."
  set +u
  source "${HOME}/.bash_profile"
  set -u
}

function spectral() {
  if ! type -f spectral; then
    echo "Installing spectral..."
    curl -L https://github.com/stoplightio/spectral/releases/download/v5.9.1/spectral-linux -o "${BINDIR}/spectral"
    chmod +x "${BINDIR}/spectral"
  fi
}

function assisted_service() {
  latest_kubectl_version=$(curl --retry 5 -L -s https://dl.k8s.io/release/stable.txt)
  curl --retry 5 -L "https://dl.k8s.io/release/${latest_kubectl_version}/bin/linux/amd64/kubectl" -o /tmp/kubectl && \
    install -o root -g root -m 0755 /tmp/kubectl "${BINDIR}/kubectl" && \
    rm -f /tmp/kubectl
  yum install -y docker podman libvirt-clients awscli python3-pip postgresql genisoimage skopeo p7zip && \
    yum clean all

  kustomize

  spectral

  curl --retry 5 -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b $(go env GOPATH)/bin v1.36.0

  ARCH=$(case $(arch) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(arch) ;; esac)
  OS=$(uname | awk '{print tolower($0)}')
  OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/v1.10.1
  curl --retry 5 -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}
  chmod +x operator-sdk_${OS}_${ARCH}
  install operator-sdk_${OS}_${ARCH} "${BINDIR}/operator-sdk"

  go get github.com/onsi/ginkgo/ginkgo@v1.16.4 \
    golang.org/x/tools/cmd/goimports@v0.1.5 \
    github.com/golang/mock/mockgen@v1.5.0 \
    github.com/vektra/mockery/.../@v1.1.2 \
    gotest.tools/gotestsum@v1.6.3 \
    github.com/axw/gocov/gocov \
    sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.2 \
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
