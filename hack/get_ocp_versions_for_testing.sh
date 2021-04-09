#!/bin/bash

set -euo pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT_DIR="$(cd "$(dirname "${__dir}")" && pwd)"
OPENSHIFT_SNO_IMAGE="quay.io/openshift-release-dev/ocp-release-nightly:4.8.0-0.nightly-2021-03-16-221720"

# Append single node image as 4.8 for local deployments
"${PROJECT_ROOT_DIR}"/tools/handle_ocp_versions.py --src ./default_ocp_versions.json --ocp-override ${OPENSHIFT_SNO_IMAGE} --name-override single-node-4.8-alpha --version-override 4.8 --skip-verify | tr -d "\n\t "
