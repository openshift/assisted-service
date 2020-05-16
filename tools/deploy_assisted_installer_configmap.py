import os
import utils

SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory-configmap.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory-configmap.yaml")
SERVICE = "bm-inventory"

def main():
    service_host = utils.get_service_host(SERVICE)
    service_port = utils.get_service_port(SERVICE)
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_URL", '"{}"'.format(service_host))
            data = data.replace("REPLACE_PORT", '"{}"'.format(service_port))
            print("Deploying {}:\n{}".format(DST_FILE, data))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
