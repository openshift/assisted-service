import argparse
import base64
import os
import requests
import yaml


IMAGE_FQDN_TEMPLATE = "quay.io/ocpmetal/{}:{}"


def load_deployment_options(parser=None):
    if not parser:
        parser = argparse.ArgumentParser()
    deploy_options = parser.add_mutually_exclusive_group()

    deploy_options.add_argument("--deploy-tag", help='Tag for all deployment images', type=str)
    deploy_options.add_argument("--deploy-manifest-tag", help='Tag of the assisted-installer-deployment repo to get the deployment images manifest from', type=str)
    deploy_options.add_argument("--deploy-manifest-path", help='Path to local deployment images manifest', type=str)
    return parser.parse_args()


def get_file_content(repo_url, revision, file_path):
    """Get a git project file content of a specific revision/tag"""
    url = "%s/contents/%s?ref=%s" % (repo_url, file_path, revision)
    response = requests.get(url)
    response.raise_for_status()
    return base64.b64decode(response.json()['content'])


def get_manifest_from_url(tag):
    manifest_file = get_file_content("https://api.github.com/repos/eranco74/assisted-installer-deployment", tag, "assisted-installer.yaml")
    return yaml.safe_load(manifest_file)


def get_image_revision_from_manifest(short_image_name, manifest):
    for repo, repo_info in manifest.items():
        if short_image_name in repo_info["images"]:
            return repo_info["revision"]
    raise Exception("Failed to find revision for image: %s" % short_image_name)


def get_image_override(deployment_options, short_image_name, env_var_name):
    # default tag is latest
    tag = "latest"
    image_from_env = os.environ.get(env_var_name)
    if deployment_options.deploy_manifest_path:
        print("Deploying {} according to manifest: {}".format(short_image_name, deployment_options.deploy_manifest_path))
        with open(deployment_options.deploy_manifest_path, "r") as manifest_contnet:
            manifest = yaml.safe_load(manifest_contnet)
        tag = get_image_revision_from_manifest(short_image_name, manifest)
    elif deployment_options.deploy_manifest_tag:
        print("Deploying {} according to assisted-installer-deployment tag: {}".format(short_image_name, deployment_options.deploy_manifest_tag))
        manifest = get_manifest_from_url(deployment_options.deploy_manifest_tag)
        tag = get_image_revision_from_manifest(short_image_name, manifest)
    elif deployment_options.deploy_tag:
        print("Deploying {} with deploy tag {}".format(short_image_name, deployment_options.deploy_tag))
        tag = deployment_options.deploy_tag
    # In case non of the above options was used allow overriding specific images with env var
    elif image_from_env:
        print("Overriding {} deployment image according to env {}".format(short_image_name, env_var_name))
        print("{} image for deployment: {}".format(short_image_name, image_from_env))
        return image_from_env

    image_fqdn = IMAGE_FQDN_TEMPLATE.format(short_image_name, tag)
    print("{} image for deployment: {}".format(short_image_name, image_fqdn))
    return image_fqdn