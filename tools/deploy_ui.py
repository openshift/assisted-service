import argparse
import os
import utils
import deployment_options


def main():

    parser = argparse.ArgumentParser()
    parser.add_argument("--target")
    parser.add_argument("--domain")
    parser.add_argument("--subsystem-test", help='deploy in subsystem mode', action='store_true')
    deploy_options = deployment_options.load_deployment_options(parser)

    dst_file = os.path.join(os.getcwd(), "build/deploy_ui.yaml")
    image_fqdn = deployment_options.get_image_override(deploy_options, "ocp-metal-ui", "UI_IMAGE")
    runtime_cmd = utils.get_runtime_command()
    utils.check_output(f'{runtime_cmd} pull {image_fqdn}')
    cmd = f'{runtime_cmd} run {image_fqdn} /deploy/deploy_config.sh -i {image_fqdn} -n {deploy_options.namespace}'
    cmd += ' > {}'.format(dst_file)
    utils.check_output(cmd)
    print("Deploying {}".format(dst_file))
    utils.apply(dst_file)

    # in case of openshift deploy ingress as well
    if deploy_options.target == "oc-ingress":
        src_file = os.path.join(os.getcwd(), "deploy/ui/ui_ingress.yaml")
        dst_file = os.path.join(os.getcwd(), "build/ui_ingress.yaml")
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
                data = data.replace("REPLACE_HOSTNAME",
                                    utils.get_service_host("assisted-installer-ui", deploy_options.target, deploy_options.domain, deploy_options.namespace))
                print("Deploying {}".format(dst_file))
                dst.write(data)
        utils.apply(dst_file)


if __name__ == "__main__":
    main()
