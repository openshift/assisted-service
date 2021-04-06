#!/bin/bash

set -o nounset
set -o pipefail
set -o errexit
set -o xtrace

TAG=$(git rev-parse --short=7 HEAD)
ASSISTED_SERVICE_IMAGE=quay.io/app-sre/assisted-service
export SERVICE="${ASSISTED_SERVICE_IMAGE}:${TAG}"

make update-minimal
