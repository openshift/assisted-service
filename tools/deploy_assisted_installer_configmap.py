import os
import utils
import argparse
import yaml
import deployment_options


SRC_FILE = os.path.join(os.getcwd(), "deploy/assisted-service-configmap.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/assisted-service-configmap.yaml")
SERVICE = "assisted-service"


def get_deployment_tag(args):
    if args.deploy_manifest_tag:
        return args.deploy_manifest_tag
    if args.deploy_tag:
        return args.deploy_tag


def handle_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target")
    parser.add_argument("--domain")
    parser.add_argument("--base-dns-domains")
    parser.add_argument("--enable-auth", default="False")
    parser.add_argument("--jwks-url", default="https://api.openshift.com/.well-known/jwks.json")

    return deployment_options.load_deployment_options(parser)


def main():
    deploy_options = handle_arguments()

    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_DOMAINS", '"{}"'.format(deploy_options.base_dns_domains))
            data = data.replace("REPLACE_BASE_URL", utils.get_service_url(SERVICE, deploy_options.target, deploy_options.domain, deploy_options.namespace))
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace('REPLACE_AUTH_ENABLED_FLAG', '"{}"'.format(deploy_options.enable_auth))
            data = data.replace('REPLACE_JWKS_URL', deploy_options.jwks_url)
            print("Deploying {}".format(DST_FILE))

            versions = {"IMAGE_BUILDER": "installer-image-build",
                        "AGENT_DOCKER_IMAGE": "agent",
                        "IGNITION_GENERATE_IMAGE": "assisted-ignition-generator",
                        "INSTALLER_IMAGE": "assisted-installer",
                        "CONTROLLER_IMAGE": "assisted-installer-controller",
                        "CONNECTIVITY_CHECK_IMAGE": "connectivity_check",
                        "INVENTORY_IMAGE": "inventory"}
            for env_var_name, image_short_name in versions.items():
                image_fqdn = deployment_options.get_image_override(deploy_options, image_short_name, env_var_name)
                versions[env_var_name] = image_fqdn

            # Edge case for controller image override
            if os.environ.get("INSTALLER_IMAGE") and not os.environ.get("CONTROLLER_IMAGE"):
                versions["CONTROLLER_IMAGE"] = deployment_options.IMAGE_FQDN_TEMPLATE.format("assisted-installer-controller",
                    deployment_options.get_tag(versions["INSTALLER_IMAGE"]))

            versions["SELF_VERSION"] = deployment_options.get_image_override(deploy_options, "assisted-service", "SERVICE")
            deploy_tag = get_deployment_tag(deploy_options)
            if deploy_tag:
                versions["RELEASE_TAG"] = deploy_tag

            y = yaml.load(data)
            y['data'].update(versions)
            data = yaml.dump(y)
            dst.write(data)

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
