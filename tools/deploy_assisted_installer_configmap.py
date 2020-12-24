import os
import utils
import argparse
import yaml
import deployment_options


def handle_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-dns-domains")
    parser.add_argument("--enable-auth", default="False")
    parser.add_argument("--subsystem-test", action='store_true')
    parser.add_argument("--jwks-url", default="https://api.openshift.com/.well-known/jwks.json")
    parser.add_argument("--ocm-url", default="https://api-integration.6943.hive-integration.openshiftapps.com")
    parser.add_argument("--ocp-versions")
    parser.add_argument("--ocp-override")
    parser.add_argument("--installation-timeout", type=int)
    parser.add_argument("--public-registries", default="")
    parser.add_argument("--img-expr-time", default="")
    parser.add_argument("--img-expr-interval", default="")

    return deployment_options.load_deployment_options(parser)


deploy_options = handle_arguments()

SRC_FILE = os.path.join(os.getcwd(), 'deploy/assisted-service-configmap.yaml')
DST_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-service-configmap.yaml')
SERVICE = "assisted-service"


def get_deployment_tag(args):
    if args.deploy_manifest_tag:
        return args.deploy_manifest_tag
    if args.deploy_tag:
        return args.deploy_tag


def main():
    log = utils.get_logger('deploy-service-configmap')
    utils.verify_build_directory(deploy_options.namespace)

    with open(SRC_FILE, "r") as src:
        with open(DST_FILE, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_DOMAINS", '"{}"'.format(deploy_options.base_dns_domains))
            data = data.replace("REPLACE_BASE_URL", utils.get_service_url(service=SERVICE,
                                                                          target=deploy_options.target,
                                                                          domain=deploy_options.domain,
                                                                          namespace=deploy_options.namespace,
                                                                          profile=deploy_options.profile,
                                                                          disable_tls=deploy_options.disable_tls))

            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            data = data.replace('REPLACE_AUTH_ENABLED_FLAG', '"{}"'.format(deploy_options.enable_auth))
            data = data.replace('REPLACE_JWKS_URL', '"{}"'.format(deploy_options.jwks_url))
            data = data.replace('REPLACE_OCM_BASE_URL', '"{}"'.format(deploy_options.ocm_url))
            data = data.replace('REPLACE_OPENSHIFT_VERSIONS', '"{}"'.format(deploy_options.ocp_versions))
            data = data.replace('REPLACE_OPENSHIFT_INSTALL_RELEASE_IMAGE', '"{}"'.format(deploy_options.ocp_override))
            data = data.replace('REPLACE_PUBLIC_CONTAINER_REGISTRIES', '"{}"'.format(deploy_options.public_registries))

            versions = {"INSTALLER_IMAGE": "assisted-installer",
                        "CONTROLLER_IMAGE": "assisted-installer-controller",
                        "AGENT_DOCKER_IMAGE": "assisted-installer-agent",
                        "CONNECTIVITY_CHECK_IMAGE": "assisted-installer-agent",
                        "INVENTORY_IMAGE": "assisted-installer-agent",
                        "FREE_ADDRESSES_IMAGE": "assisted-installer-agent",
                        "DHCP_LEASE_ALLOCATOR_IMAGE": "assisted-installer-agent",
                        "API_VIP_CONNECTIVITY_CHECK_IMAGE": "assisted-installer-agent",
                        "NTP_SYNCHRONIZER_IMAGE": "assisted-installer-agent"}
            for env_var_name, image_short_name in versions.items():
                versions[env_var_name] = deployment_options.get_image_override(deploy_options, image_short_name, env_var_name)

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

            if deploy_options.installation_timeout:
                y['data']['INSTALLATION_TIMEOUT'] = str(deploy_options.installation_timeout)

            admins = get_admin_users()
            if admins:
                y['data']['ADMIN_USERS'] = admins

            if deploy_options.img_expr_time:
                y['data']['IMAGE_EXPIRATION_TIME'] = deploy_options.img_expr_time

            if deploy_options.img_expr_time:
                y['data']['IMAGE_EXPIRATION_INTERVAL'] = deploy_options.img_expr_interval

            data = yaml.dump(y)
            dst.write(data)

    log.info("Deploying {}".format(DST_FILE))
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=DST_FILE
    )


def get_admin_users():
    admins_file = os.path.join(os.getcwd(), 'ADMINS')
    if not os.path.isfile(admins_file):
        return

    with open(admins_file) as fp:
        return ','.join([x.strip() for x in fp.readlines()])


if __name__ == "__main__":
    main()
