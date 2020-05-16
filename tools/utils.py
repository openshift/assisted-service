import os
import subprocess
import re

def check_output(cmd):
    return subprocess.check_output(cmd, shell=True).decode("utf-8")

def get_service_url(service):
    target = os.environ.get("TARGET")
    if target is None or target == "minikube":
        return check_output("minikube service --url {}".format(service))[:-1]
    else:
        cmd = 'kubectl get service {service} | grep {service}'.format(service=service)
        reply = check_output(cmd).split()
        return "".join(["http://", reply[3], ":", reply[4].split(":")[0]])

def get_service_host(service):
    target = os.environ.get("TARGET")
    if target is None or target == "minikube":
        reply = check_output("minikube service --url {}".format(service))
        return re.sub("http://(.*):.*", r'\1', reply)
    else:
        cmd = 'kubectl get service {service} | grep {service}'.format(service=service)
        reply = check_output(cmd)[:-1].split()
        return reply[3]

def get_service_port(service):
    target = os.environ.get("TARGET")
    if target is None or target == "minikube":
        reply = check_output("minikube service --url {}".format(service))
        return reply.split(":")[-1]
    else:
        cmd = 'kubectl get service {service} | grep {service}'.format(service=service)
        reply = check_output(cmd)[:-1].split()
        return reply[4].split(":")[0]

def apply(file):
    check_output("kubectl apply -f {}".format(file))
