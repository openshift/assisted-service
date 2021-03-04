import os
import utils
import deployment_options


def main():
    deploy_options = deployment_options.load_deployment_options()

    utils.verify_build_directory(deploy_options.namespace)

    if deploy_options.enable_kube_api:
        file_path = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'resources.yaml')

        if not deploy_options.apply_manifest:
            return

        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            profile=deploy_options.profile,
            file=file_path
        )

        file_path = os.path.join(os.getcwd(), 'hack/crds/hive.openshift.io_clusterdeployments.yaml')
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            profile=deploy_options.profile,
            file=file_path
        )


if __name__ == "__main__":
    main()
