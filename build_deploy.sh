#!/bin/bash

TAG=$(git rev-parse --short=7 HEAD)

BM_INVENTORY_IMAGE="quay.io/app-sre/bm-inventory"

docker build -t "${BM_INVENTORY_IMAGE}:latest" -f Dockerfile.demo .
docker tag "${BM_INVENTORY_IMAGE}:latest" "${BM_INVENTORY_IMAGE}:${TAG}"
#docker build -t "${BM_INVENTORY_IMAGE}:latest" -f Dockerfile.bm-inventory .
#docker tag "${BM_INVENTORY_IMAGE}:latest" "${BM_INVENTORY_IMAGE}:${TAG}"

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
