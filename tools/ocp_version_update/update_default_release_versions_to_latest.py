import os
import re
import json
import copy
import logging
import argparse
import tempfile
import functools
import subprocess

from bs4 import BeautifulSoup
from distutils.version import LooseVersion

import jira
import github
import requests

logging.basicConfig(level=logging.INFO, format='%(levelname)-10s %(filename)s:%(lineno)d %(message)s')
logger = logging.getLogger(__name__)
logging.getLogger("__main__").setLevel(logging.INFO)

# Users / branch names / messages
BRANCH_NAME = "{prefix}_update_assisted_service_versions"
DEFAULT_ASSIGN = "odepaz"
DEFAULT_WATCHERS = ["odepaz", "lgamliel", "yuvalgoldberg"]
PR_MENTION = ["osherdp", "gamli75", "YuviGold"]
PR_MESSAGE = "{task}: Bump OCP versions {versions_string}"

OCP_INFO_CALL = """curl https://api.openshift.com/api/upgrades_info/v1/graph\?channel\=stable-{version}\&arch\={architecture} | jq '[.nodes[]] | sort_by(.version | split(".") | map(tonumber))[-1]'"""
OCP_INFO_FC_CALL = """curl https://api.openshift.com/api/upgrades_info/v1/graph\?channel\=candidate-{version}\&arch\={architecture} | jq '[.nodes[]] | max_by(.version)'"""

RHCOS_RELEASES = "https://mirror.openshift.com/pub/openshift-v4/{architecture}/dependencies/rhcos/{minor}"
RHCOS_PRE_RELEASE = "pre-release"

# RCHOS version
RCHOS_LIVE_ISO_URL = "https://mirror.openshift.com/pub/openshift-v4/{architecture}/dependencies/rhcos/{minor}/{version}/rhcos-{version}-{architecture}-live.{architecture}.iso"

RCHOS_VERSION_FROM_ISO_REGEX = re.compile("coreos.liveiso=rhcos-(.*) ")
DOWNLOAD_LIVE_ISO_CMD = "curl {live_iso_url} -o {out_file}"

DEFAULT_VERSIONS_FILES = os.path.join("data", "default_ocp_versions.json")
DEFAULT_OS_IMAGES_FILE = os.path.join("data", "default_os_images.json")
DEFAULT_RELEASE_IMAGES_FILE = os.path.join("data", "default_release_images.json")

# assisted-service PR related constants
ASSISTED_SERVICE_CLONE_DIR = "assisted-service"
ASSISTED_SERVICE_GITHUB_REPO_ORGANIZATION = "openshift"
ASSISTED_SERVICE_GITHUB_REPO = f"{ASSISTED_SERVICE_GITHUB_REPO_ORGANIZATION}/assisted-service"
ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER = f"https://raw.githubusercontent.com/{ASSISTED_SERVICE_GITHUB_REPO}/master"
ASSISTED_SERVICE_GITHUB_FORK_REPO = "{github_user}/assisted-service"
ASSISTED_SERVICE_CLONE_URL = "https://{github_user}:{github_password}@github.com/{ASSISTED_SERVICE_GITHUB_FORK_REPO}.git"
ASSISTED_SERVICE_UPSTREAM_URL = f"https://github.com/{ASSISTED_SERVICE_GITHUB_REPO}.git"

ASSISTED_SERVICE_MASTER_DEFAULT_OCP_VERSIONS_JSON_URL = \
    f"{ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER}/{DEFAULT_VERSIONS_FILES}"
ASSISTED_SERVICE_MASTER_DEFAULT_OS_IMAGES_JSON_URL = \
    f"{ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER}/{DEFAULT_OS_IMAGES_FILE}"
ASSISTED_SERVICE_MASTER_DEFAULT_RELEASE_IMAGES_JSON_URL = \
    f"{ASSISTED_SERVICE_GITHUB_REPO_URL_MASTER}/{DEFAULT_RELEASE_IMAGES_FILE}"
ASSISTED_SERVICE_OPENSHIFT_TEMPLATE_YAML = f"{ASSISTED_SERVICE_CLONE_DIR}/openshift/template.yaml"

OCP_REPLACE_CONTEXT = ['"{version}"', "ocp-release:{version}"]

# GitLab SSL
REDHAT_CERT_URL = 'https://password.corp.redhat.com/RH-IT-Root-CA.crt'
REDHAT_CERT_LOCATION = "/tmp/redhat.cert"
GIT_SSH_COMMAND_WITH_KEY = "ssh -o StrictHostKeyChecking=accept-new -i '{key}'"

# Jira
JIRA_SERVER = "https://issues.redhat.com"
JIRA_BROWSE_TICKET = f"{JIRA_SERVER}/browse/{{ticket_id}}"

