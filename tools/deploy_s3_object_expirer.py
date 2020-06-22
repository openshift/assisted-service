import os
import utils
import argparse


parser = argparse.ArgumentParser()
parser.add_argument("--deploy-tag", help='Tag for all deployment images', type=str, default='latest')
args = parser.parse_args()


def main():
    src_file = os.path.join(os.getcwd(), "deploy/s3/s3-object-expirer-cron.yaml")
    dst_file = os.path.join(os.getcwd(), "build/s3-object-expirer-cron.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            if args.deploy_tag:
                data = data.replace("REPLACE_IMAGE", "quay.io/ocpmetal/s3-object-expirer:{}".format(args.deploy_tag))
            else:
                data = data.replace("REPLACE_IMAGE", os.environ.get("OBJEXP"))
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
