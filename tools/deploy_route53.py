import os
import utils
import argparse
import deployment_options

parser = argparse.ArgumentParser()
parser.add_argument("--secret")
deploy_options = deployment_options.load_deployment_options(parser)


def deploy_secret():
    if deploy_options.secret is "":
        return

    # Renderized secret with specified secret
    src_file = os.path.join(os.getcwd(), "deploy/route53/route53-secret.yaml")
    dst_file = os.path.join(os.getcwd(), "build/route53-secret.yaml")
    topic = 'Route53 Secret'
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace("BASE64_CREDS", deploy_options.secret)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def main():
    deploy_secret()


if __name__ == "__main__":
    main()
