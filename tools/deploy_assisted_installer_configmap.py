import os
import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--target")
parser.add_argument("--domain")

args = parser.parse_args()


SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory-configmap.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory-configmap.yaml")
SERVICE = "bm-inventory"

def main():
    # TODO: delete once rename everything to assisted-installer
    if args.target == "oc-ingress":
        service_host = "assisted-installer.{}".format(utils.get_domain(args.domain))
        service_port = "80"
    else:
        service_host = utils.get_service_host(SERVICE, args.target)
        service_port = utils.get_service_port(SERVICE, args.target)
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_URL", '"{}"'.format(service_host))
            data = data.replace("REPLACE_PORT", '"{}"'.format(service_port))
            print("Deploying {}".format(DST_FILE))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
