#!/bin/bash

set -e

source build_images.sh

# This folder/authfile is not necessary to be patched, podman will use $HOME/.docker/config.json.
DOCKER_CONF="${PWD}/.docker"
mkdir -p "${DOCKER_CONF}"
podman --config="${DOCKER_CONF}" login -u="${QUAY_USER}" -p="${QUAY_TOKEN}" quay.io

podman --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_IMAGE}:latest"
podman --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_IMAGE}:${TAG}"

podman --config="${DOCKER_CONF}" push "${ASSISTED_ISO_CREATE_IMAGE}:latest"
podman --config="${DOCKER_CONF}" push "${ASSISTED_ISO_CREATE_IMAGE}:${TAG}"

