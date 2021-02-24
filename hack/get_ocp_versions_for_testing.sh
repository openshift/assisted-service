#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
PROJECT_ROOT_DIR=${SCRIPT_DIR}/..

OPENSHIFT_SNO_IMAGE="registry.svc.ci.openshift.org/sno-dev/openshift-bip:0.6.0"

# Append single node image as 4.8 for local deployments
"${PROJECT_ROOT_DIR}"/tools/handle_ocp_versions.py --src ./default_ocp_versions.json --ocp-override ${OPENSHIFT_SNO_IMAGE} --name-override single-node-4.8-alpha --version-override 4.8 --skip-verify | tr -d "\n\t "
