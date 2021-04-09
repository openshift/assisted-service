import argparse
import os
import utils
import deployment_options

def handle_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--network-type", default="IPV4")

    return deployment_options.load_deployment_options(parser)


deploy_options = handle_arguments()
log = utils.get_logger('deploy-service-registry-ca-configmap')

SRC_FILE = os.path.join(os.getcwd(), 'deploy/assisted-service-configmap-registry-ca.yaml')
DST_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-service-configmap-registry-ca.yaml')

def main():
    utils.verify_build_directory(deploy_options.namespace)
    if deploy_options.network_type == "IPV6":
        with open("/etc/pki/ca-trust/extracted/pem/tls2-ca-bundle.pem", "r") as src:
            cabundle = src.read()
            cabundle = ['    {0}\n'.format(elem) for elem in cabundle.split("\n")]
            cabundle = "".join(cabundle)
    
        with open(SRC_FILE, "r") as src:
            with open(DST_FILE, "w+") as dst:
                data = src.read()
                data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
                data = data.replace('REPLACE_WITH_TLS_CA_BUNDLE_PEM',f'{cabundle}')
                print("Deploying {}".format(DST_FILE))
                dst.write(data)

        if not deploy_options.apply_manifest:
            return

        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            profile=deploy_options.profile,
            file=DST_FILE
        )


if __name__ == "__main__":
    main()
