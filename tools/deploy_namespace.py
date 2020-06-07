import os
import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--deploy-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
args = parser.parse_args()


def main():
    if args.deploy_namespace is False:
        print("Not deploying namespace")
        return
    src_file = os.path.join(os.getcwd(), "deploy/namespace/namespace.yaml")
    dst_file = os.path.join(os.getcwd(), "build/namespace.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
