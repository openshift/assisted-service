import os
import subprocess
import re

MINIKUBE_CMD = 'minikube -n assisted-installer'
KUBECTL_CMD = 'kubectl -n assisted-installer'


def check_output(cmd):
    return subprocess.check_output(cmd, shell=True).decode("utf-8")


def get_service_url(service, target=None):
    if target is None or target == "minikube":
        return check_output("{} service --url {}".format(MINIKUBE_CMD, service))[:-1]
    elif target == "oc-ingress":
        return "http://{}.{}".format(service, get_domain())
    else:
        cmd = '{kubecmd} get service {service} | grep {service}'.format(kubecmd=KUBECTL_CMD, service=service)
        reply = check_output(cmd).split()
        return "".join(["http://", reply[3], ":", reply[4].split(":")[0]])


def get_service_host(service, target=None):
    target = os.environ.get("TARGET")
    if target is None or target == "minikube":
        reply = check_output("{} service --url {}".format(MINIKUBE_CMD, service))
        return re.sub("http://(.*):.*", r'\1', reply)
    elif target == "oc-ingress":
        return "{}.{}".format(service, get_domain())
    else:
        cmd = '{kubecmd} get service {service} | grep {service}'.format(kubecmd=KUBECTL_CMD, service=service)
        reply = check_output(cmd)[:-1].split()
        return reply[3]


def get_service_port(service):
    target = os.environ.get("TARGET")
    if target is None or target == "minikube":
        reply = check_output("{} service --url {}".format(MINIKUBE_CMD, service))
        return reply.split(":")[-1]
    else:
        cmd = '{kubecmd} get service {service} | grep {service}'.format(kubecmd=KUBECTL_CMD, service=service)
        reply = check_output(cmd)[:-1].split()
        return reply[4].split(":")[0]


def apply(file):
    check_output("kubectl apply -f {}".format(file))


def get_domain():
    cmd = '{kubecmd} get ingresscontrollers.operator.openshift.io -n openshift-ingress-operator -o custom-columns=:.status.domain'.format(kubecmd=KUBECTL_CMD)
    return check_output(cmd).split()[-1]
