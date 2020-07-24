#!/bin/bash

# required for `skipper` according to
# https://github.com/Stratoscale/skipper/blob/upstream/README.md#python3-environment
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

TAG=$(git rev-parse --short=7 HEAD)
BM_INVENTORY_IMAGE="quay.io/app-sre/bm-inventory"

SERVICE="${BM_INVENTORY_IMAGE}:latest" skipper make update-minimal
docker tag "${BM_INVENTORY_IMAGE}:latest" "${BM_INVENTORY_IMAGE}:${TAG}"

#BM_INVENTORY_BUILD_IMAGE="quay.io/app-sre/bm-inventory-build"
#
#docker build -t "${BM_INVENTORY_BUILD_IMAGE}:latest" -f Dockerfile.bm-inventory-build .
#docker tag "${BM_INVENTORY_BUILD_IMAGE}:latest" "${BM_INVENTORY_BUILD_IMAGE}:${TAG}"

#OBJ_EXPIRER_IMAGE="quay.io/app-sre/s3-object-expirer"
#
#docker build -t "${OBJ_EXPIRER_IMAGE}:latest" -f Dockerfile.s3-object-expirer .
#docker tag "${OBJ_EXPIRER_IMAGE}:latest" "${OBJ_EXPIRER_IMAGE}:${TAG}"

DOCKER_CONF="${PWD}/.docker"
mkdir -p "${DOCKER_CONF}"
docker --config="${DOCKER_CONF}" login -u="${QUAY_USER}" -p="${QUAY_TOKEN}" quay.io

docker --config="${DOCKER_CONF}" push "${BM_INVENTORY_IMAGE}:latest"
docker --config="${DOCKER_CONF}" push "${BM_INVENTORY_IMAGE}:${TAG}"

#docker --config="${DOCKER_CONF}" push "${BM_INVENTORY_BUILD_IMAGE}:latest"
#docker --config="${DOCKER_CONF}" push "${BM_INVENTORY_BUILD_IMAGE}:${TAG}"
#
#docker --config="${DOCKER_CONF}" push "${OBJ_EXPIRER_IMAGE}:latest"
#docker --config="${DOCKER_CONF}" push "${OBJ_EXPIRER_IMAGE}:${TAG}"
