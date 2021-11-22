#!/usr/bin/env python3
import os
import re
import json
import copy
import netrc
import logging
import argparse
import tempfile
import textwrap
import subprocess
import uuid

import bs4
from distutils import version
import github
import requests

logging.basicConfig(level=logging.INFO, format='%(levelname)-10s %(filename)s:%(lineno)d %(message)s')
logger = logging.getLogger(__name__)
logging.getLogger(__file__).setLevel(logging.INFO)

PR_MENTION = ["romfreiman", "celebdor", "gamli75"]

OCP_INFO_CALL = """curl https://api.openshift.com/api/upgrades_info/v1/graph\?channel\=fast-{version}\&arch\={architecture} | jq '[.nodes[]] | sort_by(.version | split(".") | map(tonumber))[-1]'"""
OCP_INFO_FC_CALL = """curl https://api.openshift.com/api/upgrades_info/v1/graph\?channel\=candidate-{version}\&arch\={architecture} | jq '[.nodes[]] | max_by(.version)'"""

RHCOS_RELEASES = "https://mirror.openshift.com/pub/openshift-v4/{architecture}/dependencies/rhcos/{minor}"
RHCOS_PRE_RELEASE = "pre-release"

# RHCOS version
RHCOS_LIVE_ISO_URL = "https://mirror.openshift.com/pub/openshift-v4/{architecture}/dependencies/rhcos/{minor}/{version}/rhcos-{version}-{architecture}-live.{architecture}.iso"

RHCOS_VERSION_FROM_ISO_REGEX = re.compile("coreos.liveiso=rhcos-(.*) ")
DOWNLOAD_LIVE_ISO_CMD = "curl {live_iso_url} -o {out_file}"

DEFAULT_OS_IMAGES_FILE = os.path.join("data", "default_os_images.json")
DEFAULT_RELEASE_IMAGES_FILE = os.path.join("data", "default_release_images.json")

# assisted-service PR related constants
ASSISTED_SERVICE_CLONE_DIR = "assisted-service"
ASSISTED_SERVICE_GITHUB_REPO_ORGANIZATION = "openshift"
ASSISTED_SERVICE_GITHUB_REPO = f"{ASSISTED_SERVICE_GITHUB_REPO_ORGANIZATION}/assisted-service"
ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER = f"https://raw.githubusercontent.com/{ASSISTED_SERVICE_GITHUB_REPO}/master"
ASSISTED_SERVICE_UPSTREAM_URL = f"https://github.com/{ASSISTED_SERVICE_GITHUB_REPO}.git"

ASSISTED_SERVICE_MASTER_DEFAULT_OS_IMAGES_JSON_URL = \
    f"{ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER}/{DEFAULT_OS_IMAGES_FILE}"
ASSISTED_SERVICE_MASTER_DEFAULT_RELEASE_IMAGES_JSON_URL = \
    f"{ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER}/{DEFAULT_RELEASE_IMAGES_FILE}"
ASSISTED_SERVICE_OPENSHIFT_TEMPLATE_YAML = f"{ASSISTED_SERVICE_CLONE_DIR}/openshift/template.yaml"

OCP_REPLACE_CONTEXT = ['"{version}"', "ocp-release:{version}"]

SKIPPED_MAJOR_RELEASE = ["4.6"]

CPU_ARCHITECTURE_AMD64 = "amd64"
CPU_ARCHITECTURE_X86_64 = "x86_64"
CPU_ARCHITECTURE_ARM64 = "arm64"
CPU_ARCHITECTURE_AARCH64 = "aarch64"


def cmd(command, env=None, **kwargs):
    logging.info(f"Running command {command} with env {env} kwargs {kwargs}")

    if env is None:
        env = os.environ
    else:
        env = {**os.environ, **env}

    popen = subprocess.Popen(command, env=env, **kwargs)
    stdout, stderr = popen.communicate()

    if popen.returncode != 0:
        raise subprocess.CalledProcessError(returncode=popen.returncode, cmd=command, output=stdout, stderr=stderr)

    return stdout, stderr


