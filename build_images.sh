#!/bin/bash

# required for `skipper` according to
# https://github.com/Stratoscale/skipper/blob/upstream/README.md#python3-environment
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

TAG=$(git rev-parse --short=7 HEAD)
ASSISTED_SERVICE_IMAGE=quay.io/app-sre/assisted-service
ASSISTED_ISO_CREATE_IMAGE=quay.io/app-sre/assisted-iso-create

SERVICE="${ASSISTED_SERVICE_IMAGE}:latest" skipper make update-minimal
docker tag "${ASSISTED_SERVICE_IMAGE}:latest" "${ASSISTED_SERVICE_IMAGE}:${TAG}"

ISO_CREATION="${ASSISTED_ISO_CREATE_IMAGE}:latest" skipper make build-minimal-assisted-iso-generator-image
docker tag "${ASSISTED_ISO_CREATE_IMAGE}:latest" "${ASSISTED_ISO_CREATE_IMAGE}:${TAG}"
