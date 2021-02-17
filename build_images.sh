#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

# required for `skipper` according to
# https://github.com/Stratoscale/skipper/blob/upstream/README.md#python3-environment
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

TAG=$(git rev-parse --short=7 HEAD)
ASSISTED_SERVICE_IMAGE=quay.io/app-sre/assisted-service

GOLANG_ORIGINAL_IMAGE=registry.svc.ci.openshift.org/openshift/release:golang-1.15
GOLANG_APPSRE_IMAGE=quay.io/app-sre/golang

docker login quay.io/app-sre
sed -i "s|FROM ${GOLANG_ORIGINAL_IMAGE}|FROM ${GOLANG_APPSRE_IMAGE}|" Dockerfile.assisted-service-build
sed -i "s|FROM ${GOLANG_ORIGINAL_IMAGE}|FROM ${GOLANG_APPSRE_IMAGE}|" Dockerfile.assisted-service

SERVICE="${ASSISTED_SERVICE_IMAGE}:${TAG}" skipper make update-minimal
