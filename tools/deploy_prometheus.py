import os
import utils
from time import sleep
import argparse
import secrets
import string
import base64
from waiting import wait, TimeoutExpired

parser = argparse.ArgumentParser()
parser.add_argument("--target")
args = parser.parse_args()


if args.target != "oc-ingress":
    CMD_BIN = 'kubectl'
    olm_ns = 'olm'
    cat_src = 'operatorhubio-catalog'
else:
    CMD_BIN = 'oc'
    olm_ns = 'openshift-marketplace'
    cat_src = 'community-operators'

def deploy_oauth_reqs():
    # Token generation for session_secret
    session_secret = secrets.token_hex(43)
    secret_name = 'prometheus-k8s-proxy'
    if not utils.check_if_exists('secret', secret_name):
        cmd = "{} -n assisted-installer create secret generic {} --from-literal=session_secret={}".format(CMD_BIN, secret_name, session_secret)
        utils.check_output(cmd)

    ## Annotate Serviceaccount
    json_manifest = '{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"prometheus-assisted"}}'
    sa_name = 'prometheus-k8s'
    annotation_name = 'serviceaccounts.openshift.io/oauth-redirectreference.assisted-installer-prometheus'
    cmd = "{} -n assisted-installer annotate serviceaccount {} --overwrite {}='{}'".format(
            CMD_BIN, sa_name, annotation_name, json_manifest)
    utils.check_output(cmd)

    # Get OCP Certificate
    secret_name = 'router-certs-default'
    ns = 'openshift-ingress'
    template = '{{index .data "tls.crt"}}'
    cmd = "{} get secret {} --namespace={} --template '{}'".format(CMD_BIN, secret_name, ns, template)
    ca_cert = utils.check_output(cmd)

    # Check the Cert
    # print(base64.b64decode(ca_cert).decode('utf-8'))

    # Renderized secret with CA Certificate of the OCP Cluster
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-custom-ca.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-ocp-prometheus-custom-ca.yaml")
    topic = 'OCP Custom CA'
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("BASE64_CERT", ca_cert)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def deploy_prometheus_route():
    # Deploy Prometheus Route
    topic = 'Prometheus Operator Route'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-route.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-ocp-prometheus-route.yaml")
    ingress_domain = utils.get_domain()
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("INGRESS_DOMAIN", ingress_domain)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def deploy_prometheus_sub(olm_ns, cat_src):
    # Deploy Operator Subscription
    topic = 'Prometheus Operator Subscription'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/prometheus/assisted-installer-operator-subscription.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-operator-subscription.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("CAT_SRC", cat_src).replace("OLM_NAMESPACE", olm_ns)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)
    utils.wait_for_rollout('deployment', 'prometheus-operator')


def deployer(src_file, topic):
    src_file = os.path.join(os.getcwd(), src_file)
    print("Deploying {}: {}".format(topic ,src_file))
    utils.apply(src_file)


def main():
    if args.target != "oc-ingress":
        # Deploy Operator Group
        deployer('deploy/monitoring/prometheus/assisted-installer-operator-group.yaml', 'OperatorGroup')
        # Deploy Subscription
        deploy_prometheus_sub(olm_ns, cat_src)
        # Deploy Prom svc
        deployer('deploy/monitoring/prometheus/assisted-installer-k8s-prometheus-svc.yaml', 'Prometheus Service')
        # Deploy Prometheus Instance
        deployer('deploy/monitoring/prometheus/assisted-installer-k8s-prometheus-subscription-instance.yaml', 'Prometheus Instance on K8s')
        sleep(10)
        utils.check_k8s_rollout('statefulset', 'prometheus-assisted-installer-prometheus')
        # Deploy Prom svc Monitor
        deployer('deploy/monitoring/prometheus/assisted-installer-prometheus-svc-monitor.yaml', 'Prometheus Service Monitor')
    else:
        # Deploy Operator Group
        deployer('deploy/monitoring/prometheus/assisted-installer-operator-group.yaml', 'OperatorGroup')
        # Deploy Subscription
        deploy_prometheus_sub(olm_ns, cat_src)
        # Deploy Oauth Pre-reqs for OCP integration
        deploy_oauth_reqs()
        # Deploy Prom svc; We create the service first in order to self-generate the secret prometheus-k8s-tls
        deployer('deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-svc.yaml', 'Prometheus Service on OCP')
        # Deploy Prometheus Instance
        deployer('deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-subscription-instance.yaml', 'Prometheus Instance on OCP')
        sleep(10)
        utils.check_k8s_rollout('statefulset', 'prometheus-assisted-installer-prometheus')
        # Deploy Prom svc Monitor
        deployer('deploy/monitoring/prometheus/assisted-installer-prometheus-svc-monitor.yaml', 'Prometheus Service Monitor')
        # Deploy Prometheus Route
        deploy_prometheus_route()


if __name__ == "__main__":
    main()
