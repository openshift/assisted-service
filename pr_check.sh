#!/bin/bash

IMAGE_TEST=assisted-service-test

docker build -t ${IMAGE_TEST} -f Dockerfile.test .
docker run --rm ${IMAGE_TEST}
