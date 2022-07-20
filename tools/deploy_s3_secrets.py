import os
import utils
import deployment_options

def deploy(src_file):
    deploy_options = deployment_options.load_deployment_options()

    utils.verify_build_directory(deploy_options.namespace)

    src_file = os.path.join(os.getcwd(), src_file)
    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, os.path.basename(src_file))
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            print("Deploying {}".format(dst_file))
            dst.write(data)

    if not deploy_options.apply_manifest:
        return

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )

def main():
    deploy('deploy/s3/secret.yaml')

if __name__ == "__main__":
    main()
