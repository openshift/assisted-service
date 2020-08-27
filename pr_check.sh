#!/bin/bash

IMAGE_TEST=assisted-service-test

# run some 'assisted-service' checks inside a golang container
docker build -t ${IMAGE_TEST} -f Dockerfile.test .
docker run --rm ${IMAGE_TEST}

# build app-sre image to make sure they are build without errors before merging to master
./build_images.sh

