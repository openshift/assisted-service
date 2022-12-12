import argparse
import os

import deployment_options
import utils


def handle_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ca-file-path", default="")
    parser.add_argument("--registries-file-path", default="")

    return deployment_options.load_deployment_options(parser)


deploy_options = handle_arguments()
log = utils.get_logger("deploy-service-registry-ca-configmap")

SRC_FILE = os.path.join(
    os.getcwd(), "deploy/assisted-service-configmap-registry-ca.yaml"
)
DST_FILE = os.path.join(
    os.getcwd(),
    "build",
    deploy_options.namespace,
    "assisted-service-configmap-registry-ca.yaml",
)


def read_input_data_file(file_name):
    with open(file_name) as src:
        contents = src.read()
        contents = [f"    {elem}\n" for elem in contents.split("\n")]
        contents = "".join(contents)
        return contents


def constuct_deployment_yaml(ca_content, registries_conf_content):
    with open(SRC_FILE) as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            data = data.replace("REPLACE_WITH_TLS_CA_BUNDLE_PEM", f"{ca_content}")
            data = data.replace(
                "REPLACE_WITH_REGISTRIES_CONF", f"{registries_conf_content}"
            )
            dst.write(data)


def main():
    utils.verify_build_directory(deploy_options.namespace)
    if not deploy_options.ca_file_path or not deploy_options.registries_file_path:
        print(
            "mirror registry CA file or registries file are not provided, skipping generation of assisted-service-configmap-registry-ca.yaml"
        )
        return

    ca_content = read_input_data_file(deploy_options.ca_file_path)
    registries_conf_content = read_input_data_file(deploy_options.registries_file_path)
    constuct_deployment_yaml(ca_content, registries_conf_content)

    if not deploy_options.apply_manifest:
        return

    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=DST_FILE
    )


if __name__ == "__main__":
    main()
