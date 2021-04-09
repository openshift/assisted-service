import argparse
import os
import utils
import deployment_options

UI_REPOSITORY = "https://github.com/openshift-metal3/facet"

log = utils.get_logger('deploy_ui')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--subsystem-test", help="deploy in subsystem mode", action="store_true")
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.verify_build_directory(deploy_options.namespace)

    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'deploy_ui.yaml')
    image_fqdn = deployment_options.get_image_override(deploy_options, "ocp-metal-ui", "UI_IMAGE")

    tag = deployment_options.get_tag(image_fqdn)
    clone_directory = os.path.join(os.getcwd(), "build/assisted-installer-ui")

    if not os.path.exists(clone_directory):
        utils.check_output(f"git clone --branch master {UI_REPOSITORY} {clone_directory}")

    cmd = f"cd {clone_directory} && git pull"

    if tag == "latest":
        log.warning("No hash specified. Will run the deployment generation script from the top of master branch")
    else:
        cmd += f" && git reset --hard {tag}"

    cmd += f" && deploy/deploy_config.sh -t {clone_directory}/deploy/ocp-metal-ui-template.yaml " \
           f"-i {image_fqdn} -n {deploy_options.namespace} > {dst_file}"

    utils.check_output(cmd)

    if deploy_options.apply_manifest:
        log.info("Deploying %s", dst_file)
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=dst_file
        )

    # in case of openshift deploy ingress as well
    if deploy_options.target == "oc-ingress":
        src_file = os.path.join(os.getcwd(), 'deploy/ui/ui_ingress.yaml')
        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'ui_ingress.yaml')
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
                data = data.replace('REPLACE_HOSTNAME', utils.get_service_host(
                    'assisted-installer-ui',
                    deploy_options.target,
                    deploy_options.domain,
                    deploy_options.namespace
                ))
                dst.write(data)
        if deploy_options.apply_manifest:
            log.info("Deploying ingress from %s", dst_file)
            utils.apply(
                target=deploy_options.target,
                namespace=deploy_options.namespace,
                file=dst_file
            )


if __name__ == "__main__":
    main()
