import os
import utils
import argparse
import yaml

parser = argparse.ArgumentParser()
parser.add_argument("--target")
parser.add_argument("--domain")
parser.add_argument("--deploy-tag", help='Tag for all deployment images', type=str, default='latest')

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

            if args.deploy_tag is not "":
                versions = {"IMAGE_BUILDER": "quay.io/ocpmetal/installer-image-build:",
                            "AGENT_DOCKER_IMAGE": "quay.io/ocpmetal/agent:",
                            "KUBECONFIG_GENERATE_IMAGE": "quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:",
                            "INSTALLER_IMAGE": "quay.io/ocpmetal/assisted-installer:",
                            "CONNECTIVITY_CHECK_IMAGE": "quay.io/ocpmetal/connectivity_check:",
                            "INVENTORY_IMAGE": "quay.io/ocpmetal/inventory:",
                            "HARDWARE_INFO_IMAGE": "quay.io/ocpmetal/hardware_info:",
                            "SELF_VERSION": "quay.io/ocpmetal/installer-image-build:"}
                versions = {k: v + args.deploy_tag for k, v in versions.items()}
                y = yaml.load(data)
                y['data'].update(versions)
                data = yaml.dump(y)
            else:
                y = yaml.load(data)
                y['data'].update({"SELF_VERSION": os.environ.get("SERVICE")})
                data = yaml.dump(y)
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
