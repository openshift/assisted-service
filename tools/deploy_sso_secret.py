import os
import utils
import argparse
import deployment_options

parser = argparse.ArgumentParser()
parser.add_argument("--secret")
parser.add_argument("--id")
deploy_options = deployment_options.load_deployment_options(parser)

def main():
    ocm_secret = deploy_options.secret
    if ocm_secret == "":
        ocm_secret = '""'
    ocm_id = deploy_options.id
    if ocm_id == "":
        ocm_id = '""'

    src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-sso.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-sso.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace('REPLACE_OCM_SECRET', ocm_secret)
            data = data.replace('REPLACE_OCM_ID', ocm_id)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