script_dir = os.path.dirname(os.path.realpath(__file__))
CUSTOM_OPENSHIFT_IMAGES = os.path.join(script_dir, "custom_openshift_images.json")

TICKET_DESCRIPTION = "Default versions need to be updated"

SKIPPED_MAJOR_RELEASE = ["4.6"]

CPU_ARCHITECTURE_AMD64 = "amd64"
CPU_ARCHITECTURE_X86_64 = "x86_64"
CPU_ARCHITECTURE_ARM64 = "arm64"
CPU_ARCHITECTURE_AARCH64 = "aarch64"

def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("-jup",  "--jira-user-password",    help="JIRA Username and password in the format of user:pass", required=True)
    parser.add_argument("-gup",  "--github-user-password",  help="GITHUB Username and password in the format of user:pass", required=True)
    parser.add_argument("--dry-run", action='store_true',   help="test run")
    return parser.parse_args()

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


def cmd_with_git_ssh_key(key_file):
    return functools.partial(cmd, env={
        "GIT_SSH_COMMAND": GIT_SSH_COMMAND_WITH_KEY.format(key=key_file)
    })

def get_rchos_version_from_iso(minor_version, rhcos_latest_release, cpu_architecture):
    # RHCOS filename uses 'aarch64' naming
    if cpu_architecture == CPU_ARCHITECTURE_ARM64:
        cpu_architecture = CPU_ARCHITECTURE_AARCH64
    live_iso_url = RCHOS_LIVE_ISO_URL.format(minor=minor_version, version=rhcos_latest_release, architecture=cpu_architecture)
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
        result = RCHOS_VERSION_FROM_ISO_REGEX.search(zipl_info)
        rchos_version_from_iso = result.group(1)
        logger.info(f"Found rchos_version_from_iso: {rchos_version_from_iso}")
    return rchos_version_from_iso.split()[0]

def create_task(args, description: str):
    jira_client = get_jira_client(*get_login(args.jira_user_password))
    task = create_jira_ticket(jira_client, description)
    return jira_client, task


def request_json_file(json_url):
    res = requests.get(json_url)
    if not res.ok:
        raise RuntimeError(
            f"GET {json_url} failed status {res.status_code}")
    return json.loads(res.text)


def get_login(user_password):
    try:
        username, password = user_password.split(":", 1)
    except ValueError:
        logger.error("Failed to parse user:password")
        raise

    return username, password


def create_jira_ticket(jira_client, description):
    ticket_text = description
    new_task = jira_client.create_issue(project="MGMT",
                                        summary=ticket_text,
                                        priority={'name': 'Blocker'},
                                        components=[{'name': "Assisted-Installer CI"}],
                                        issuetype={'name': 'Task'},
                                        description=ticket_text)
    jira_client.assign_issue(new_task, DEFAULT_ASSIGN)
    logger.info(f"Task created: {new_task} - {JIRA_BROWSE_TICKET.format(ticket_id=new_task)}")
    return new_task


def add_watchers(jira_client, issue):
    for w in DEFAULT_WATCHERS:
        jira_client.add_watcher(issue.key, w)


def get_jira_client(username, password):
    logger.info("log-in with username: %s", username)
    return jira.JIRA(JIRA_SERVER, basic_auth=(username, password))

def clone_assisted_service(github_user, github_password):
    cmd(["rm", "-rf", ASSISTED_SERVICE_CLONE_DIR])
    assisted_service_github_fork_repo = ASSISTED_SERVICE_GITHUB_FORK_REPO.format(github_user=github_user)
    cmd(["git", "clone", ASSISTED_SERVICE_CLONE_URL.format(github_user=github_user, github_password=github_password, ASSISTED_SERVICE_GITHUB_FORK_REPO=assisted_service_github_fork_repo), ASSISTED_SERVICE_CLONE_DIR])

    def git_cmd(*args: str):
        return subprocess.check_output("git " +  " ".join(args), cwd=ASSISTED_SERVICE_CLONE_DIR, shell=True)

    git_cmd("remote", "add", "upstream", ASSISTED_SERVICE_UPSTREAM_URL)
    git_cmd("fetch", "upstream")
    git_cmd("reset", "upstream/master", "--hard")

def commit_and_push_version_update_changes(message_prefix, title):
    def git_cmd(*args: str, stdout=None):
        return subprocess.check_output("git " +  " ".join(args), cwd=ASSISTED_SERVICE_CLONE_DIR, shell=True)
    git_cmd("commit", "-a", "-m", f"\"{title}\"")
    branch = BRANCH_NAME.format(prefix=message_prefix)
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

