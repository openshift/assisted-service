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
    segments = get_oc_version(release_image).split(".")
    assert ocp_version == f"{segments[0]}.{segments[1]}", f"{release_image} image version is {segments[0]}.{segments[1]} not {ocp_version}"


def get_oc_version(release_image: str) -> str:
    return check_output(f"oc adm release info '{release_image}' -o template --template {{{{.metadata.version}}}}")


if __name__ == "__main__":
    main()
