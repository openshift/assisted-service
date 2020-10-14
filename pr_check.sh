#!/bin/bash

set -e

IMAGE_TEST=assisted-service-test

# run some 'assisted-service' checks inside a golang container
podman build -t ${IMAGE_TEST} -f Dockerfile.test .
podman run --rm ${IMAGE_TEST}

# build app-sre image to make sure they are build without errors before merging to master
source build_images.sh