def get_rhcos_version_from_iso(minor_version, rhcos_latest_release, cpu_architecture):
    # RHCOS filename uses 'aarch64' naming
    if cpu_architecture == CPU_ARCHITECTURE_ARM64:
        cpu_architecture = CPU_ARCHITECTURE_AARCH64
    live_iso_url = RHCOS_LIVE_ISO_URL.format(minor=minor_version, version=rhcos_latest_release, architecture=cpu_architecture)
    with tempfile.NamedTemporaryFile() as tmp_live_iso_file:
        subprocess.check_output(
            DOWNLOAD_LIVE_ISO_CMD.format(live_iso_url=live_iso_url, out_file=tmp_live_iso_file.name), shell=True)
        try:
            os.remove("/tmp/zipl.prm")
        except FileNotFoundError:
            pass

        subprocess.check_output(f"isoinfo -i {tmp_live_iso_file.name} -x /ZIPL.PRM\;1 > zipl.prm", shell=True, cwd="/tmp")
        with open("/tmp/zipl.prm", 'r') as f:
            zipl_info = f.read()
        result = RHCOS_VERSION_FROM_ISO_REGEX.search(zipl_info)
        rhcos_version_from_iso = result.group(1)
        logger.info(f"Found rhcos_version_from_iso: {rhcos_version_from_iso}")
    return rhcos_version_from_iso.split()[0]


def request_json_file(json_url):
    res = requests.get(json_url)
    if not res.ok:
        raise RuntimeError(
            f"GET {json_url} failed status {res.status_code}")
    return json.loads(res.text)


def git_cmd(*args: str):
    return subprocess.check_output(("git",) + args, cwd=ASSISTED_SERVICE_CLONE_DIR)


def clone_assisted_service(github_user, github_password):
    cmd(["rm", "-rf", ASSISTED_SERVICE_CLONE_DIR])

    cmd([
        "git",
        "clone",
        f"https://{github_user}:{github_password}@github.com/{github_user}/assisted-service.git",
        ASSISTED_SERVICE_CLONE_DIR,
    ])

    git_cmd("remote", "add", "upstream", ASSISTED_SERVICE_UPSTREAM_URL)
    git_cmd("fetch", "upstream")
    git_cmd("reset", "upstream/master", "--hard")


def commit_and_push_version_update_changes(title):
    git_cmd("commit", "-a", "-m", title)
    branch = f"bump/{uuid.uuid4()}"
    git_cmd("push", "origin", f"HEAD:{branch}")
    return branch


def verify_latest_config():
    try:
        cmd(["make", "generate-configuration"], cwd=ASSISTED_SERVICE_CLONE_DIR)
        cmd(["make", "generate-bundle"], cwd=ASSISTED_SERVICE_CLONE_DIR)
    except subprocess.CalledProcessError as e:
        if e.returncode == 2:
            # We run the command just for its side-effects, we don't care if it fails
            return

        raise


def open_pr(github_client, title, body, branch):
    repo = github_client.get_repo(ASSISTED_SERVICE_GITHUB_REPO)
    pr = repo.create_pull(
        title=title,
        body=body,
        head=f"{github_client.get_user().login}:{branch}",
        base="master"
    )
    logging.info(f"new PR opened {pr.html_url}")
    return pr


def get_latest_release_from_minor(minor_release, cpu_architecture: str):
    release_data = get_release_data(minor_release, cpu_architecture)
    return release_data['version']


def get_release_note_url(minor_release):
    return get_release_data(minor_release, CPU_ARCHITECTURE_AMD64)["metadata"].get("url")


def get_release_data(minor_release, cpu_architecture):
    # We're using 'x86_64' as a default architecture in the service,
    # changing to 'amd64' as used for quering the OCP release images.
    if cpu_architecture == CPU_ARCHITECTURE_X86_64:
        cpu_architecture = CPU_ARCHITECTURE_AMD64
    release_data = subprocess.check_output(OCP_INFO_CALL.format(version=minor_release, architecture=cpu_architecture), shell=True)
    release_data = json.loads(release_data)
    if not release_data:
        release_data = subprocess.check_output(OCP_INFO_FC_CALL.format(version=minor_release, architecture=cpu_architecture), shell=True)
        release_data = json.loads(release_data)
    return release_data


def is_pre_release(release):
    return ("-fc" in release or "-rc" in release) and not "nightly" in release