def open_pr(args, task, title, body):
    branch = BRANCH_NAME.format(prefix=task)

    github_client = github.Github(*get_login(args.github_user_password))
    repo = github_client.get_repo(ASSISTED_SERVICE_GITHUB_REPO)
    pr = repo.create_pull(
        title=title,
        body=body,
        head=f"{github_client.get_user().login}:{branch}",
        base="master"
    )
    hold_pr(pr)
    logging.info(f"new PR opened {pr.url}")
    return pr


def hold_pr(pr):
    pr.create_issue_comment('/hold')

def unhold_pr(pr):
    pr.create_issue_comment('/unhold')


def get_latest_release_from_minor(minor_release, cpu_architecture: str):
    release_data = get_release_data(minor_release, cpu_architecture)
    return release_data['version']

def get_release_note_url(minor_release):
    release_data = get_release_data(minor_release, CPU_ARCHITECTURE_AMD64)
    try:
        release_url = release_data['metadata']["url"]
    except KeyError:
        logger.info("release has no release notes url")
        return None
    return release_url


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

    return sorted(all_relevant_releases, key=LooseVersion)[-1]

def get_all_releases(openshift_version, cpu_architecture):
    path = RHCOS_RELEASES.format(minor=openshift_version, architecture=cpu_architecture)
    res = requests.get(path)
    if not res.ok:
        return None

    page = res.text
    soup = BeautifulSoup(page, 'html.parser')
    return [node.get('href').replace("/", "") for node in soup.find_all('a')]

def get_rchos_release_from_default_version_json(rhcos_image_url):
    # Fetch version from RHCOS image URL
    return rhcos_image_url.split('/')[-2]


def is_open_update_version_ticket(args):
    jira_client = get_jira_client(*get_login(args.jira_user_password))
    open_tickets = jira_client.search_issues(f'component = "Assisted-Installer CI" AND status="TO DO" AND Summary~"{TICKET_DESCRIPTION}"', maxResults=False, fields=['summary', 'key'])
    if open_tickets:
        open_ticket_id = open_tickets[0].key
        logger.info(f"ticket {open_ticket_id} with updates waiting to get resolved, not checking for new updates until it is closed")
        return True
    return False


class NoChangesNeeded(Exception):
    pass


def main(args):
    dry_run = args.dry_run
    if dry_run:
        logger.info("Running dry-run")
    else:
        if is_open_update_version_ticket(args):
            logger.info("No updates today since there is a update waiting to be merged")
            return
        user, password = get_login(args.github_user_password)
        clone_assisted_service(user, password)

    default_release_images_json = request_json_file(ASSISTED_SERVICE_MASTER_DEFAULT_RELEASE_IMAGES_JSON_URL)
    default_os_images_json = request_json_file(ASSISTED_SERVICE_MASTER_DEFAULT_OS_IMAGES_JSON_URL)
    default_version_json = request_json_file(ASSISTED_SERVICE_MASTER_DEFAULT_OCP_VERSIONS_JSON_URL)

    updates_made = set()
    updates_made_str = set()

    update_release_images_json(default_release_images_json, updates_made, updates_made_str, dry_run)
    update_os_images_json(default_os_images_json, updates_made, updates_made_str, dry_run)
    update_ocp_versions_json(default_version_json, updates_made, updates_made_str, dry_run)

    if updates_made:
        verify_latest_config()

        if dry_run:
            logger.info(f"Bump OCP versions: {updates_made_str}")
            return

        title, task = create_jira_task(updates_made_str, dry_run, args)
        create_github_pr(updates_made, title, task, args)


