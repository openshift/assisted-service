#!/usr/bin/env bash

function running_from_skipper() {
   # The SKIPPER_UID environment variable is an indication that we are running on a skipper container.
   [ -n "${SKIPPER_UID+x}" ]
}

function get_container_runtime_command() {
    if running_from_skipper; then
        CONTAINER_TOOL=$( [[ "${CONTAINER_RUNTIME_COMMAND}" == ""docker"" ]] && echo "docker" || echo "podman-remote")    
    elif [ ! -n "${CONTAINER_TOOL+x}" ]; then
        CONTAINER_TOOL=$( command -v podman &> /dev/null && echo "podman" || echo "docker")
    fi 

    echo $CONTAINER_TOOL
}

"$@"