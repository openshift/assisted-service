'''
This script deploy Prometheus Operator and the instance of it on K8s and OCP
n the OCP case, it will be integrated automatically with OCP oauth.
'''

import os
import sys
from time import sleep
import argparse
import secrets
import utils
import deployment_options

parser = argparse.ArgumentParser()
parser.add_argument("--target")
deploy_options = deployment_options.load_deployment_options(parser)


if deploy_options.target != "oc-ingress":
    CMD_BIN = 'kubectl'
    OLM_NS = 'olm'
    CAT_SRC = 'operatorhubio-catalog'
else:
    CMD_BIN = 'oc'
    OLM_NS = 'openshift-marketplace'
    CAT_SRC = 'community-operators'

def deploy_oauth_reqs():
    '''oauth Integration in OCP'''
    ## Token generation for session_secret
    session_secret = secrets.token_hex(43)
    secret_name = 'prometheus-k8s-proxy'
    if not utils.check_if_exists('secret', secret_name, deploy_options.namespace):
        cmd = "{} -n {} create secret generic {} --from-literal=session_secret={}" \
                .format(CMD_BIN, deploy_options.namespace, secret_name, session_secret)
        utils.check_output(cmd)

    ## Annotate Serviceaccount
    json_manifest = '{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"prometheus-assisted"}}'
    sa_name = 'prometheus-k8s'
    annotation_name = 'serviceaccounts.openshift.io/oauth-redirectreference.assisted-installer-prometheus'
    cmd = "{} -n {} annotate serviceaccount {} --overwrite {}='{}'"\
            .format(CMD_BIN, deploy_options.namespace, sa_name, annotation_name, json_manifest)
    utils.check_output(cmd)

    # Download OCP Certificate as a secret
    cert_secret_name = 'openshift-custom-ca'
    cmd = "{} -n {} get secret {} --no-headers".format(CMD_BIN, deploy_options.namespace, cert_secret_name)
    cert_secret = utils.check_output(cmd)
    if not cert_secret:
        # Get OCP Certificate
        secret_name = 'router-certs-default'
        namespace = 'openshift-ingress'
        template = '{{index .data "tls.crt"}}'
        cmd = "{} get secret {} --namespace={} --template '{}'"\
                .format(CMD_BIN, secret_name, namespace, template)
        ca_cert = utils.check_output(cmd)

        # Renderized secret with CA Certificate of the OCP Cluster
        src_file = os.path.join(os.getcwd(), \
                "deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-custom-ca.yaml")
        dst_file = os.path.join(os.getcwd(), \
                "build/assisted-installer-ocp-prometheus-custom-ca.yaml")
        topic = 'OCP Custom CA'
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace("BASE64_CERT", ca_cert)
                data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
                print("Deploying {}: {}".format(topic, dst_file))
                dst.write(data)
        utils.apply(dst_file)
    else:
        print("Secret {} already exists", cert_secret_name)


def deploy_prometheus_route():
    '''Deploy Prometheus Route'''
    topic = 'Prometheus Operator Route'
    src_file = os.path.join(os.getcwd(),\
            "deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-route.yaml")
    dst_file = os.path.join(os.getcwd(),\
            "build/assisted-installer-ocp-prometheus-route.yaml")
    try:
        # I have permissions
        ingress_domain = utils.get_domain()
    except:
        # I have not permissions, yes it's ugly...
        # This ingress should be there because of UI deployment
        json_path_ingress = '{.spec.rules[0].host}'
        cmd = "{} -n {} get ingress assisted-installer -o jsonpath='{}'".format(
            CMD_BIN, deploy_options.namespace, json_path_ingress)
        assisted_installer_ingress_domain = utils.check_output(cmd)
        if assisted_installer_ingress_domain.split(".")[0] != 'assisted-installer':
            print("Error recovering the ingress route")
            sys.exit(1)

        ingress_domain = assisted_installer_ingress_domain.split(".", maxsplit=1)[1]
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace("INGRESS_DOMAIN", ingress_domain)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def deploy_prometheus_sub(olm_ns, cat_src):
    '''Deploy Operator Subscription'''
    topic = 'Prometheus Operator Subscription'
    src_file = os.path.join(os.getcwd(),\
            "deploy/monitoring/prometheus/assisted-installer-operator-subscription.yaml")
    dst_file = os.path.join(os.getcwd(),\
            "build/assisted-installer-operator-subscription.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace("CAT_SRC", cat_src).replace("OLM_NAMESPACE", olm_ns)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)
    utils.wait_for_rollout('deployment', 'prometheus-operator', deploy_options.namespace)


def deployer(src_file, topic):
    '''Wrapper for oc/kubectl apply -f'''
    src_file = os.path.join(os.getcwd(), src_file)
    dst_file = os.path.join(os.getcwd(), 'build', os.path.basename(src_file))
    with open(src_file) as fp:
        data = fp.read()
    data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
    with open(dst_file, 'w') as fp:
        fp.write(data)
    print("Deploying {}: {}".format(topic ,dst_file))
    utils.apply(dst_file)



def main():
    '''Deploy Prometheus operator and Instance '''
    if deploy_options.target != "oc-ingress":
        # Deploy Operator Group
        deployer('deploy/monitoring/prometheus/assisted-installer-operator-group.yaml',
                 'OperatorGroup')
        # Deploy Subscription
        deploy_prometheus_sub(OLM_NS, CAT_SRC)
        # Deploy Prom svc
        deployer('deploy/monitoring/prometheus/assisted-installer-k8s-prometheus-svc.yaml',
                 'Prometheus Service')
        # Deploy Prometheus Instance
        deployer('deploy/monitoring/prometheus/assisted-installer-k8s-prometheus-subscription-instance.yaml',
                 'Prometheus Instance on K8s')
        sleep(10)
        utils.check_k8s_rollout('statefulset', 'prometheus-assisted-installer-prometheus', deploy_options.namespace)
        # Deploy Prom svc Monitor
        deployer('deploy/monitoring/prometheus/assisted-installer-prometheus-svc-monitor.yaml',
                 'Prometheus Service Monitor')
    else:
        # Deploy Operator Group
        try:
            deployer('deploy/monitoring/prometheus/assisted-installer-operator-group.yaml',
                     'OperatorGroup')
        except:
            cmd = "{} -n {} get OperatorGroup --no-headers".format(CMD_BIN, deploy_options.namespace)
            if not utils.check_output(cmd):
                print("The creation of an OperatorGroup is Forbidden for you user please request a creation of one before execute this again, exiting...")
                sys.exit(1)
            else:
                print("Another OperatorGroup exists, continuing")
        # Deploy Subscription
        deploy_prometheus_sub(OLM_NS, CAT_SRC)
        # Deploy Oauth Pre-reqs for OCP integration
        deploy_oauth_reqs()
        # Deploy Prom svc;
        # We create the service first in order to self-generate the secret prometheus-k8s-tls
        deployer('deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-svc.yaml',
                 'Prometheus Service on OCP')
        # Deploy Prometheus Instance
        deployer('deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-subscription-instance.yaml',
                 'Prometheus Instance on OCP')
        sleep(10)
        utils.check_k8s_rollout('statefulset', 'prometheus-assisted-installer-prometheus', deploy_options.namespace)
        # Deploy Prom svc Monitor
        deployer('deploy/monitoring/prometheus/assisted-installer-prometheus-svc-monitor.yaml',
                 'Prometheus Service Monitor')
        # Deploy Prometheus Route
        deploy_prometheus_route()


if __name__ == "__main__":
    main()