def get_latest_rhcos_release_from_minor(minor_release: str, all_releases: list, pre_release: bool = False):
    if pre_release:
        all_relevant_releases = [r for r in all_releases if r.startswith(minor_release) and is_pre_release(r)]
    else:
        all_relevant_releases = [r for r in all_releases if r.startswith(minor_release) and not is_pre_release(r)]

    if not all_relevant_releases:
        return None

    return sorted(all_relevant_releases, key=version.LooseVersion)[-1]


def get_all_releases(openshift_version, cpu_architecture):
    path = RHCOS_RELEASES.format(minor=openshift_version, architecture=cpu_architecture)
    res = requests.get(path)
    if not res.ok:
        return None

    page = res.text
    soup = bs4.BeautifulSoup(page, 'html.parser')
    return [node.get('href').replace("/", "") for node in soup.find_all('a')]

def get_rhcos_release_from_default_version_json(rhcos_image_url):
    # Fetch version from RHCOS image URL
    return rhcos_image_url.split('/')[-2]


def main(username, password, dry_run):
    if dry_run:
        logger.info("On dry-run mode")

    clone_assisted_service(username, password)

    default_release_images_json = request_json_file(ASSISTED_SERVICE_MASTER_DEFAULT_RELEASE_IMAGES_JSON_URL)
    default_os_images_json = request_json_file(ASSISTED_SERVICE_MASTER_DEFAULT_OS_IMAGES_JSON_URL)

    updates_made = set()
    updates_made_str = set()

    update_release_images_json(default_release_images_json, updates_made, updates_made_str, dry_run)
    update_os_images_json(default_os_images_json, updates_made, updates_made_str, dry_run)

    if not updates_made:
        return

    verify_latest_config()

    if dry_run:
        logger.info(f"Bump OCP versions: {updates_made_str}")
        logger.info(f"GitHub PR description:\n{get_pr_body(updates_made)}")
        return

    github_client = github.Github(username, password)

    title = f'Bump OCP versions {", ".join(sorted(updates_made_str))}'

    repo = github_client.get_repo(ASSISTED_SERVICE_GITHUB_REPO)
    for pull_request in repo.get_pulls(state="open", base="master"):
        if pull_request.title == title:
            logger.info("Already created PR %s for changes: %s", pull_request.html_url, updates_made_str)
            return

    create_github_pr(github_client, updates_made, title)


def update_release_images_json(default_release_images_json, updates_made, updates_made_str, dry_run):
    updated_version_json = copy.deepcopy(default_release_images_json)

    for index, release_image in enumerate(default_release_images_json):
        openshift_version = release_image["openshift_version"]
        if openshift_version in SKIPPED_MAJOR_RELEASE:
            logger.info(f"Skipping {openshift_version} listed in the skip list")
            continue

        cpu_architecture = release_image["cpu_architecture"]
        latest_ocp_release_version = get_latest_release_from_minor(openshift_version, cpu_architecture)
        if not latest_ocp_release_version:
            logger.info(f"No release found for ocp version {openshift_version}, continuing")
            continue

        current_default_release_version = release_image["version"]

        if current_default_release_version != latest_ocp_release_version:

            updates_made.add(openshift_version)
            updates_made_str.add(f"release {current_default_release_version} -> {latest_ocp_release_version}")

            logger.info(f"New latest ocp release available for {openshift_version}, {current_default_release_version} -> {latest_ocp_release_version}")
            updated_version_json[index]["version"] = latest_ocp_release_version
            updated_version_json[index]["url"] = updated_version_json[index]["url"].replace(current_default_release_version, latest_ocp_release_version)

    if updates_made:
        with open(os.path.join(ASSISTED_SERVICE_CLONE_DIR, DEFAULT_RELEASE_IMAGES_FILE), 'w') as outfile:
            json.dump(updated_version_json, outfile, indent=4)

    return updates_made, updates_made_str