def update_ocp_versions_json(default_version_json, updates_made, updates_made_str, dry_run):
    updated_version_json = copy.deepcopy(default_version_json)

    for release in default_version_json:

        if release in SKIPPED_MAJOR_RELEASE:
            logger.info(f"Skipping {release} listed in the skip list")
            continue

        latest_ocp_release = get_latest_release_from_minor(release, CPU_ARCHITECTURE_AMD64)
        if not latest_ocp_release:
            logger.info(f"No release found for {release}, continuing")
            continue

        current_default_ocp_release = default_version_json.get(release).get("display_name")

        if current_default_ocp_release != latest_ocp_release:

            updates_made.add(release)
            updates_made_str.add(f"release {current_default_ocp_release} -> {latest_ocp_release}")

            logger.info(f"New latest ocp release available for {release}, {current_default_ocp_release} -> {latest_ocp_release}")
            updated_version_release = updated_version_json[release]
            updated_version_release["display_name"] = latest_ocp_release

            # 'release_version' and 'release_image' are optional (used as a fallback if missing in default_release_images json)
            if "release_version" in updated_version_release:
                updated_version_release["release_version"] = latest_ocp_release
            if "release_image" in updated_version_release:
                updated_version_release["release_image"] = updated_version_json[release]["release_image"].replace(current_default_ocp_release, latest_ocp_release)


        # rhcos_image/rhcos_rootfs/rhcos_version are optional (used as a fallback if missing in default_os_images json)
        if not all (k in updated_version_json[release] for k in ("rhcos_image","rhcos_rootfs","rhcos_version")):
            continue

        rhcos_image_url = updated_version_json[release]['rhcos_image']
        rhcos_default_release = get_rchos_release_from_default_version_json(rhcos_image_url)

        # Get all releases for minor versions. If not available, fallback to pre-releases.
        rhcos_latest_of_releases = get_all_releases(release, CPU_ARCHITECTURE_X86_64)
        pre_release = False
        if not rhcos_latest_of_releases:
            rhcos_latest_of_releases = get_all_releases(RHCOS_PRE_RELEASE, CPU_ARCHITECTURE_X86_64)
            pre_release = True
        rhcos_latest_release = get_latest_rhcos_release_from_minor(release, rhcos_latest_of_releases, pre_release)

        if rhcos_default_release != rhcos_latest_release:

            updates_made.add(release)
            updates_made_str.add(f"rhcos {rhcos_default_release} -> {rhcos_latest_release}")

            logger.info(f"New latest rhcos release available, {rhcos_default_release} -> {rhcos_latest_release}")

            updated_version_json[release]["rhcos_image"] = updated_version_json[release]["rhcos_image"].replace(rhcos_default_release, rhcos_latest_release)
            updated_version_json[release]["rhcos_rootfs"] = updated_version_json[release]["rhcos_rootfs"].replace(rhcos_default_release, rhcos_latest_release)

            if dry_run:
                rhcos_version_from_iso = "8888888"
            else:
                minor_version = RHCOS_PRE_RELEASE if pre_release else release
                rhcos_version_from_iso = get_rchos_version_from_iso(minor_version, rhcos_latest_release, CPU_ARCHITECTURE_X86_64)
            updated_version_json[release]["rhcos_version"] = rhcos_version_from_iso

    if updates_made:
        with open(os.path.join(ASSISTED_SERVICE_CLONE_DIR, DEFAULT_VERSIONS_FILES), 'w') as outfile:
            json.dump(updated_version_json, outfile, indent=4)

    return updates_made, updates_made_str


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
        rhcos_default_release = get_rchos_release_from_default_version_json(rhcos_image_url)

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
            updated_version_json[index]["url"] = updated_version_json[index]["url"].replace(rhcos_default_release, rhcos_latest_release)
            updated_version_json[index]["rootfs_url"] = updated_version_json[index]["rootfs_url"].replace(rhcos_default_release, rhcos_latest_release)

            if dry_run:
                rhcos_version_from_iso = "8888888"
            else:
                minor_version = RHCOS_PRE_RELEASE if pre_release else openshift_version
                rhcos_version_from_iso = get_rchos_version_from_iso(minor_version, rhcos_latest_release, cpu_architecture)
            updated_version_json[index]["version"] = rhcos_version_from_iso

    if updates_made:
        with open(os.path.join(ASSISTED_SERVICE_CLONE_DIR, DEFAULT_OS_IMAGES_FILE), 'w') as outfile:
            json.dump(updated_version_json, outfile, indent=4)

    return updates_made, updates_made_str


def create_jira_task(updates_made_str, dry_run, args):
    logger.info(f"changes were made on the following versions: {updates_made_str}")

    versions_str = ", ".join(updates_made_str)

    if dry_run:
        _, task = None, "TEST-8888"
    else:
        _, task = create_task(args, TICKET_DESCRIPTION + " " + versions_str)

    title = PR_MESSAGE.format(task=task, versions_string=versions_str)
    logger.info(f"PR title will be {title}")

    return title, task


def create_github_pr(updates_made, title, task, args):
    body = get_pr_body(updates_made)

    commit_message = title + '\n\n' + get_release_notes(updates_made)

    branch = commit_and_push_version_update_changes(task, commit_message)

    github_pr = open_pr(args, task, title, body)

    github_pr.create_issue_comment(f"Running all tests")
    github_pr.create_issue_comment(f"/test all")

    unhold_pr(github_pr)


def get_pr_body(updates_made):
    body = " ".join([f"@{user}" for user in PR_MENTION])
    release_notes = get_release_notes(updates_made)
    body += "\n" + release_notes
    return body


def get_release_notes(updates_made):
    release_notes = ""
    for updated_version in updates_made:
        release_note = get_release_note_url(updated_version)
        if release_note:
            release_notes += f"{updated_version} release notes: {release_note}\n"
        else:
            release_notes += f"{updated_version} has no available release notes\n"
    return release_notes


if __name__ == "__main__":
    main(parse_args())
