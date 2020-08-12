import argparse
import os

import deploy_tls_secret
import deployment_options
import utils


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target")
    parser.add_argument("--domain")
    parser.add_argument('--enable-tls', action='store_true', default=False)
    deploy_options = deployment_options.load_deployment_options(parser)

    src_file = os.path.join(os.getcwd(), "deploy/assisted-service-service.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-service-service.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(dst_file)

    # in case of OpenShift deploy ingress as well
    if deploy_options.target == "oc-ingress":
        hostname = utils.get_service_host("assisted-service", deploy_options.target, deploy_options.domain,
                                          deploy_options.namespace)

        if deploy_options.enable_tls:
            print("WARNING: To change TLS redirection behavior update "
                  "spec/tls/insecureEdgeTerminationPolicy (None|Allow|Redirect) "
                  "in the corresponding OpenShift route")
            deploy_tls_secret.generate_secret(output_dir=os.path.join(os.getcwd(), "build"),
                                              service="assisted-service", san=hostname, namespace=deploy_options.namespace)
            template = "assisted-installer-ingress-tls.yaml"
        else:
            template = "assisted-installer-ingress.yaml"

        deploy_ingress(hostname=hostname, namespace=deploy_options.namespace, template_file=template)


def deploy_ingress(hostname, namespace, template_file):
    src_file = os.path.join(os.getcwd(), "deploy", template_file)
    dst_file = os.path.join(os.getcwd(), "build", template_file)
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', namespace)
            data = data.replace("REPLACE_HOSTNAME", hostname)
            print("Deploying {}".format(dst_file))
            dst.write(data)
    utils.apply(dst_file)


if __name__ == "__main__":
    main()
