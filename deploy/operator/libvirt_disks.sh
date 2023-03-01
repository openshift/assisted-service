#!/usr/bin/env bash

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

set -x

function print_help() {
    echo "Usage: DISKS=\$(echo sd{b..f}) bash ${0} (create|destroy|print_help)"
}

if [ -z "${NODES:-}" ]; then
    export NODES=$(virsh list --name | grep worker || virsh list --name | grep master)
fi

function create() {
    export SIZE=${SIZE:-50G}
    export DISKS_IMAGE_DIR=${DISKS_IMAGE_DIR:-/tmp}

    echo "Creating libvirt disks and attaching them..."
    for node in ${NODES}; do
        possible_targets=(sd{b..z})
        for disk in ${DISKS}; do
            img_path="${DISKS_IMAGE_DIR}/${node}-${disk}.img"
            if [ ! -f "${img_path}" ]; then
                qemu-img create -f raw "${img_path}" "${SIZE}"
            else
                echo "Image ${img_path} already existing. Skipping creation"
            fi

            if virsh domblklist "${node}" | grep -q "${img_path}"; then
                echo "Image ${img_path} is already attached to ${node}. Skipping attachment"
                continue
            fi

            failed=true
            while [ "${#possible_targets[@]}" -gt 0 ]; do
                target="${possible_targets[0]}" # get the first element
                possible_targets=("${possible_targets[@]:1}") # remove the first element

                # Libvirt cannot guarantee the device name on the guest OS (It's controlled by udev)
                # In order to make an absolute expected destination, we provide a WWN
                # that could be derived from the disk name.
                # The disk would be available as a link on /dev/disk/by-id/wwn-.}}
                # https://bugzilla.redhat.com/show_bug.cgi?id=693372
                if virsh attach-disk "${node}" "${img_path}" "${target}" --wwn "$(disk_to_wwn ${disk})"; then
                    failed=false
                    break
                fi
            done

            if ${failed}; then
                echo "Failed to attach image ${img_path} to node ${node}."
                exit 1
            fi
        done
    done

    echo "Done creating libvirt disks!"
}

function destroy() {
    echo "Unattaching libvirt disks and destroying them..."

    for node in ${NODES}; do
        for disk in ${DISKS}; do
            img_path="/tmp/${node}-${disk}.img"

            if virsh domblklist "${node}" | grep -q "${img_path}"; then
                virsh detach-disk "${node}" "${disk}" || true
            fi

            if [ -f "${img_path}" ]; then
                rm -rf "${img_path}"
            fi
        done
    done

    echo "Done destroying libvirt disks!"
}

if [ -z "$@" ] || ! declare -F "$@"; then
  print_help
  exit 1
fi

"$@"
