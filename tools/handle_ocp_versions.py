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
    """
    This function takes the custom definition of release image as expected by the Assisted Service
    and performs the following checks

    1) 'openshift_version' field must match at least "major.minor" version as extracted from the
       release image's "metadata.version"
    2) 'version' field must fully match the "metadata.version" field of the release image

    This logic allows us to serve release images in a simplified way so that e.g. image with
    "4.12.0-0.nightly-multi-2022-09-08-131900" as metadata can be offered as simply "4.12-nightly".

    The requirement of the full match of the 'version' field ensures awareness of what is the
    real version of the image that is being served.
    """

    oc_version = get_oc_version(release_image)
    assert release_version.startswith(oc_version), f"{oc_version} is not a prefix of {release_version}"

    # Valid delimiters for versions are "." as well as "-". This is in order to cover extraction
    # for all the following combinations
    #   * "4.11.0"
    #   * "4.11-multi"
    #   * "4.12.0-0.nightly-multi-2022-09-08-131900"
    oc_version = oc_version.replace('-', '.')
    ocp_version = ocp_version.replace('-', '.')

    major, minor, *_other_version_components = oc_version.split(".")
    ocp_major, ocp_minor, *_ = ocp_version.split(".")
    assert (ocp_major, ocp_minor) == (major, minor), f"{release_image} major.minor key should be {major}.{minor} not {ocp_major}.{ocp_minor}"


def get_oc_version(release_image: str) -> str:
    """
    This function takes OCP release image and returns the value of its "metadata.version"
    without doing any modifications of the returned value.
    """

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
