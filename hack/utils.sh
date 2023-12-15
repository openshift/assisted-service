#!/usr/bin/env bash

function running_from_skipper() {
   # The SKIPPER_UID environment variable is an indication that we are running on a skipper container.
   [ -n "${SKIPPER_UID+x}" ]
}

function get_container_runtime_command() {
  if [[ "${CONTAINER_TOOL}" == "docker" ]]; then
      >&2 echo "WARNING: docker is not supported. For seamless experience use podman or podman-remote"
  fi
  # if CONTAINER_TOOL is defined skipping
  if [ -z "${CONTAINER_TOOL+x}" ]; then
      CONTAINER_TOOL=$( command -v podman &> /dev/null && echo "podman" || echo "podman-remote")
  fi
  echo $CONTAINER_TOOL
}

# podman-remote4 cannot run against podman server 3 so the skipper image contains them both
# here we select the right podman-remote version
function select_podman_client() {
  # already linked
  if command -v podman-remote &> /dev/null; then
    exit
  fi

  if [ "$(get_container_runtime_command)" = "podman-remote" ]; then
    if podman-remote4 info 2>&1 | grep "server API version is too old" &> /dev/null; then
      echo "using podman-remote version 3"
      ln $(which podman-remote3) /tools/podman-remote
    else
      echo "using podman-remote version 4"
      ln $(which podman-remote4) /tools/podman-remote
    fi
  fi
}

"$@"
