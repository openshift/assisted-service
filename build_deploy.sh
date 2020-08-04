#!/bin/bash

TAG=$(git rev-parse --short=7 HEAD)

ASSISTED_SERVICE_IMAGE="quay.io/app-sre/assisted-service"

docker build -t "${ASSISTED_SERVICE_IMAGE}:latest" -f Dockerfile.demo .
docker tag "${ASSISTED_SERVICE_IMAGE}:latest" "${ASSISTED_SERVICE_IMAGE}:${TAG}"
#docker build -t "${ASSISTED_SERVICE_IMAGE}:latest" -f Dockerfile.assisted-service .
#docker tag "${ASSISTED_SERVICE_IMAGE}:latest" "${ASSISTED_SERVICE_IMAGE}:${TAG}"

#ASSISTED_SERVICE_BUILD_IMAGE="quay.io/app-sre/assisted-service-build"
#
#docker build -t "${ASSISTED_SERVICE_BUILD_IMAGE}:latest" -f Dockerfile.assisted-service-build .
#docker tag "${ASSISTED_SERVICE_BUILD_IMAGE}:latest" "${ASSISTED_SERVICE_BUILD_IMAGE}:${TAG}"

#OBJ_EXPIRER_IMAGE="quay.io/app-sre/s3-object-expirer"
#
#docker build -t "${OBJ_EXPIRER_IMAGE}:latest" -f Dockerfile.s3-object-expirer .
#docker tag "${OBJ_EXPIRER_IMAGE}:latest" "${OBJ_EXPIRER_IMAGE}:${TAG}"

DOCKER_CONF="${PWD}/.docker"
mkdir -p "${DOCKER_CONF}"
docker --config="${DOCKER_CONF}" login -u="${QUAY_USER}" -p="${QUAY_TOKEN}" quay.io

docker --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_IMAGE}:latest"
docker --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_IMAGE}:${TAG}"

#docker --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_BUILD_IMAGE}:latest"
#docker --config="${DOCKER_CONF}" push "${ASSISTED_SERVICE_BUILD_IMAGE}:${TAG}"
#
#docker --config="${DOCKER_CONF}" push "${OBJ_EXPIRER_IMAGE}:latest"
#docker --config="${DOCKER_CONF}" push "${OBJ_EXPIRER_IMAGE}:${TAG}"
