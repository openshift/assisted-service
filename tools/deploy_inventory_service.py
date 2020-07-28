import os
import utils
import argparse
import deployment_options


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target")
    parser.add_argument("--domain")
    deploy_options = deployment_options.load_deployment_options(parser)

    src_file = os.path.join(os.getcwd(), "deploy/bm-inventory-service.yaml")
    dst_file = os.path.join(os.getcwd(), "build/bm-inventory-service.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)

    # in case of openshift deploy ingress as well
    if deploy_options.target == "oc-ingress":
        src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-ingress.yaml")
        dst_file = os.path.join(os.getcwd(), "build/assisted-installer-ingress.yaml")
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
                data = data.replace("REPLACE_HOSTNAME",
                                    utils.get_service_host("assisted-installer", deploy_options.target, deploy_options.domain, deploy_options.namespace))
                print("Deploying {}".format(dst_file))
                dst.write(data)
        utils.apply(dst_file)


if __name__ == "__main__":
    main()
