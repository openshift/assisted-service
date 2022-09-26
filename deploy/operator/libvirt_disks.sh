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

    echo "Creating libvirt disks and attaching them..."
    for node in ${NODES}; do
        for disk in ${DISKS}; do
            img_path="/tmp/${node}-${disk}.img"
            if [ ! -f "${img_path}" ]; then
                qemu-img create -f raw "${img_path}" "${SIZE}"
            else
                echo "Image ${img_path} already existing. Skipping creation"
            fi

            node_disks=$(virsh domblklist "${node}" | awk '{print $1}')
            if [[ ! "${node_disks}" =~ "${disk}" ]]; then
                # Libvirt cannot guarantee the device name on the guest OS (It's controlled by udev)
                # In order to make an absolute expected destination, we provide a WWN
                # that could be derived from the disk name.
                # The disk would be available as a link on /dev/disk/by-id/wwn-.
                virsh attach-disk "${node}" "${img_path}" "${disk}" --wwn "$(disk_to_wwn ${disk})"
            else
                echo "Disk ${disk} is already attached to ${node}. Skipping attachment"
            fi
        done
    done

    echo "Done creating libvirt disks!"
}

function destroy() {
    echo "Unattaching libvirt disks and destroying them..."

    for node in ${NODES}; do
        for disk in ${DISKS}; do
            node_disks=$(virsh domblklist "${node}" | awk '{print $1}')
            if [[ "${node_disks}" =~ "${disk}" ]]; then
                virsh detach-disk "${node}" "${disk}" || true
            fi

            img_path="/tmp/${node}-${disk}.img"
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
