import os

import deployment_options
import utils


def main():
    deploy_options = deployment_options.load_deployment_options()

    utils.verify_build_directory(deploy_options.namespace)
    log = utils.get_logger("deploy-crd")

    if deploy_options.enable_kube_api:
        file_path = os.path.join(
            os.getcwd(), "build", deploy_options.namespace, "resources.yaml"
        )

        if not deploy_options.apply_manifest:
            log.info("Not applying manifests")
            return

        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=file_path,
        )

        utils.deploy_from_dir(log, deploy_options, "hack/crds/")


if __name__ == "__main__":
    main()
