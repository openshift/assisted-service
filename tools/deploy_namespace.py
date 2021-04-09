import os
import utils
import argparse
import deployment_options


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--deploy-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.verify_build_directory(deploy_options.namespace)

    if deploy_options.deploy_namespace is False:
        print("Not deploying namespace")
        return
    src_file = os.path.join(os.getcwd(), 'deploy/namespace/namespace.yaml')
    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'namespace.yaml')
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )

if __name__ == "__main__":
    main()
