#!/bin/bash

set -euo pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT_DIR="$(cd "$(dirname "${__dir}")" && pwd)"

# This is currently disabled because the 4.8-fc image has been released and we already
# include it in `${PROJECT_ROOT_DIR}/data/default_ocp_versions.json`.
# The code is still kept around behind this gate to provide an example of how we can do
# a similar override in the future.
OVERRIDE_4_8_WITH_SINGLE_NODE=0
SINGLE_VERSION_ONLY=${SINGLE_VERSION_ONLY:-false}

if [[ ${OVERRIDE_4_8_WITH_SINGLE_NODE} == 1 ]]; then
    OPENSHIFT_SNO_IMAGE="quay.io/openshift-release-dev/ocp-release-nightly:4.8.0-0.nightly-2021-03-16-221720"

    # Append single node image as 4.8 for local deployments
    "${PROJECT_ROOT_DIR}"/tools/handle_ocp_versions.py --src ${PROJECT_ROOT_DIR}/data/default_ocp_versions.json --ocp-override ${OPENSHIFT_SNO_IMAGE} --name-override single-node-4.8-alpha --version-override 4.8 --skip-verify --single-version-only=${SINGLE_VERSION_ONLY} | tr -d "\n\t "
else
    tr -d "\n\t " < ${PROJECT_ROOT_DIR}/data/default_ocp_versions.json
fi