def update_os_images_json(default_os_images_json, updates_made, updates_made_str, dry_run):
    updated_version_json = copy.deepcopy(default_os_images_json)

    for index, os_image in enumerate(default_os_images_json):
        openshift_version = os_image["openshift_version"]
        if openshift_version in SKIPPED_MAJOR_RELEASE:
            logger.info(f"Skipping {openshift_version} listed in the skip list")
            continue

        rhcos_image_url = os_image['url']
        rhcos_default_release = get_rhcos_release_from_default_version_json(rhcos_image_url)

        # Get all releases for minor versions. If not available, fallback to pre-releases.
        cpu_architecture = os_image["cpu_architecture"]
        rhcos_latest_of_releases = get_all_releases(openshift_version, cpu_architecture)
        pre_release = False
        if not rhcos_latest_of_releases:
            rhcos_latest_of_releases = get_all_releases(RHCOS_PRE_RELEASE, cpu_architecture)
            pre_release = True
        rhcos_latest_release = get_latest_rhcos_release_from_minor(openshift_version, rhcos_latest_of_releases, pre_release)

        if rhcos_default_release != rhcos_latest_release:
            updates_made.add(openshift_version)
            updates_made_str.add(f"rhcos {rhcos_default_release} -> {rhcos_latest_release}")

            logger.info(f"New latest rhcos release available, {rhcos_default_release} -> {rhcos_latest_release}")
            # Update rhcos image/rootfs with latest version
            rhcos_image = updated_version_json[index]["url"].replace(rhcos_default_release, rhcos_latest_release)
            rhcos_rootfs = updated_version_json[index]["rootfs_url"].replace(rhcos_default_release, rhcos_latest_release)
            if not pre_release:
                # Replace 'pre-release' with minor version
                rhcos_image = rhcos_image.replace(RHCOS_PRE_RELEASE, openshift_version)
                rhcos_rootfs = rhcos_rootfs.replace(RHCOS_PRE_RELEASE, openshift_version)
            # Update json
            updated_version_json[index]["url"] = rhcos_image
            updated_version_json[index]["rootfs_url"] = rhcos_rootfs

            if dry_run:
                rhcos_version_from_iso = "8888888"
            else:
                minor_version = RHCOS_PRE_RELEASE if pre_release else openshift_version
                rhcos_version_from_iso = get_rhcos_version_from_iso(minor_version, rhcos_latest_release, cpu_architecture)
            updated_version_json[index]["version"] = rhcos_version_from_iso

    if updates_made:
        with open(os.path.join(ASSISTED_SERVICE_CLONE_DIR, DEFAULT_OS_IMAGES_FILE), 'w') as outfile:
            json.dump(updated_version_json, outfile, indent=4)

    return updates_made, updates_made_str


def create_github_pr(github_client, updates_made, title):
    commit_message = f"NO-ISSUE: {title}\n\n{get_release_notes(updates_made)}"
    branch = commit_and_push_version_update_changes(commit_message)

    body = get_pr_body(updates_made)
    open_pr(github_client, title, body, branch)


def get_pr_body(updates_made):
    return (
        get_release_notes(updates_made) +
        textwrap.dedent(f"""
            /cc {" ".join(f"@{user}" for user in PR_MENTION)}
            /hold
        """)
    )


def get_release_notes(updates_made):
    release_notes = ""
    for updated_version in updates_made:
        release_note = get_release_note_url(updated_version)
        if release_note:
            release_notes += f"{updated_version} release notes: {release_note}\n"
        else:
            release_notes += f"{updated_version} has no available release notes\n"

    return release_notes


def get_github_credentials_from_netrc():
    credentials = netrc.netrc().authenticators("github.com")
    if credentials is None:
        return None

    return credentials[0], credentials[2]


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "-gup",
        "--github-user-password",
        help="Github's Username and password in the format of username:password",
        default=os.environ.get("GITHUB_CREDS"),
    )
    parser.add_argument(
        "--dry-run",
        action='store_true',
        help="Do not apply any changes, but rather print what would have been applied",
    )
    args = parser.parse_args()

    if args.github_user_password is None:
        credentials = get_github_credentials_from_netrc()
        if credentials is None:
            raise ValueError("No github credentials were supplied via --github-user-password CLI option, "
                             "GITHUB_CREDS env-var, or an entry in ~/.netrc")

        username, password = credentials
    else:
        try:
            username, password = args.github_user_password.split(":", 1)
        except ValueError as e:
            raise ValueError("Failed to parse user:password") from e

    main(username, password, args.dry_run)
