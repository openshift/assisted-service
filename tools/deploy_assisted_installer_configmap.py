import argparse
import json
import os
import yaml
import socket
import subprocess

import deployment_options
import utils
from handle_ocp_versions import verify_images


def handle_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-dns-domains")
    parser.add_argument("--auth-type", default="none")
    parser.add_argument("--subsystem-test", action='store_true')
    parser.add_argument("--jwks-url", default="https://api.openshift.com/.well-known/jwks.json")
    parser.add_argument("--ocm-url", default="https://api-integration.6943.hive-integration.openshiftapps.com")
    parser.add_argument("--ocp-versions")
    parser.add_argument("--os-images")
    parser.add_argument("--release-images")
    parser.add_argument("--must-gather-images")
    parser.add_argument("--installation-timeout", type=int)
    parser.add_argument("--public-registries", default="")
    parser.add_argument("--img-expr-time", default="")
    parser.add_argument("--img-expr-interval", default="")
    parser.add_argument("--check-cvo", default="False")
    parser.add_argument("--ipv6-support", default="True")
    parser.add_argument("--iso-image-type", default="full-iso")
    parser.add_argument("--enable-sno-dnsmasq", default="True")
    parser.add_argument("--hw-requirements")
    parser.add_argument("--disabled-host-validations", default="")
    parser.add_argument("--disabled-steps", default="")
    parser.add_argument("--disk-encryption-support", default="True")
    parser.add_argument("--enable-org-tenancy", default="False")
    parser.add_argument("--enable-org-based-feature-gates", default="False")
    parser.add_argument("--allow-converged-flow", default=False, action='store_true')

    return deployment_options.load_deployment_options(parser)


deploy_options = handle_arguments()
log = utils.get_logger('deploy-service-configmap')

SRC_FILE = os.path.join(os.getcwd(), 'deploy/assisted-service-configmap.yaml')
DST_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-service-configmap.yaml')
SERVICE = "assisted-service"
IMAGE_SERVICE = "assisted-image-service"

RELEASE_SOURCES = os.environ.get("RELEASE_SOURCES", "")
OPENSHIFT_RELEASE_SYNCER_INTERVAL = os.environ.get("OPENSHIFT_RELEASE_SYNCER_INTERVAL", "30m")
IGNORED_OPENSHIFT_VERSIONS = os.environ.get("IGNORED_OPENSHIFT_VERSIONS", "")

def get_deployment_tag(args):
    if args.deploy_manifest_tag:
        return args.deploy_manifest_tag
    if args.deploy_tag:
        return args.deploy_tag


