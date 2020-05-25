import os
import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--target")

args = parser.parse_args()


def main():
    src_file = os.path.join(os.getcwd(), "deploy/bm-inventory-service.yaml")
    dst_file = os.path.join(os.getcwd(), "build/bm-inventory-service.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)

    # in case of openshift deploy ingress as well
    if args.target == "oc-ingress":
        src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-ingress.yaml")
        dst_file = os.path.join(os.getcwd(), "build/assisted-installer-ingress.yaml")
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace("REPLACE_HOSTNAME", utils.get_service_host("assisted-installer", args.target))
                print("Deploying {}".format(dst_file))
                dst.write(data)
        utils.apply(dst_file)


if __name__ == "__main__":
    main()
