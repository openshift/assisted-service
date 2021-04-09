import logging
import os
import subprocess
import time
import re
import yaml
from distutils.spawn import find_executable
from functools import reduce
from typing import Optional
from deployment_options import INGRESS_REMOTE_TARGET, LOCAL_TARGET, OCP_TARGET


KUBECTL_CMD = 'kubectl'
DOCKER = "docker"
PODMAN = "podman"


def verify_build_directory(namespace):
    dirname = os.path.join(os.getcwd(), 'build', namespace)
    if os.path.isdir(dirname):
        return
    os.makedirs(dirname)
    logging.info('Created build directory: %s', dirname)


def get_logger(name, level=logging.INFO):
    fmt = '[%(levelname)s] %(asctime)s - %(name)s - %(message)s'
    formatter = logging.Formatter(fmt)
    sh = logging.StreamHandler()
    sh.setFormatter(formatter)
    log = logging.getLogger(name)
    log.setLevel(level)
    log.addHandler(sh)
    return log


def load_yaml_file_docs(basename):
    src_file = os.path.join(os.getcwd(), basename)
    with open(src_file) as fp:
        return list(yaml.load_all(fp, Loader=yaml.SafeLoader))


def dump_yaml_file_docs(basename, docs):
    dst_file = os.path.join(os.getcwd(), basename)
    with open(dst_file, 'w') as fp:
        yaml.dump_all(docs, fp, Dumper=yaml.SafeDumper)

    return dst_file


def set_namespace_in_yaml_docs(docs, ns):
    for doc in docs:
        try:
            if 'namespace' in doc['metadata']:
                doc['metadata']['namespace'] = ns
        except KeyError:
            continue


def check_output(command, raise_on_error=True):
    process = subprocess.run(
        command,
        shell=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        universal_newlines=True
    )

    out = process.stdout.strip()
    err = process.stderr.strip()

    if raise_on_error and process.returncode != 0:
        raise RuntimeError(
            f'command={command} exited with an '
            f'error={err if err else out} '
            f'code={process.returncode}'
        )

    return out if out else err


def get_service_host(
        service,
        target=None,
        domain='',
        namespace='assisted-installer',
        ):
    if target == INGRESS_REMOTE_TARGET:
        domain = get_domain(domain, target, namespace)
        host = f'{service}.{domain}'
    elif target == OCP_TARGET:
        kubectl_cmd = get_kubectl_command(target, namespace)
        cmd = f'{kubectl_cmd} get nodes -o=jsonpath={{.items[0].status.addresses[0].address}}'
        host = check_output(cmd)
    else:
        kubectl_cmd = get_kubectl_command(target, namespace)
        cmd = f'{kubectl_cmd} get service {service} | grep {service}'
        reply = check_output(cmd)[:-1].split()
        host = reply[3]
    return host.strip()


def get_service_port(
        service,
        target=None,
        namespace='assisted-installer',
        ):
    kubectl_cmd = get_kubectl_command(target, namespace)
    cmd = f'{kubectl_cmd} get service {service} | grep {service}'
    reply = check_output(cmd)[:-1].split()
    ports = reply[4].split(":")
    port = ports[0] if target != OCP_TARGET else ports[1].split("/")[0]
    return port.strip()


def get_service_url(
        service: str,
        target: Optional[str] = None,
        domain: str = '',
        namespace: str = 'assisted-installer',
        disable_tls: bool = False
        ) -> str:
    # TODO: delete once rename everything to assisted-installer
    if target == INGRESS_REMOTE_TARGET:
        domain = get_domain(domain, target, namespace)
        service_host = f"assisted-installer.{domain}"
        return to_url(service_host, disable_tls)
    else:
        service_host = get_service_host(
            service,
            target,
            namespace=namespace
        )
        service_port = get_service_port(
            service,
            target,
            namespace=namespace
        )

    return to_url(host=service_host, port=service_port, disable_tls=disable_tls)


def to_url(host, port=None, disable_tls=False):
    protocol = 'http' if disable_tls else 'https'
    port = port if port else 80 if disable_tls else 443
    return f'{protocol}://{host}:{port}'


def apply(target, namespace, file):
    kubectl_cmd = get_kubectl_command(target, namespace)
    print(check_output(f'{kubectl_cmd} apply -f {file}'))


def get_domain(domain="", target=None, namespace='assisted-installer'):
    if domain:
        return domain
    kubectl_cmd = get_kubectl_command(target, namespace)
    cmd = f'{kubectl_cmd} get ingresscontrollers.operator.openshift.io -n openshift-ingress-operator -o custom-columns=:.status.domain'
    return check_output(cmd).split()[-1]


def check_k8s_rollout(
        k8s_object,
        k8s_object_name,
        target,
        namespace='assisted-installer',
        ):
    kubectl_cmd = get_kubectl_command(target, namespace)
    cmd = f'{kubectl_cmd} rollout status {k8s_object}/{k8s_object_name}'
    return check_output(cmd)


def wait_for_rollout(
        k8s_object,
        k8s_object_name,
        target,
        namespace='assisted-installer',
        limit=10,
        desired_status='successfully rolled out'
        ):
    # Wait for the element to ensure it exists
    for x in range(0, limit):
        try:
            status = check_if_exists(
                k8s_object=k8s_object,
                k8s_object_name=k8s_object_name,
                target=target,
                namespace=namespace
            )
            if status:
                break
            else:
                time.sleep(5)
        except:
            time.sleep(5)

    # Wait for the object to raise up
    for x in range(0, limit):
        status = check_k8s_rollout(
            k8s_object=k8s_object,
            k8s_object_name=k8s_object_name,
            target=target,
            namespace=namespace
        )
        print("Waiting for {}/{} to be ready".format(k8s_object, k8s_object_name))
        if desired_status in status:
            break
        else:
            time.sleep(5)


def get_config_value(key, cfg):
    return reduce(lambda c, k: c[k], key.split('.'), cfg)


def get_yaml_field(field, yaml_path):
    with open(yaml_path) as yaml_file:
        manifest = yaml.safe_load(yaml_file)
        _field = get_config_value(field, manifest)

    return _field


def check_if_exists(
        k8s_object,
        k8s_object_name,
        target=None,
        namespace='assisted-installer',
        ):
    try:
        kubectl_cmd = get_kubectl_command(target, namespace)
        cmd = f'{kubectl_cmd} get {k8s_object} {k8s_object_name} --no-headers'
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


def get_kubectl_command(target=None, namespace=None):
    cmd = KUBECTL_CMD

    if namespace:
        cmd += f' --namespace {namespace}'

    if target == OCP_TARGET:
        kubeconfig = os.environ.get("OCP_KUBECONFIG")
        if kubeconfig is None:
            kubeconfig = "build/kubeconfig"
        cmd += f' --kubeconfig {kubeconfig}'
        return cmd

    return cmd


def get_cluster_server(cluster_name='default'):
    p = subprocess.Popen(
        f"""kubectl config view -o jsonpath='{{.clusters[?(@.name == "{cluster_name}")].cluster.server}}'""",
        shell=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    out = p.stdout.read().decode().strip()
    err = p.stderr.read().decode().strip()
    if err:
        raise RuntimeError(
            f'failed to get server ip for cluster {cluster_name}: {err}'
        )

    return out
