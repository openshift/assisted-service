import os
import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--secret")

args = parser.parse_args()


def deploy_secret():
    if args.secret is "":
        return

    # Renderized secret with specified secret
    src_file = os.path.join(os.getcwd(), "deploy/route53/route53-secret.yaml")
    dst_file = os.path.join(os.getcwd(), "build/route53-secret.yaml")
    topic = 'Route53 Secret'
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("BASE64_CREDS", args.secret)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def main():
    deploy_secret()


if __name__ == "__main__":
    main()
