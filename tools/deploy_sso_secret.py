import argparse
import os

import deployment_options
import utils

parser = argparse.ArgumentParser()
parser.add_argument("--secret")
parser.add_argument("--id")
deploy_options = deployment_options.load_deployment_options(parser)


def main():
    utils.verify_build_directory(deploy_options.namespace)

    ocm_secret = deploy_options.secret
    if ocm_secret == "":
        ocm_secret = '""'
    ocm_id = deploy_options.id
    if ocm_id == "":
        ocm_id = '""'

    src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-sso.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "assisted-installer-sso.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            data = data.replace("REPLACE_OCM_SECRET", ocm_secret)
            data = data.replace("REPLACE_OCM_ID", ocm_id)
            print(f"Deploying {dst_file}")
            dst.write(data)

    if not deploy_options.apply_manifest:
        return

    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )


if __name__ == "__main__":
    main()
