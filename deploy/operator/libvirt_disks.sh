__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/utils.sh

if [ -z "${NODES:-}" ]; then
    export NODES=$(virsh list --name | grep worker || virsh list --name | grep master)
fi

if [ -z "${DISKS:-}" ]; then
    echo "You must provide DISKS env-var."
    print_help
    exit 1
fi

function create() {
    export SIZE=${SIZE:-50G}

    echo "Creating libvirt disks and attaching them..."
    for node in ${NODES}; do
        for disk in ${DISKS}; do
            img_path="/tmp/${node}-${disk}.img"
            if [ -f ${img_path} ]; then
                echo "Image ${img_path} already existing. Skipping creation"
                continue
            fi

            qemu-img create -f raw "${img_path}" "${SIZE}"
            virsh attach-disk "${node}" "${img_path}" "${disk}"
        done
    done

    echo "Done creating libvirt disks!"
}

function destroy() {
    echo "Unattaching libvirt disks and destroying them..."

    for node in ${NODES}; do
        for disk in ${DISKS}; do
            img_path="/tmp/${node}-${disk}.img"
            if [ -f ${img_path} ]; then
                virsh detach-disk "${node}" "${disk}"
                rm -rf "${img_path}"
            fi
        done
    done

    echo "Done destroying libvirt disks!"
}

declare -F $@ || (print_help && exit 1)

"$@"
