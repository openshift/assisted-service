import os
import utils
import argparse
import deployment_options


def main():
    deploy_options = deployment_options.load_deployment_options()

    src_file = os.path.join(os.getcwd(), "deploy/s3/s3-object-expirer-cron.yaml")
    dst_file = os.path.join(os.getcwd(), "build/s3-object-expirer-cron.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            image_fqdn = deployment_options.get_image_override(deploy_options, "s3-object-expirer", "OBJEXP")
            data = data.replace("REPLACE_IMAGE", image_fqdn)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
