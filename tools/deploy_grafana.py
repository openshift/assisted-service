'''
This script deploy Grafana instance of it on K8s and OCP
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
else:
    CMD_BIN = 'oc'

def deploy_oauth_reqs():
    '''oauth Integration in OCP'''
    # Token generation for session_secret
    session_secret = secrets.token_hex(43)
    secret_name = 'grafana-proxy'
    if not utils.check_if_exists('secret', secret_name, deploy_options.namespace):
        cmd = "{} -n {} create secret generic {} --from-literal=session_secret={}"\
        .format(CMD_BIN, deploy_options.namespace, secret_name, session_secret)
        utils.check_output(cmd)

    ## Create and Annotate Serviceaccount
    sa_name = 'grafana'
    if not utils.check_if_exists('sa', sa_name, deploy_options.namespace):
        cmd = "{} -n {} create serviceaccount {} ".format(CMD_BIN, deploy_options.namespace, sa_name)
        utils.check_output(cmd)
    json_manifest = '{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"grafana"}}'
    annotation_name = 'serviceaccounts.openshift.io/oauth-redirectreference.grafana'
    cmd = "{} -n {} annotate serviceaccount {} --overwrite {}='{}'".format(
        CMD_BIN, deploy_options.namespace, sa_name, annotation_name, json_manifest)
    utils.check_output(cmd)

    # Get OCP Certificate
    if not utils.check_if_exists('secret', 'openshift-custom-ca', deploy_options.namespace):
        secret_name = 'router-certs-default'
        namespace = 'openshift-ingress'
        template = '{{index .data "tls.crt"}}'
        cmd = "{} get secret {} --namespace={} --template '{}'".format(CMD_BIN, secret_name, namespace, template)
        ca_cert = utils.check_output(cmd)

        # Renderized secret with CA Certificate of the OCP Cluster
        src_file = os.path.join(os.getcwd(),\
                "deploy/monitoring/prometheus/assisted-installer-ocp-prometheus-custom-ca.yaml")
        dst_file = os.path.join(os.getcwd(),\
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


def deploy_grafana_route():
    '''Deploy Grafana Route'''
    topic = 'Grafana Route'
    src_file = os.path.join(os.getcwd(),\
            "deploy/monitoring/grafana/assisted-installer-ocp-grafana-route.yaml")
    dst_file = os.path.join(os.getcwd(),\
            "build/assisted-installer-ocp-grafana-route.yaml")
    try:
        # I have permissions
        ingress_domain = utils.get_domain(namespace=deploy_options.namespace)
    except:
        # I have not permissions, yes it's ugly...
        # This ingress should be there because of UI deployment
        json_path_ingress = '{.spec.rules[0].host}'
        cmd = "{} -n {} get ingress assisted-installer -o jsonpath='{}'".format(
            CMD_BIN, deploy_options.namespace,  json_path_ingress)
        assisted_installer_ingress_domain = utils.check_output(cmd)
        if assisted_installer_ingress_domain.split(".")[0] != 'assisted-installer':
            print("Error recovering the ingress route")
            sys.exit(1)
        ingress_domain = assisted_installer_ingress_domain.split(".", maxsplit=1)[1]

    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("INGRESS_DOMAIN", ingress_domain)
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            print("Deploying {}: {}".format(topic, dst_file))
            dst.write(data)
    utils.apply(dst_file)


def deploy_grafana_ds():
    '''Deploy grafana daemonSet'''
    secret_name = 'grafana-datasources'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/grafana/prometheus.json")
    dst_file = os.path.join(os.getcwd(), "build/prometheus.json")
    with open(src_file) as fp:
        data = fp.read()
    data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
    with open(dst_file, 'w') as fp:
        fp.write(data)
    if not utils.check_if_exists('secret', secret_name, deploy_options.namespace):
        print("Creating Grafana Datasource")
        cmd = "{} create secret generic {} --namespace={} --from-file=prometheus.yaml={}".format(CMD_BIN, secret_name, deploy_options.namespace, dst_file)
        utils.check_output(cmd)


def deploy_grafana_config(conf_file):
    '''Deploy Grafana ConfigMap'''
    secret_name = 'grafana-config'
    src_file = os.path.join(os.getcwd(), "deploy/monitoring/grafana/" + conf_file)
    dst_file = os.path.join(os.getcwd(), conf_file)
    with open(src_file) as fp:
        data = fp.read()
    data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
    with open(dst_file, 'w') as fp:
        fp.write(data)
    if not utils.check_if_exists('secret', secret_name, deploy_options.namespace):
        print("Creating Grafana Configuration")
        cmd = "{} create secret generic {} --namespace={} --from-file=grafana.ini={}".format(CMD_BIN, secret_name, deploy_options.namespace, dst_file)
        utils.check_output(cmd)
    else:
        print("Updating Grafana Configuration")
        cmd = "{} delete secret {} --namespace={}".format(CMD_BIN, secret_name, deploy_options.namespace)
        utils.check_output(cmd)
        cmd = "{} create secret generic {} --namespace={} --from-file=grafana.ini={}".format(CMD_BIN, secret_name, deploy_options.namespace, dst_file)
        utils.check_output(cmd)


def main():
    '''Deploy Grafana for Assisted Installer'''
    if deploy_options.target != "oc-ingress":
        # Deploy grafana configuration
        grafana_conf_file = 'grafana-k8s.ini'
        deploy_grafana_config(grafana_conf_file)
        # Deploy grafana DS
        deploy_grafana_ds()
        # Deploy Dashboards
        deployer('deploy/monitoring/grafana/grafana-dashboards.yaml',
                 'Grafana Dashboards')
        # Deploy Assisted Installer Dashboard
        deployer('deploy/monitoring/grafana/assisted-installer-grafana-dashboard.yaml',
                 'Grafana Assisted Installer Dashboard')
        # Deploy Grafana
        deployer('deploy/monitoring/grafana/assisted-installer-k8s-grafana.yaml',
                 'Grafana Instance on K8s')
        sleep(10)
        utils.check_k8s_rollout('deployment', 'grafana', deploy_options.namespace)
    else:
        # Deploy Oauth Pre-reqs for OCP integration
        deploy_oauth_reqs()
        # Deploy grafana configuration
        grafana_conf_file = 'grafana.ini'
        deploy_grafana_config(grafana_conf_file)
        # Deploy grafana DS
        deploy_grafana_ds()
        # Deploy Dashboards
        deployer('deploy/monitoring/grafana/grafana-dashboards.yaml',
                 'Grafana Dashboards')
        # Deploy Assisted Installer Dashboard
        deployer('deploy/monitoring/grafana/assisted-installer-grafana-dashboard.yaml',
                 'Grafana Assisted Installer Dashboard')
        # Deploy Grafana
        deployer('deploy/monitoring/grafana/assisted-installer-ocp-grafana.yaml',
                 'Grafana Instance on OCP')
        sleep(10)
        utils.check_k8s_rollout('deployment', 'grafana', deploy_options.namespace)
        # Deploy grafana Route
        deploy_grafana_route()


if __name__ == "__main__":
    main()
