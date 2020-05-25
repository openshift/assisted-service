import os
import utils


def main():
    src_file = os.path.join(os.getcwd(), "deploy/s3/scality-deployment.yaml")
    dst_file = os.path.join(os.getcwd(), "build/scality-deployment.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply("deploy/s3/scality-storage.yaml")
    utils.apply(dst_file)


if __name__ == "__main__":
    main()
