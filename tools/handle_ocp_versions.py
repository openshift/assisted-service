#!/usr/bin/env python3

import json
import sys
import os
import tempfile
import distutils.util
from argparse import ArgumentParser
from pathlib import Path
from typing import List

from utils import check_output

OCP_VERSIONS_FILE = os.path.join("data", "default_ocp_versions.json")

# 4.8.0-0.nightly SNO require pull secret, this should allow the deployment to skip this validation.
# This entry disables the validation for that mismatch. Safe to remove this line and all
# of its usages once the deployment env use pull secret with creds for this image.
SKIP_IMAGES = ['quay.io/openshift-release-dev/ocp-release-nightly:4.8.0-0.nightly-2021-03-16-221720']


def handle_arguments():
    parser = ArgumentParser()
    parser.add_argument('--src', type=str, default=OCP_VERSIONS_FILE)
    parser.add_argument('--dest', type=str)
    parser.add_argument('--ocp-override', type=str)
    parser.add_argument('--name-override', type=str)
    parser.add_argument('--version-override', type=str)
    parser.add_argument('--skip-verify', action="store_true")

    parser.add_argument(
        '--single-version-only',
        type=distutils.util.strtobool,
        default=False,
        help='''By default use OPENSHIFT_VERSION env variable, if not
        set use ocp-override openshift version. If none of those
        are set, use the latest version from openshift versions file.''',
    )

    return parser.parse_args()


def main():
    args = handle_arguments()

    with Path(args.src).open("r") as file_stream:
        ocp_versions = json.load(file_stream)

    if args.ocp_override:
        update_openshift_versions_hashmap(ocp_versions, args.ocp_override, args.name_override, args.version_override)

    if not args.skip_verify:
        print("Verifying that images match keys", file=sys.stderr)
        verify_ocp_versions(ocp_versions)

    if bool(args.single_version_only):
        print("Using only single version of the RHCOS ISO", file=sys.stderr)
        ocp_versions = update_openshift_versions_hashmap_to_single_only(ocp_versions, args.ocp_override)

    if args.dest:
        with Path(args.dest).open("w") as file_stream:
            json.dump(ocp_versions, file_stream, indent=4)
    else:
        print(json.dumps(ocp_versions, indent=4))


def update_openshift_versions_hashmap_to_single_only(ocp_versions: dict, release_image: str):
    key = os.getenv('OPENSHIFT_VERSION')
    if key is None and release_image:
        oc_version = get_oc_version(release_image)
        major, minor, *_other_version_components = oc_version.split(".")
        key = f"{major}.{minor}"

    if key is None:
        key = get_largest_version(list(ocp_versions.keys()))

    single_version = dict()
    single_version[key] = ocp_versions[key].copy()
    return single_version


def update_openshift_versions_hashmap(ocp_versions: dict, release_image: str, name_override: str, version_override: str):
    oc_version = get_oc_version(release_image) if version_override is None else version_override
    major, minor, *_other_version_components = oc_version.split(".")
    key = f"{major}.{minor}"

    if key not in ocp_versions:
        largest_version = get_largest_version(list(ocp_versions.keys()))
        ocp_versions[key] = ocp_versions[largest_version].copy()
        ocp_versions[key].pop('default', None) # removes the 'default' key to avoid conflict

    ocp_versions[key]["release_image"] = release_image
    ocp_versions[key]["release_version"] = oc_version
    ocp_versions[key]["display_name"] = oc_version if name_override is None else name_override


def verify_ocp_versions(ocp_versions: dict):
    for key, metadata in ocp_versions.items():
        if "release_image" not in metadata:
            # in hive cluster deployment scenario, the release image is specified within an imageset
            continue
        verify_image_version(key, metadata["release_image"], metadata["release_version"])


def verify_image_version(ocp_version: str, release_image: str, release_version: str):
    if release_image in SKIP_IMAGES:
        print(f"Skipping image version {release_image}", file=sys.stderr)
        return

    oc_version = get_oc_version(release_image)
    assert oc_version == release_version, f"{release_image} full version is {oc_version} not {release_version}"

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
        return check_output(
            f"oc adm release info '{release_image}' {registry_config} -o template --template {{{{.metadata.version}}}}")
    finally:
        if pull_secret_file and os.path.exists(pull_secret_file):
            os.unlink(pull_secret_file)


def get_largest_version(versions: List[str]) -> str:
    versions.sort(key=lambda s: [int(u) for u in s.split('.')])
    return versions[-1]


if __name__ == "__main__":
    main()
