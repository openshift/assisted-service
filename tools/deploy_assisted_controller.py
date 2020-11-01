import os
import utils
import deployment_options
import pvc_size_utils
import argparse
import deployment_options

log = utils.get_logger('deploy_assisted_controller')

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--inventory-url")
    parser.add_argument("--controller-image")
    deploy_options = deployment_options.load_deployment_options(parser)

    log.info('Starting assisted-installer-controller deployment')
    utils.verify_build_directory(deploy_options.namespace)
    deploy_configmap(deploy_options)
    deploy_controller(deploy_options)
    log.info('Completed assisted-installer-controller deployment')

def deploy_controller(deploy_options):
    src_file = os.path.join(os.getcwd(), 'deploy/assisted-controller/assisted-controller-deployment.yaml')
    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-controller-deployment.yaml')
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_CONTROLLER_OCP_IMAGE', f'"{deploy_options.controller_image}"')
            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            print("Deploying {}".format(dst_file))
            dst.write(data)

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )

def deploy_configmap(deploy_options):
    src_file = os.path.join(os.getcwd(), 'deploy/assisted-controller/assisted-controller-configmap-patch.yaml')
    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-controller-configmap-patch.yaml')
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('INVENTORY_URL', f'"{deploy_options.inventory_url}"')
            print("Deploying {}".format(dst_file))
            dst.write(data)

    log.info('Deploying %s', dst_file)
    utils.patch(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file,
        resource_type="configmap",
        resource_name="assisted-installer-controller-config",
    )

if __name__ == "__main__":
    main()
