#!/bin/bash

set -euxo pipefail

SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
SHELL_DIR="${SCRIPT_DIR}"/shell
RAW_MACHINE_JSON_DIR="${SCRIPT_DIR}"/raw

HOST_JSON="${RAW_MACHINE_JSON_DIR}"/host_state_machine.json
CLUSTER_JSON="${RAW_MACHINE_JSON_DIR}"/cluster_state_machine.json
UNBOUND_HOST_JSON="${RAW_MACHINE_JSON_DIR}"/unbound_host_state_machine.json

# Import helper functions
. "${SHELL_DIR}"/helper_functions.sh

# Generate the state machine JSON by building and running the Go program
mkdir --parents "${RAW_MACHINE_JSON_DIR}"
. "${SHELL_DIR}"/generate_json.sh

# Generate readme for all machines
machines=($HOST_JSON $CLUSTER_JSON $UNBOUND_HOST_JSON)
. "${SHELL_DIR}"/generate_machine_readmes.sh

# Generate the main README
. "${SHELL_DIR}"/generate_main_readme.sh
