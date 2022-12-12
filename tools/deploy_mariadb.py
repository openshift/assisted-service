import os

import deployment_options
import utils


def main():
    deploy_options = deployment_options.load_deployment_options()
    utils.verify_build_directory(deploy_options.namespace)

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-configmap.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "mariadb-configmap.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            print(f"Deploying {dst_file}")
            dst.write(data)

    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-deployment.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "mariadb-deployment.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            print(f"Deploying {dst_file}")
            dst.write(data)
    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-storage.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "mariadb-storage.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{deploy_options.namespace}"')
            try:
                kubectl_cmd = utils.get_kubectl_command(
                    target=deploy_options.target, namespace=deploy_options.namespace
                )
                size = utils.check_output(
                    f"{kubectl_cmd} get persistentvolumeclaims mariadb-pv-claim "
                    + '-o=jsonpath="{.status.capacity.storage}"'
                )
                print("Using existing disk size", size)
            except:
                size = "10Gi"
                print("Using default size", size)
            data = data.replace("REPLACE_STORAGE", size)
            print(f"Deploying {dst_file}")
            dst.write(data)

    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )


if __name__ == "__main__":
    main()
