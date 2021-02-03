#!/usr/bin/env python3

import json
from pathlib import Path

from utils import check_output

OCP_VERSIONS_FILE = Path("default_ocp_versions.json")


def main():
    with OCP_VERSIONS_FILE.open("r") as file_stream:
        ocp_versions = json.load(file_stream)

    verify_ocp_versions(ocp_versions)


def verify_ocp_versions(ocp_versions: dict):
    for key, metadata in ocp_versions.items():
        verify_image_version(key, metadata["release_image"])


def verify_image_version(ocp_version: str, release_image: str):
    if release_image == 'registry.svc.ci.openshift.org/sno-dev/openshift-bip:0.2.0':
        print("Skipping image version verification for BiP PoC because it's 4.7 but marked as 4.8") 
        return

    major, minor, *_other_version_components = get_oc_version(release_image).split(".")
    ocp_major, ocp_minor, *_ = ocp_version.split(".")

    assert (ocp_major, ocp_minor) == (major, minor), f"{release_image} image version is {major}.{minor} not {ocp_major}.{ocp_minor}"


def get_oc_version(release_image: str) -> str:
    return check_output(f"oc adm release info '{release_image}' -o template --template {{{{.metadata.version}}}}")


if __name__ == "__main__":
    main()
