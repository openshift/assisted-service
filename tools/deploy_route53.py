import argparse
import os

import deployment_options
import utils

parser = argparse.ArgumentParser()
parser.add_argument("--secret")
deploy_options = deployment_options.load_deployment_options(parser)


def deploy_secret():
    if deploy_options.secret == "":
        return

    # Renderized secret with specified secret
    src_file = os.path.join(os.getcwd(), "deploy/route53/route53-secret.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "route53-secret.yaml"
    )
    topic = "Route53 Secret"
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            data = data.replace("BASE64_CREDS", deploy_options.secret)
            print(f"Deploying {topic}: {dst_file}")
            dst.write(data)
    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )


def main():
    deploy_secret()
    utils.verify_build_directory(deploy_options.namespace)


if __name__ == "__main__":
    main()
