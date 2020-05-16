import os
import sys
import utils

SRC_FILE = os.path.join(os.getcwd(), "deploy/s3/scality-configmap.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/scality-configmap.yaml")
SERVICE = 'scality'

def main():
    scality_url = utils.get_service_url(SERVICE)
    scality_host = utils.get_service_host(SERVICE)
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_URL', scality_url)
            data = data.replace('REPLACE_HOST_NAME', scality_host)
            print("Deploying {}:\n{}".format(DST_FILE, data))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
