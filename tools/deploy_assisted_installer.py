import os
import sys
import utils

SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory.yaml")

def main():
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_IMAGE", os.environ.get("SERVICE"))
            print("Deploying {}:\n{}".format(DST_FILE, data))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
