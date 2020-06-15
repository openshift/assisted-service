import os
import utils
import argparse


parser = argparse.ArgumentParser()
parser.add_argument("--deploy-tag", help='Tag for all deployment images', type=str, default='latest')
args = parser.parse_args()

SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory.yaml")


def main():
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            if args.deploy_tag is not "":
                data = data.replace("REPLACE_IMAGE", "quay.io/ocpmetal/bm-inventory:{}".format(args.deploy_tag))
            else:
                data = data.replace("REPLACE_IMAGE", os.environ.get("SERVICE"))
            print("Deploying {}".format(DST_FILE))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
