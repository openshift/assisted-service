import os
import utils
from time import sleep
import argparse
import secrets
import string
import base64

parser = argparse.ArgumentParser()
parser.add_argument("--target")
args = parser.parse_args()


if args.target != "oc-ingress":
    CMD_BIN = 'kubectl'
else:
    CMD_BIN = 'oc'

def deploy_oauth_reqs():
    # Token generation for session_secret
    session_secret = secrets.token_hex(43)
    secret_name = 'grafana-proxy'
    if not utils.check_if_exists('secret', secret_name):
        cmd = "{} -n assisted-installer create secret generic {} --from-literal=session_secret={}".format(CMD_BIN, secret_name, session_secret)
        utils.check_output(cmd)

    ## Create and Annotate Serviceaccount
    sa_name = 'grafana'
    if not utils.check_if_exists('sa', sa_name):
        cmd = "{} -n assisted-installer create serviceaccount {} ".format(CMD_BIN, sa_name)
        utils.check_output(cmd)
    json_manifest = '{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"grafana"}}' 
    annotation_name = 'serviceaccounts.openshift.io/oauth-redirectreference.grafana'
    cmd = "{} -n assisted-installer annotate serviceaccount {} --overwrite {}='{}'".format(
            CMD_BIN, sa_name, annotation_name, json_manifest)
    utils.check_output(cmd)

    # Get OCP Certificate
    if not utils.check_if_exists('secret', 'openshift-custom-ca'):
        secret_name = 'router-certs-default'
        ns = 'openshift-ingress'
        template = '{{index .data "tls.crt"}}'
        cmd = "{} get secret {} --namespace={} --template '{}'".format(CMD_BIN, secret_name, ns, template)
        ca_cert = utils.check_output(cmd)

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


def deployer(src_file, topic):
    src_file = os.path.join(os.getcwd(), src_file)
    print("Deploying {}: {}".format(topic ,src_file))
    utils.apply(src_file)


def deploy_grafana_route():
    # Deploy Grafana Route
    topic = 'Grafana Route'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/grafana/assisted-installer-ocp-grafana-route.yaml")
    dst_file = os.path.join(os.getcwd(), "build/assisted-installer-ocp-grafana-route.yaml")
    ingress_domain = utils.get_domain()
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("INGRESS_DOMAIN", ingress_domain)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def deploy_grafana_ds():
    ns = 'assisted-installer'
    secret_name = 'grafana-datasources'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/grafana/prometheus.json")
    if not utils.check_if_exists('secret', secret_name):
        print("Creating Grafana Datasource")
        cmd = "{} create secret generic {} --namespace={} --from-file=prometheus.yaml={}".format(CMD_BIN, secret_name, ns, src_file)
        utils.check_output(cmd)


def deploy_grafana_config(conf_file):
    ns = 'assisted-installer'
    secret_name = 'grafana-config'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/grafana/" + conf_file)
    if not utils.check_if_exists('secret', secret_name):
        print("Creating Grafana Configuration")
        cmd = "{} create secret generic {} --namespace={} --from-file=grafana.ini={}".format(CMD_BIN, secret_name, ns, src_file)
        utils.check_output(cmd)
    else:
        print("Updating Grafana Configuration")
        cmd = "{} delete secret {} --namespace={}".format(CMD_BIN, secret_name, ns)
        utils.check_output(cmd)
        cmd = "{} create secret generic {} --namespace={} --from-file=grafana.ini={}".format(CMD_BIN, secret_name, ns, src_file)
        utils.check_output(cmd)


def main():
    if args.target != "oc-ingress":
        # Deploy grafana configuration
        grafana_conf_file = 'grafana-k8s.ini'
        deploy_grafana_config(grafana_conf_file)
        # Deploy grafana DS
        deploy_grafana_ds()
        # Deploy Dashboards
        deployer('deploy/monitoring/grafana/grafana-dashboards.yaml', 'Grafana Dashboards')
        # Deploy Assisted Installer Dashboard
        deployer('deploy/monitoring/grafana/assisted-installer-grafana-dashboard.yaml', 'Grafana Assisted Installer Dashboard')
        # Deploy Grafana 
        deployer('deploy/monitoring/grafana/assisted-installer-k8s-grafana.yaml', 'Grafana Instance on K8s')
        sleep(10)
        utils.check_k8s_rollout('deployment', 'grafana')
    else:
        # Deploy Oauth Pre-reqs for OCP integration
        deploy_oauth_reqs()
        # Deploy grafana configuration
        grafana_conf_file = 'grafana.ini'
        deploy_grafana_config(grafana_conf_file)
        # Deploy grafana DS
        deploy_grafana_ds()
        # Deploy Dashboards
        deployer('deploy/monitoring/grafana/grafana-dashboards.yaml', 'Grafana Dashboards')
        # Deploy Assisted Installer Dashboard
        deployer('deploy/monitoring/grafana/assisted-installer-grafana-dashboard.yaml', 'Grafana Assisted Installer Dashboard')
        # Deploy Grafana 
        deployer('deploy/monitoring/grafana/assisted-installer-ocp-grafana.yaml', 'Grafana Instance on OCP')
        sleep(10)
        utils.check_k8s_rollout('deployment', 'grafana')
        # Deploy grafana Route
        deploy_grafana_route()


if __name__ == "__main__":
    main()
