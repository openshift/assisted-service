#!/usr/bin/env python
# Temporary solution for fixing the required indentation for CA bundle cert

from argparse import ArgumentParser

import yaml


def handle_arguments():
    parser = ArgumentParser()
    parser.add_argument("cert")
    parser.add_argument("configmap")

    return parser.parse_args()


def main():
    args = handle_arguments()

    with open(args.cert) as ca_bundle_file:
        ca_bundle = ca_bundle_file.read()

    with open(args.configmap, "r+") as yamlfile:
        content = yaml.safe_load(yamlfile)
        content["data"]["ca-bundle.crt"] = ca_bundle

        yaml.safe_dump(content, yamlfile)


if __name__ == "__main__":
    main()
