import subprocess
import time
import re
import yaml
from distutils.spawn import find_executable
from functools import reduce
from typing import Optional

LOCAL_TARGET = 'minikube'
INGRESS_REMOTE_TARGET = 'oc-ingress'

MINIKUBE_CMD = 'minikube'
KUBECTL_CMD = 'kubectl'
DOCKER = "docker"
PODMAN = "podman"


def check_output(cmd):
    return subprocess.check_output(cmd, shell=True).decode("utf-8")


def set_profile(target, profile):
    if target != LOCAL_TARGET:
        return
    check_output(f'{MINIKUBE_CMD} profile {profile}')


def get_service_host(
        service,
        target=None,
        domain='',
        namespace='assisted-installer',
        profile='minikube'
            ):
    if target is None or target == LOCAL_TARGET:
        reply = check_output(
            f'{MINIKUBE_CMD} '
            f'-n {namespace} '
            f'-p {profile} '
            f'service --url {service}'
        )
        host = re.sub("http://(.*):.*", r'\1', reply)
    elif target == INGRESS_REMOTE_TARGET:
        host = "{}.{}".format(service, get_domain(domain, namespace))
    else:
        cmd = '{kubecmd} -n {ns} get service {service} | grep {service}'.format(kubecmd=KUBECTL_CMD, ns=namespace, service=service)
        reply = check_output(cmd)[:-1].split()
        host = reply[3]
    return host.strip()


def get_service_port(
        service,
        target=None,
        namespace='assisted-installer',
        profile='minikube'
        ):
    if target is None or target == LOCAL_TARGET:
        reply = check_output(
            f'{MINIKUBE_CMD} '
            f'-n {namespace} '
            f'-p {profile} '
            f'service --url {service}')
        port = reply.split(":")[-1]
    else:
        cmd = '{kubecmd} -n {ns} get service {service} | grep {service}'.format(kubecmd=KUBECTL_CMD, ns=namespace, service=service)
        reply = check_output(cmd)[:-1].split()
        port = reply[4].split(":")[0]
    return port.strip()


def get_service_url(
        service: str,
        target: Optional[str] = None,
        domain: str = '',
        namespace: str = 'assisted-installer',
        profile: str = 'minikube',
        disable_tls: bool = False
        ) -> str:
    # TODO: delete once rename everything to assisted-installer
    if target == INGRESS_REMOTE_TARGET:
        service_host = f"assisted-installer.{get_domain(domain)}"
        return to_url(service_host, disable_tls)
    else:
        service_host = get_service_host(
            service,
            target,
            namespace=namespace,
            profile=profile
        )
        service_port = get_service_port(
            service,
            target,
            namespace=namespace,
            profile=profile
        )

    return to_url(host=service_host, port=service_port, disable_tls=disable_tls)


def to_url(host, port=None, disable_tls=False):
    protocol = 'http' if disable_tls else 'https'
    port = port if port else 80 if disable_tls else 443
    return f'{protocol}://{host}:{port}'


def apply(file):
    print(check_output("kubectl apply -f {}".format(file)))


def get_domain(domain="", namespace='assisted-installer'):
    if domain:
        return domain
    cmd = '{kubecmd} -n {ns} get ingresscontrollers.operator.openshift.io -n openshift-ingress-operator -o custom-columns=:.status.domain'.format(kubecmd=KUBECTL_CMD, ns=namespace)
    return check_output(cmd).split()[-1]


def check_k8s_rollout(k8s_object, k8s_object_name, namespace="assisted-installer"):
    cmd = '{} rollout status {}/{} --namespace {}'.format('kubectl', k8s_object, k8s_object_name, namespace)
    return check_output(cmd)


def wait_for_rollout(k8s_object, k8s_object_name, namespace="assisted-installer", limit=10, desired_status="successfully rolled out"):
    # Wait for the element to ensure it exists
    for x in range(0, limit):
        try:
            status = check_if_exists(k8s_object, k8s_object_name, namespace=namespace)
            if status:
                break
            else:
                time.sleep(5)
        except:
            time.sleep(5)

    # Wait for the object to raise up
    for x in range(0, limit):
        status = check_k8s_rollout(k8s_object, k8s_object_name, namespace)
        print("Waiting for {}/{} to be ready".format(k8s_object, k8s_object_name))
        if desired_status in status:
            break
        else:
            time.sleep(5)


def get_config_value(key, cfg):
    return reduce(lambda c, k: c[k], key.split('.'), cfg)


def get_yaml_field(field, yaml_path):
    with open(yaml_path) as yaml_file:
        manifest = yaml.load(yaml_file)
        _field = get_config_value(field, manifest)

    return _field

def check_if_exists(k8s_object, k8s_object_name, namespace="assisted-installer"):
    try:
        cmd = "{} -n {} get {} {} --no-headers".format(KUBECTL_CMD, namespace, k8s_object, k8s_object_name)
        subprocess.check_output(cmd, stderr=None, shell=True).decode("utf-8")
        output = True
    except:
        output = False

    return output

def is_tool(name):
    """Check whether `name` is on PATH and marked as executable."""
    return find_executable(name) is not None


def get_runtime_command():
    if is_tool(DOCKER):
        cmd = DOCKER
    elif is_tool(PODMAN):
        cmd = PODMAN
    else:
        raise Exception("Nor %s nor %s are installed" % (PODMAN, DOCKER))
    return cmd
