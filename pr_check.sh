#!/bin/bash

IMAGE_TEST=bm-inventory-test

docker build -t ${IMAGE_TEST} -f Dockerfile.test .
docker run --rm ${IMAGE_TEST}
