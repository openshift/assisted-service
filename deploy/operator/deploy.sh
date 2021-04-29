if [ -z "${DISKS}" ]; then
    export DISKS=$(echo sd{b..f})
fi

source ./libvirt_disks.sh create
source ./setup_lso.sh
source ./setup_hive.sh
source ./setup_assisted_operator.sh