def main():
    utils.verify_build_directory(deploy_options.namespace)
    verify_images(release_images=json.loads(json.loads('"{}"'.format(deploy_options.release_images))))

    with open(SRC_FILE, "r") as src:
        data = src.read()

    data = data.replace("REPLACE_DOMAINS", '"{}"'.format(deploy_options.base_dns_domains))

    if deploy_options.apply_manifest:
        if deploy_options.target == "kind":
            assisted_service_base_url = f"http://{socket.gethostname()}"
            image_service_base_url = f"http://{socket.gethostname()}"
        else:
            assisted_service_base_url = utils.get_service_url(
                service=SERVICE,
                target=deploy_options.target,
                domain=deploy_options.domain,
                namespace=deploy_options.namespace,
                disable_tls=deploy_options.disable_tls,
                check_connection=True,
            )
            image_service_base_url = utils.get_service_url(
                service=IMAGE_SERVICE,
                target=deploy_options.target,
                domain=deploy_options.domain,
                namespace=deploy_options.namespace,
                disable_tls=deploy_options.disable_tls,
                check_connection=True,
            )

        data = data.replace("REPLACE_BASE_URL", assisted_service_base_url)
        data = data.replace("REPLACE_IMAGE_SERVICE_BASE_URL", image_service_base_url)

    data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
    data = data.replace('REPLACE_AUTH_TYPE_FLAG', '"{}"'.format(deploy_options.auth_type))
    data = data.replace('REPLACE_CHECK_CLUSTER_VERSION_FLAG', '"{}"'.format(deploy_options.check_cvo))
    data = data.replace('REPLACE_JWKS_URL', '"{}"'.format(deploy_options.jwks_url))
    data = data.replace('REPLACE_OCM_BASE_URL', '"{}"'.format(deploy_options.ocm_url))
    data = data.replace('REPLACE_OPENSHIFT_VERSIONS', '"{}"'.format(deploy_options.ocp_versions))
    data = data.replace('REPLACE_OS_IMAGES', '"{}"'.format(deploy_options.os_images))
    data = data.replace('REPLACE_RELEASE_IMAGES', '"{}"'.format(deploy_options.release_images))
    data = data.replace('REPLACE_MUST_GATHER_IMAGES', '"{}"'.format(deploy_options.must_gather_images))
    data = data.replace('REPLACE_PUBLIC_CONTAINER_REGISTRIES', '"{}"'.format(deploy_options.public_registries))
    data = data.replace('REPLACE_IPV6_SUPPORT', '"{}"'.format(deploy_options.ipv6_support))
    data = data.replace('REPLACE_ISO_IMAGE_TYPE', '"{}"'.format(deploy_options.iso_image_type))
    data = data.replace('REPLACE_HW_VALIDATOR_REQUIREMENTS', '"{}"'.format(deploy_options.hw_requirements))
    data = data.replace('REPLACE_DISABLED_HOST_VALIDATIONS', '"{}"'.format(deploy_options.disabled_host_validations))
    data = data.replace('REPLACE_DISABLED_STEPS', '"{}"'.format(deploy_options.disabled_steps))
    data = data.replace('REPLACE_RELEASE_SOURCES', "'{}'".format(RELEASE_SOURCES))
    data = data.replace('REPLACE_OPENSHIFT_RELEASE_SYNCER_INTERVAL', '"{}"'.format(OPENSHIFT_RELEASE_SYNCER_INTERVAL))
    data = data.replace('REPLACE_IGNORED_OPENSHIFT_VERSIONS', '"{}"'.format(IGNORED_OPENSHIFT_VERSIONS))

    versions = {"INSTALLER_IMAGE": "assisted-installer",
                "CONTROLLER_IMAGE": "assisted-installer-controller",
                "AGENT_DOCKER_IMAGE": "assisted-installer-agent"}
    for env_var_name, image_short_name in versions.items():
        versions[env_var_name] = deployment_options.get_image_override(deploy_options, image_short_name, env_var_name)
        log.info(f"Logging {image_short_name} information")
        log_image_revision(versions[env_var_name])

    # Edge case for controller image override
    if os.environ.get("INSTALLER_IMAGE") and not os.environ.get("CONTROLLER_IMAGE"):
        versions["CONTROLLER_IMAGE"] = deployment_options.IMAGE_FQDN_TEMPLATE.format(
            "edge-infrastructure", "assisted-installer-controller",
            deployment_options.get_tag(versions["INSTALLER_IMAGE"]))

    versions["SELF_VERSION"] = deployment_options.get_image_override(deploy_options, "assisted-service", "SERVICE")
    log.info(f"Logging assisted-service information")
    log_image_revision(versions["SELF_VERSION"])
    deploy_tag = get_deployment_tag(deploy_options)
    if deploy_tag:
        versions["RELEASE_TAG"] = deploy_tag

    y = yaml.safe_load(data)
    y['data'].update(versions)

    y['data']['ENABLE_SINGLE_NODE_DNSMASQ'] = deploy_options.enable_sno_dnsmasq
    y['data']['STORAGE'] = deploy_options.storage

    if deploy_options.installation_timeout:
        y['data']['INSTALLATION_TIMEOUT'] = str(deploy_options.installation_timeout)

    admins = get_admin_users()
    if admins:
        y['data']['ADMIN_USERS'] = admins

    if deploy_options.img_expr_time:
        y['data']['IMAGE_EXPIRATION_TIME'] = deploy_options.img_expr_time

    if deploy_options.img_expr_time:
        y['data']['IMAGE_EXPIRATION_INTERVAL'] = deploy_options.img_expr_interval

    if deploy_options.enable_kube_api:
        y['data']['ENABLE_KUBE_API'] = 'true'

    y['data']['DISK_ENCRYPTION_SUPPORT'] = deploy_options.disk_encryption_support

    y['data']['ENABLE_ORG_TENANCY'] = deploy_options.enable_org_tenancy

    y['data']['ENABLE_ORG_BASED_FEATURE_GATES'] = deploy_options.enable_org_based_feature_gates

    if deploy_options.allow_converged_flow:
        y['data']['ALLOW_CONVERGED_FLOW'] = 'true'

    with open(DST_FILE, "w+") as dst:
        dst.write(yaml.dump(y))

    if deploy_options.apply_manifest:
        log.info("Deploying {}".format(DST_FILE))
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=DST_FILE
        )

def log_image_revision(image: str):
    image_inspect = get_remote_image_inspect_json(image)
    if not image_inspect:
        return
    created = image_inspect.get("created", None)
    image_labels = image_inspect['config'].get("Labels", None)
    if not image_labels:
       log.info(f"Using image: {image}, created: {created} (image has no labels)")
       return

    git_revision = image_labels.get("git_revision", None)
    log.info(f"Using image: {image}, git_revision: {git_revision}, created: {created}")

def get_remote_image_inspect_json(image: str):

    image_inspect_str = docker_cmd(f"skopeo inspect docker://{image} --config")
    if not image_inspect_str:
        return None
    return convert_image_inspect_to_json(image_inspect_str)

def convert_image_inspect_to_json(image_inspect_str):
    try:
        image_inspect = json.loads(image_inspect_str)
    except ValueError as e:
        return None
    return image_inspect

def get_admin_users():
    admins_file = os.path.join(os.getcwd(), 'ADMINS')
    if not os.path.isfile(admins_file):
        return

    with open(admins_file) as fp:
        return ','.join([x.strip() for x in fp.readlines()])

def docker_cmd(cmd):
    try:
        out = subprocess.check_output(cmd, shell=True)
    except subprocess.CalledProcessError as e:
         return None
    return out


if __name__ == "__main__":
    main()
