import argparse
import os
import utils
import deployment_options


UI_REPOSITORY = "https://github.com/openshift-assisted/assisted-installer-ui"

log = utils.get_logger('deploy_ui')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--subsystem-test", help="deploy in subsystem mode", action="store_true")
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.verify_build_directory(deploy_options.namespace)

    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'deploy_ui.yaml')
    image_fqdn = deployment_options.get_image_override(deploy_options,
                                                       "assisted-installer-ui",
                                                       "UI_IMAGE")

    tag = deployment_options.get_tag(image_fqdn)
    clone_directory = os.path.join(os.getcwd(), "build/assisted-installer-ui")

    if not os.path.exists(clone_directory):
        utils.check_output(f"git clone --branch master {UI_REPOSITORY} {clone_directory}")

    cmd = f"cd {clone_directory} && git pull"

    if deploy_options.target == "kind":
        utils.override_service_type_definition_and_node_port(
            internal_definitions_path=f"{clone_directory}/apps/assisted-ui/deploy/deployment-template.yaml",
            internal_target_definitions_path=f"{clone_directory}/apps/assisted-ui/deploy/deployment-template.yaml",
            service_type="NodePort",
            node_port=30004
        )

    if tag == "latest":
        log.warning("No hash specified. Will run the deployment generation script from the top of master branch")
    else:
        cmd += f" && git reset --hard {tag}"

    cmd += f" && apps/assisted-ui/deploy/deploy_config.sh -t {clone_directory}/apps/assisted-ui/deploy/deployment-template.yaml " \
           f"-i {image_fqdn} -n {deploy_options.namespace} > {dst_file}"

    log.debug(f"Executing: {cmd}")
    utils.check_output(cmd)

    if deploy_options.apply_manifest:
        log.info("Deploying %s", dst_file)
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=dst_file
        )

    if deploy_options.target == "oc-ingress":
        hostname = utils.get_service_host(
            'assisted-installer-ui',
            deploy_options.target,
            deploy_options.domain,
            deploy_options.namespace
        )
    else:
        hostname = None

    # in case of openshift - deploy ingress as well
    if hostname is not None:
        src_file = os.path.join(os.getcwd(), 'deploy/ui/ui_ingress.yaml')
        with open(src_file, "r") as src:
            data = src.read()

        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'ui_ingress.yaml')
        with open(dst_file, "w+") as dst:
            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            data = data.replace('REPLACE_HOSTNAME', hostname)
            dst.write(data)

        if deploy_options.apply_manifest:
            log.info("Deploying ingress from %s", dst_file)
            utils.apply(
                target=deploy_options.target,
                namespace=deploy_options.namespace,
                file=dst_file,
            )


if __name__ == "__main__":
    main()
