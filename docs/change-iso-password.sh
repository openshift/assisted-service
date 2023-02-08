#!/bin/bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "Usage: $0 <path to discovery .iso>"
	exit 1
fi

if [[ ! -f $1 ]]; then
	echo "ERROR: Discovery ISO not found at $1"
	exit 1
fi

DISCOVERY_ISO_HOST_PATH="$1"
DISCOVERY_ISO_HOST_DIR=$(dirname "$DISCOVERY_ISO_HOST_PATH")
function COREOS_INSTALLER() {
	podman run -v "$DISCOVERY_ISO_HOST_DIR":/data --rm quay.io/coreos/coreos-installer:release "$@"
}

ISO_NAME=$(basename "$DISCOVERY_ISO_HOST_PATH" .iso)

# Container paths
DISCOVERY_ISO_PATH=/data/${ISO_NAME}.iso
DISCOVERY_ISO_WITH_PASSWORD=/data/${ISO_NAME}_with_password.iso

# Host output path
DISCOVERY_ISO_WITH_PASSWORD_HOST=$(dirname "$DISCOVERY_ISO_HOST_PATH")/$(basename "$DISCOVERY_ISO_WITH_PASSWORD")

# Prompt
read -rsp 'Please enter the password to be used by the "core" user: ' pw
echo ''
USER_PASSWORD=$(openssl passwd -6 --stdin <<<"$pw")
unset pw

# Transform original ignition
TRANSFORMED_IGNITION_PATH=$(mktemp --tmpdir="$DISCOVERY_ISO_HOST_DIR")
TRANSFORMED_IGNITION_NAME=$(basename "$TRANSFORMED_IGNITION_PATH")
COREOS_INSTALLER iso ignition show "$DISCOVERY_ISO_PATH" | jq --arg pass "$USER_PASSWORD" '.passwd.users[0].passwordHash = $pass' >"$TRANSFORMED_IGNITION_PATH"

if [[ -f "$DISCOVERY_ISO_WITH_PASSWORD_HOST" ]]; then
	echo "ERROR: $DISCOVERY_ISO_WITH_PASSWORD_HOST already exists"
	echo "Would you like to overwrite it? [y/N]"
	read -r SHOULD_OVERWRITE
	if [[ "$SHOULD_OVERWRITE" != "y" ]]; then
		echo "Exiting"
		exit 1
	fi
fi

# Generate new ISO
rm -f "$DISCOVERY_ISO_WITH_PASSWORD_HOST"
COREOS_INSTALLER iso customize --output "$DISCOVERY_ISO_WITH_PASSWORD" --force "$DISCOVERY_ISO_PATH" --live-ignition /data/"$TRANSFORMED_IGNITION_NAME"
echo 'Created ISO with your password in "'"$DISCOVERY_ISO_WITH_PASSWORD_HOST"'", the login username is "core"'
