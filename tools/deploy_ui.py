import os
import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--target")
parser.add_argument("--domain")
parser.add_argument("--deploy-tag", help='Tag for all deployment images', type=str, default='latest')

args = parser.parse_args()


def main():
    dst_file = os.path.join(os.getcwd(), "build/deploy_ui.yaml")
    cmd = 'docker run quay.io/ocpmetal/ocp-metal-ui:latest /deploy/deploy_config.sh'
    if args.deploy_tag is not "":
        cmd += ' -i quay.io/ocpmetal/ocp-metal-ui:{}'.format(args.deploy_tag)
    cmd += ' > {}'.format(dst_file)
    utils.check_output(cmd)
    print("Deploying {}".format(dst_file))
    utils.apply(dst_file)

    # in case of openshift deploy ingress as well
    if args.target == "oc-ingress":
        src_file = os.path.join(os.getcwd(), "deploy/ui/ui_ingress.yaml")
        dst_file = os.path.join(os.getcwd(), "build/ui_ingress.yaml")
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace("REPLACE_HOSTNAME",
                                    utils.get_service_host("assisted-installer-ui", args.target, args.domain))
                print("Deploying {}".format(dst_file))
                dst.write(data)
        utils.apply(dst_file)


if __name__ == "__main__":
    main()
