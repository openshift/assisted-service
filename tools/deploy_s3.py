import os
import utils
import deployment_options

def main():
    deploy_options = deployment_options.load_deployment_options()

    src_file = os.path.join(os.getcwd(), "deploy/s3/scality-deployment.yaml")
    dst_file = os.path.join(os.getcwd(), "build/scality-deployment.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)
    utils.apply(dst_file)

    src_file = os.path.join(os.getcwd(), "deploy/s3/scality-storage.yaml")
    dst_file = os.path.join(os.getcwd(), "build/scality-storage.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)
    utils.apply(dst_file)


if __name__ == "__main__":
    main()
