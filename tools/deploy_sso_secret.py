import os
import utils
import argparse
import deployment_options

parser = argparse.ArgumentParser()
parser.add_argument("--secret")
parser.add_argument("--id")
deploy_options = deployment_options.load_deployment_options(parser)

def main():
    if deploy_options.secret is "" or deploy_options.id is "":
        return
    src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-sso.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-sso.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace('REPLACE_OCM_SECRET', deploy_options.secret)
            data = data.replace('REPLACE_OCM_ID', deploy_options.id)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
