import os
import sys
import utils

SRC_FILE = os.path.join(os.getcwd(), "deploy/roles/role_binding.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/role_binding.yaml")

def main():
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            print("Deploying {}:\n{}".format(DST_FILE, data))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
