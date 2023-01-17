import argparse
import base64
import os
import requests
import yaml


# Deployment options
MINIKUBE_TARGET = 'minikube'
INGRESS_REMOTE_TARGET = 'oc-ingress'
OCP_TARGET = 'ocp'
OPENSHIFT_TARGET = 'oc'
KIND_TARGET = 'kind'

IMAGE_FQDN_TEMPLATE = "quay.io/{}/{}:{}"


def load_deployment_options(parser=None):
    if not parser:
        parser = argparse.ArgumentParser()

    parser.add_argument(
        '--namespace',
        help='Namespace for all deployment images',
        type=str,
        default='assisted-installer'
    )
    parser.add_argument(
        '--target',
        help='Target kubernetes distribution',
        choices=[MINIKUBE_TARGET, OPENSHIFT_TARGET, INGRESS_REMOTE_TARGET, OCP_TARGET, KIND_TARGET],
        default=MINIKUBE_TARGET
    )
    parser.add_argument(
        '--domain',
        help='Target domain',
        type=str
    )

    parser.add_argument(
        '--replicas-count',
        help='Replicas count of assisted-service',
        type=int,
        default=3
    )

    parser.add_argument(
        '--enable-kube-api',
        help='Assisted service support k8s api',
        type=bool,
        default=False
    )

    parser.add_argument(
        '--enable-event-stream',
        help='Assisted service support to stream events to kafka',
        type=bool,
        default=False
    )

    parser.add_argument(
        "--storage",
        help='Assisted service storage',
        type=str,
        default="s3"
    )

    parser.add_argument("--apply-manifest", type=lambda x: (str(x).lower() == 'true'), default=True)
    parser.add_argument("--persistent-storage", type=lambda x: (str(x).lower() == 'true'), default=True)
    parser.add_argument('-p', '--port', action="append", nargs=2,
                        metavar=('port', 'name'),  help="Expose a port")
    parser.add_argument("--image-pull-policy", help='Determine if the image should be pulled prior to starting the container.',
                    type=str, choices=["Always", "IfNotPresent", "Never"])

    deploy_options = parser.add_mutually_exclusive_group()
    deploy_options.add_argument("--deploy-tag", help='Tag for all deployment images', type=str)
    deploy_options.add_argument("--deploy-manifest-tag", help='Tag of the assisted-installer-deployment repo to get the deployment images manifest from', type=str)
    deploy_options.add_argument("--deploy-manifest-path", help='Path to local deployment images manifest', type=str)
    deploy_options.add_argument('--disable-tls', action='store_true', help='Disable TLS for assisted service transport', default=False)

    parsed_arguments = parser.parse_args()
    if parsed_arguments.target != INGRESS_REMOTE_TARGET:
        parsed_arguments.disable_tls = True

    return parsed_arguments


def get_file_content(repo_url, revision, file_path):
    """Get a git project file content of a specific revision/tag"""
    url = "%s/contents/%s?ref=%s" % (repo_url, file_path, revision)
    response = requests.get(url)
    response.raise_for_status()
    return base64.b64decode(response.json()['content'])


def get_manifest_from_url(tag):
    manifest_file = get_file_content("https://api.github.com/repos/openshift-assisted/assisted-installer-deployment", tag, "assisted-installer.yaml")
    return yaml.safe_load(manifest_file)


def get_image_revision_from_manifest(short_image_name, manifest):
    for repo_info in manifest.values():
        for image in repo_info["images"]:
            if short_image_name == image.split('/')[-1]:
                return repo_info["revision"]
    raise Exception("Failed to find revision for image: %s" % short_image_name)


def get_tag(image_fqdn):
    return image_fqdn.split(":")[-1].replace("latest-", "")


def get_image_override(deployment_options, short_image_name, env_var_name, org="edge-infrastructure"):
    # default tag is latest
    tag = "latest"
    image_from_env = os.environ.get(env_var_name)
    if deployment_options.deploy_manifest_path:
        print("Deploying {} according to manifest: {}".format(short_image_name, deployment_options.deploy_manifest_path))
        with open(deployment_options.deploy_manifest_path, "r") as manifest_contnet:
            manifest = yaml.safe_load(manifest_contnet)
        tag = f"latest-{get_image_revision_from_manifest(short_image_name, manifest)}"
    elif deployment_options.deploy_manifest_tag:
        print("Deploying {} according to assisted-installer-deployment tag: {}".format(short_image_name, deployment_options.deploy_manifest_tag))
        manifest = get_manifest_from_url(deployment_options.deploy_manifest_tag)
        tag = f"latest-{get_image_revision_from_manifest(short_image_name, manifest)}"
    elif deployment_options.deploy_tag:
        print("Deploying {} with deploy tag {}".format(short_image_name, deployment_options.deploy_tag))
        tag = deployment_options.deploy_tag
    # In case non of the above options was used allow overriding specific images with env var
    elif image_from_env:
        print("Overriding {} deployment image according to env {}".format(short_image_name, env_var_name))
        print("{} image for deployment: {}".format(short_image_name, image_from_env))
        return image_from_env

    image_fqdn = IMAGE_FQDN_TEMPLATE.format(org, short_image_name, tag)
    print("{} image for deployment: {}".format(short_image_name, image_fqdn))
    return image_fqdn
