#!/bin/bash

set -e

# required for `skipper` according to
# https://github.com/Stratoscale/skipper/blob/upstream/README.md#python3-environment
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"

TAG=$(git rev-parse --short=7 HEAD)
ASSISTED_SERVICE_IMAGE=quay.io/app-sre/assisted-service

SERVICE="${ASSISTED_SERVICE_IMAGE}:${TAG}" skipper make update-minimal
