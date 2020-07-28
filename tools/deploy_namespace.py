import os
import utils
import argparse
import deployment_options


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--deploy-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
    deploy_options = deployment_options.load_deployment_options(parser)

    if deploy_options.deploy_namespace is False:
        print("Not deploying namespace")
        return
    src_file = os.path.join(os.getcwd(), "deploy/namespace/namespace.yaml")
    dst_file = os.path.join(os.getcwd(), "build/namespace.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)


if __name__ == "__main__":
    main()
