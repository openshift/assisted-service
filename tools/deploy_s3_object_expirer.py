import os
import utils

SRC_FILE = os.path.join(os.getcwd(), "deploy/s3/s3-object-expirer-cron.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/s3-object-expirer-cron.yaml")

def main():
    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_IMAGE", os.environ.get("OBJEXP"))
            print("Deploying {}".format(DST_FILE))
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
