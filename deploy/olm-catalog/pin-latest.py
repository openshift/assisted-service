#!/usr/bin/env python3

# This script scans the operator bundle manifests for container images that
# have a tag matching OPERATOR_MANIFESTS_TAG_TO_PIN (if unset, the value
# "latest" is used by default). These images will be resolved for their
# current digest at the time of running the script and will be replaced
# inline with that digest instead of the original tag.

import logging
import os
import re
from functools import lru_cache
from hashlib import sha256
from pathlib import Path
from typing import Iterable, List

import ruamel.yaml
from dxf import DXF

logging.basicConfig(level=os.environ.get("LOGLEVEL", "INFO").upper())

script_dir = Path(__file__).resolve().parent

tag_to_pin = os.environ.get("OPERATOR_MANIFESTS_TAG_TO_PIN", "latest")


# The list of manifests and which specific paths within them have to be processed
manifests = {
    "assisted-service-operator.clusterserviceversion.yaml": (
        "metadata.annotations.containerImage",
        "spec.install.spec.deployments[].spec.template.spec.containers[].env[].value",
        "spec.install.spec.deployments[].spec.template.spec.containers[].image",
        "spec.relatedImages[].image",
    )
}

ignore_patterns = {
    re.compile(pattern)
    for pattern in (
        # Pinnining can only be done if there's at-least one tag
        # that's going to point at that manifest forever. For
        # assisted components, we always have such tag. But for
        # postgres we have no control over the quay repo, so
        # we just resort to not pinning the database image. It's
        # not too important to pin it anyway, postgres is stable
        # enough.
        r".*postgresql.*",
    )
}


def yaml_config():
    yaml = ruamel.yaml.YAML()
    yaml.preserve_quotes = True
    return yaml


def main():
    yaml = yaml_config()

    for manifest, paths in manifests.items():
        fix_manifest(yaml, script_dir / "manifests" / manifest, paths)


def fix_manifest(yaml: ruamel.yaml.YAML, manifest: Path, paths: Iterable[str]):
    with manifest.open("r") as manifest_file:
        csv = yaml.load(manifest_file)

    for path in paths:
        logging.info(f"{manifest}: Fixing {path}")
        pin_path(csv, path.split("."))

    with manifest.open("w") as manifest_file:
        yaml.dump(csv, manifest_file)


def should_ignore(image_loc):
    return any(pattern.match(image_loc) for pattern in ignore_patterns)


def is_tag_matching(value: str):
    if type(value) != str:
        return False

    return value.endswith(f":{tag_to_pin}")


def parse_image_loc(image_loc: str):
    tag_splitter = ":" if "@" not in image_loc else "@"
    domain_org_repo, tag = image_loc.rsplit(tag_splitter, maxsplit=1)
    domain, org, repo = domain_org_repo.split("/")

    return domain, org, repo, tag


@lru_cache(maxsize=None)
def resolve_tag(image_loc: str):
    logging.info(f"Resolving {image_loc}")
    domain, org, repo, tag = parse_image_loc(image_loc)
    dxf = DXF(domain, f"{org}/{repo}", tag, None)
    hash = sha256(dxf.get_manifest(tag_to_pin).encode("utf-8")).hexdigest()
    resolved = f"{domain}/{org}/{repo}@sha256:{hash}"
    logging.info(f"{resolved}")
    return resolved


def pin_path(obj: dict, path: list[str]):
    logging.info(f"Iterating {'.'.join(path)}")

    current_key, *rest = path

    if not rest:
        current_value = obj.get(current_key, "")
        if is_tag_matching(current_value) and not should_ignore(current_value):
            new_value = resolve_tag(current_value)
            obj[current_key] = new_value

        return

    current_key, is_list = current_key.rstrip("[]"), current_key.endswith("[]")

    if is_list:
        for list_child in obj[current_key]:
            pin_path(list_child, rest)
    else:
        pin_path(obj[current_key], rest)


if __name__ == "__main__":
    main()
