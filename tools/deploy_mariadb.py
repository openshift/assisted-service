import os
import utils


def main():
    SRC_FILE = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-configmap.yaml")
    DST_FILE = os.path.join(os.getcwd(), "build/mariadb-configmap.yaml")
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            print("Deploying {}".format(DST_FILE))
            dst.write(data)

    utils.apply(DST_FILE)

    SRC_FILE = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-deployment.yaml")
    DST_FILE = os.path.join(os.getcwd(), "build/mariadb-deployment.yaml")
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            print("Deploying {}".format(DST_FILE))
            dst.write(data)

    utils.apply("deploy/mariadb/mariadb-storage.yaml")
    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
