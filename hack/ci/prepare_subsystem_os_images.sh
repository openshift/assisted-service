#!/usr/bin/env bash
# Prepare OS_IMAGES for subsystem test deploy.
#
# Subsystem tests register disconnected-iso InfraEnvs against the default OCP
# version. That requires a catalog entry with type "disconnected-iso". Production
# OVE images are not mirrored in CI, so when no disconnected entry exists for the
# default version we add a stub that reuses the regular public RHCOS mirror URL
# (same bytes image-service already downloads for online ISOs).
#
# Prints compact JSON to stdout for use as OS_IMAGES.

set -euo pipefail

ROOT_DIR="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OS_IMAGES_FILE="${ROOT_DIR}/data/default_os_images.json"
RELEASE_IMAGES_FILE="${ROOT_DIR}/data/default_release_images.json"

if ! command -v jq >/dev/null 2>&1; then
	echo "error: jq is required" >&2
	exit 1
fi

DEFAULT_OCP="$(jq -r '[.[] | select(.default == true)][0].openshift_version // empty' "${RELEASE_IMAGES_FILE}")"
if [[ -z "${DEFAULT_OCP}" ]]; then
	echo "error: no default openshift version in ${RELEASE_IMAGES_FILE}" >&2
	exit 1
fi

jq -c --arg ver "${DEFAULT_OCP}" '
  def is_regular:
    (.type // "") == "";
  def is_disconnected:
    .type == "disconnected-iso";

  if any(.[]; is_disconnected and .openshift_version == $ver and .cpu_architecture == "x86_64") then
    .
  else
    ([.[] | select(.openshift_version == $ver and .cpu_architecture == "x86_64" and is_regular)] | first) as $regular |
    if $regular == null then
      error("no regular x86_64 OS image for openshift version " + $ver)
    else
      . + [$regular + {type: "disconnected-iso"}]
    end
  end
' "${OS_IMAGES_FILE}"
