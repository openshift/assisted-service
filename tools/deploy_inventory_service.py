import os
import socket

import deploy_tls_secret
import deployment_options
import utils
import yaml

deploy_options = deployment_options.load_deployment_options()


def main():
    utils.verify_build_directory(deploy_options.namespace)

    src_file = os.path.join(os.getcwd(), "deploy/assisted-service-service.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", deploy_options.namespace, "assisted-service-service.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            raw_data = src.read()
            raw_data = raw_data.replace(
                "REPLACE_NAMESPACE", f'"{deploy_options.namespace}"'
            )
            data = yaml.safe_load(raw_data)

            if deploy_options.port:
                for port_number_str, port_name in deploy_options.port:
                    port = {
                        "name": port_name,
                        "port": int(port_number_str),
                        "protocol": "TCP",
                        "targetPort": int(port_number_str),
                    }
                    data["spec"]["ports"].append(port)

            print(f"Deploying {dst_file}")
            dst.write(yaml.dump(data))

    if deploy_options.apply_manifest:
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=dst_file,
        )
    # in case of OpenShift deploy ingress as well
    if deploy_options.target == "oc-ingress":
        hostname = utils.get_service_host(
            "assisted-installer",
            deploy_options.target,
            deploy_options.domain,
            deploy_options.namespace,
        )

        if deploy_options.disable_tls:
            template = "assisted-installer-ingress.yaml"
        else:
            print(
                "WARNING: On OpenShift, in order to change TLS redirection behavior update "
                "spec/tls/insecureEdgeTerminationPolicy (None|Allow|Redirect) "
                "in the corresponding OpenShift route"
            )
            deploy_tls_secret.generate_secret(
                output_dir=os.path.join(os.getcwd(), "build"),
                service="assisted-service",
                san=hostname,
                namespace=deploy_options.namespace,
            )
            template = "assisted-installer-ingress-tls.yaml"

        deploy_ingress(
            hostname=hostname,
            namespace=deploy_options.namespace,
            template_file=template,
        )

    elif deploy_options.target == "kind":
        deploy_ingress(
            hostname=socket.gethostname(),
            namespace=deploy_options.namespace,
            template_file="assisted-installer-ingress.yaml",
        )


def deploy_ingress(hostname, namespace, template_file):
    src_file = os.path.join(os.getcwd(), "deploy", template_file)
    dst_file = os.path.join(os.getcwd(), "build", namespace, template_file)
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", namespace)
            data = data.replace("REPLACE_HOSTNAME", hostname)
            print(f"Deploying {dst_file}")
            dst.write(data)
    if deploy_options.apply_manifest:
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=dst_file,
        )


if __name__ == "__main__":
    main()
