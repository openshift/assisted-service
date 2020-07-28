import os
import utils
import deployment_options

def main():
    deploy_options = deployment_options.load_deployment_options()

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-configmap.yaml")
    dst_file = os.path.join(os.getcwd(), "build/mariadb-configmap.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-deployment.yaml")
    dst_file = os.path.join(os.getcwd(), "build/mariadb-deployment.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)
    utils.apply(dst_file)

    src_file = os.path.join(os.getcwd(), "deploy/mariadb/mariadb-storage.yaml")
    dst_file = os.path.join(os.getcwd(), "build/mariadb-storage.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            try:
                size = utils.check_output(
                    f"kubectl -n {deploy_options.namespace} get persistentvolumeclaims mariadb-pv-claim " +
                    "-o=jsonpath='{.status.capacity.storage}'")
                print("Using existing disk size", size)
            except:
                size = "10Gi"
                print("Using default size", size)
            data = data.replace("REPLACE_STORAGE", size)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
