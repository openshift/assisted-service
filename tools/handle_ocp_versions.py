#!/usr/bin/env python3

import json
import sys
import os
import tempfile
from pathlib import Path
from typing import List

from utils import check_output
from retry import retry


RELEASE_IMAGES_FILE = os.path.join("data", "default_release_images.json")


def main():
    with Path(RELEASE_IMAGES_FILE).open("r") as file_stream:
        release_images = json.load(file_stream)

    print("Verifying that images match keys", file=sys.stderr)
    verify_images(release_images)
    print(json.dumps(release_images, indent=4))


def verify_images(release_images: List[str]):
    if release_images is not None:
        for release in release_images:
            verify_release_version(release["openshift_version"], release["url"], release["version"])


def verify_release_version(ocp_version: str, release_image: str, release_version: str):
    oc_version = get_oc_version(release_image)
    assert oc_version == release_version or oc_version == release_version + "-multi", (
      f"{release_image} full version is {oc_version} not {release_version}")

    major, minor, *_other_version_components = oc_version.split(".")
    ocp_major, ocp_minor, *_ = ocp_version.split(".")
    assert (ocp_major, ocp_minor) == (major, minor), f"{release_image} major.minor key should be {major}.{minor} not {ocp_major}.{ocp_minor}"


def get_oc_version(release_image: str) -> str:
    print(f"Getting release version of {release_image}...", file=sys.stderr)
    pull_secret = os.getenv("PULL_SECRET")

    pull_secret_file = None
    if pull_secret is None:
        registry_config = ""
    else:
        try:
            json.loads(pull_secret)
        except json.JSONDecodeError as e:
            raise ValueError(f"Value of PULL_SECRET environment variable "
                             f"is not a valid JSON payload: '{pull_secret}'") from e

        with tempfile.NamedTemporaryFile(mode='w', delete=False) as f:
            f.write(pull_secret)
            pull_secret_file = f.name
            registry_config = f"--registry-config '{pull_secret_file}'"

    try:
        return get_release_information(release_image, registry_config)
    finally:
        if pull_secret_file and os.path.exists(pull_secret_file):
            os.unlink(pull_secret_file)


@retry(exceptions=RuntimeError, tries=5, delay=5)
def get_release_information(release_image, registry_config):
    return check_output(
        f"oc adm release info '{release_image}' {registry_config} -o template --template {{{{.metadata.version}}}}")


if __name__ == "__main__":
    main()
