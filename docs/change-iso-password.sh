#!/bin/bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <path to discovery .iso>"
    exit 1
fi

# Check if podman is available
if ! command -v podman &>/dev/null; then
    echo "ERROR: podman is required but not installed or not in PATH"
    echo "Please install podman to use this script"
    echo "On RHEL/CentOS/Fedora: sudo dnf install podman"
    echo "On Ubuntu/Debian: sudo apt install podman"
    echo "On macOS: brew install podman"
    exit 1
fi

# Check if podman can connect (especially important on macOS)
if ! podman version &>/dev/null; then
    echo "ERROR: podman is installed but doesn't seem to be running or accessible."
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "On macOS, podman requires a virtual machine. You might need to run:"
        echo "  podman machine init"
        echo "  podman machine start"
        echo "Then try this script again."
    fi
    exit 1
fi

if [[ ! -f $1 ]]; then
    echo "ERROR: Discovery ISO not found at $1"
    exit 1
fi

DISCOVERY_ISO_HOST_PATH="$1"
DISCOVERY_ISO_HOST_DIR=$(dirname "$(realpath "$DISCOVERY_ISO_HOST_PATH")")
STDERR_FILE=$(mktemp --tmpdir="$DISCOVERY_ISO_HOST_DIR")

TRANSFORMED_IGNITION_PATH=$(mktemp --tmpdir="$DISCOVERY_ISO_HOST_DIR")
TRANSFORMED_IGNITION_NAME=$(basename "$TRANSFORMED_IGNITION_PATH")

trap 'rm -f "$STDERR_FILE $TRANSFORMED_IGNITION_PATH"' EXIT

function COREOS_INSTALLER() {
    echo "Running command in coreos-installer container, this might take a while..." >&2

    # Create temporary directory to avoid macOS permission prompt
    local temp_dir
    temp_dir=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf '$temp_dir'" RETURN

    # Copy files to temp directory that the container will need
    cp "$DISCOVERY_ISO_HOST_PATH" "$temp_dir/"
    if [[ -f "$TRANSFORMED_IGNITION_PATH" ]]; then
        cp "$TRANSFORMED_IGNITION_PATH" "$temp_dir/"
    fi

    if [[ "$OSTYPE" != "darwin"* ]] && [ -f /sys/fs/selinux/enforce ] && [ "$(</sys/fs/selinux/enforce)" = "1" ]; then
        iso_volume="$temp_dir":/data:Z
    else
        iso_volume="$temp_dir":/data
    fi

    if ! podman run -v "$iso_volume" --rm quay.io/coreos/coreos-installer:release "$@" 2>"$STDERR_FILE"; then
        echo "Failed to run podman. stderr:" >&2
        cat "$STDERR_FILE" >&2
        exit 1
    fi

    # Copy any output files back to original location
    if [[ -f "$temp_dir/$(basename "$DISCOVERY_ISO_WITH_PASSWORD")" ]]; then
        cp "$temp_dir/$(basename "$DISCOVERY_ISO_WITH_PASSWORD")" "$DISCOVERY_ISO_WITH_PASSWORD_HOST"
    fi
}

function OPENSSL_PASSWORD() {
    echo "Running command in fedora-minimal container, this might take a while..." >&2
    pw="$1"
    if ! podman run --rm -i quay.io/fedora/fedora-minimal bash -c 'microdnf install -y openssl >/dev/null 2>&1 && openssl passwd -6 --stdin' <<<"$pw" 2>"$STDERR_FILE"; then
        echo "Failed to run podman. stderr:" >&2
        cat "$STDERR_FILE" >&2
        exit 1
    fi
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
USER_PASSWORD=$(OPENSSL_PASSWORD "$pw")
unset pw

# Transform original ignition
COREOS_INSTALLER iso ignition show "$DISCOVERY_ISO_PATH" | jq --arg pass "$USER_PASSWORD" '
    . as $root |
    [($root.passwd.users[] | select(.name != "core"))] as $non_core_users |
    [($root.passwd.users[] | select(.name == "core"))] as $core_users |
    $root.passwd.users |= $non_core_users + [
        if ($core_users | length != 0) then
            $core_users[] | .passwordHash = $pass
        else
            { name: "core", passwordHash: $pass, groups: ["sudo"] }
        end
    ]
' >"$TRANSFORMED_IGNITION_PATH"

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
