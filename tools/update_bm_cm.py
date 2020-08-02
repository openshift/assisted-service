#!/usr/bin/python3
# -*- coding: utf-8 -*-

import os
import yaml

CM_PATH = "deploy/bm-inventory-configmap.yaml"
ENVS = [
    ("HW_VALIDATOR_MIN_CPU_CORES", "2"),
    ("HW_VALIDATOR_MIN_CPU_CORES_WORKER", "2"),
    ("HW_VALIDATOR_MIN_CPU_CORES_MASTER", "4"),
    ("HW_VALIDATOR_MIN_RAM_GIB", "3"),
    ("HW_VALIDATOR_MIN_RAM_GIB_WORKER", "3"),
    ("HW_VALIDATOR_MIN_RAM_GIB_MASTER", "8"),
    ("HW_VALIDATOR_MIN_DISK_SIZE_GIB", "10"),
    ("INSTALLER_IMAGE", ""),
    ("CONTROLLER_IMAGE", ""),
    ("INVENTORY_URL", ""),
    ("INVENTORY_PORT", ""),
    ("AGENT_DOCKER_IMAGE", ""),
    ("KUBECONFIG_GENERATE_IMAGE", ""),
    ("BASE_DNS_DOMAINS", ""),
]


def read_yaml():
    if not os.path.exists(CM_PATH):
        return
    with open(CM_PATH, "r+") as cm_file:
        return yaml.load(cm_file)


def get_relevant_envs():
    data = {}
    for env in ENVS:
        evn_data = os.getenv(env[0], env[1])
        # Set value as empty if variable is an empty string (e.g. defaulted in Makefile)
        if evn_data == '""':
            data[env[0]] = ""
        elif evn_data:
            data[env[0]] = evn_data
    return data


def set_envs_to_inventory_cm():
    cm_data = read_yaml()
    if not cm_data:
        raise Exception("%s must exists before setting envs to it" % CM_PATH)
    cm_data["data"].update(get_relevant_envs())
    with open(CM_PATH, "w") as cm_file:
        yaml.dump(cm_data, cm_file)


if __name__ == "__main__":
    set_envs_to_inventory_cm()
