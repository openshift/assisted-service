#!/bin/bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

TAG=$(git rev-parse HEAD)
SHORT_TAG=$(git rev-parse --short=7 HEAD)
ASSISTED_SERVICE_IMAGE=quay.io/app-sre/assisted-service
export SERVICE="${ASSISTED_SERVICE_IMAGE}:${TAG}"
export CONTAINER_BUILD_EXTRA_PARAMS="--no-cache"

make update-minimal
