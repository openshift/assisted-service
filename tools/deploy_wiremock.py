import utils
import deployment_options
import os
import requests
import waiting
from argparse import Namespace


TIMEOUT = 60 * 30
REQUEST_TIMEOUT = 2
SLEEP = 10


log = utils.get_logger('deploy_wiremock')


def main():
    deploy_options = deployment_options.load_deployment_options()
    log.info('Starting wiremock deployment')
    deploy_wiremock(deploy_options)
    log.info('Wiremock deployment completed')


def deploy_wiremock(deploy_options: Namespace):
    docs = utils.load_yaml_file_docs('deploy/wiremock/wiremock-deployment.yaml')
    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)
        
    dst_file = utils.dump_yaml_file_docs('build/wiremock-deployment.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )

    # Ensure the Wiremock pod is ready before populating the stubs.
    # To verify if Wiremock is ready to receive requests,
    # we need to obtain its hostname and port.
    log.info("Waiting for Wiremock service to be ready...")
    wait_for_wiremock_service(namespace=deploy_options.namespace)
    log.info("Wiremock service is ready")
    hostname = get_service_external_ip(namespace=deploy_options.namespace)
    port = get_service_external_port(namespace=deploy_options.namespace)

    url = f"http://{hostname}:{port}/__admin/mappings"
    log.info(f"Trying to reach Wiremock is on {url}...")
    wait_for_wiremock_pod(url=url)
    log.info(f"Wiremock is accessible")
    populate_stubs(hostname=hostname, port=port)
    log.info("Wiremock stubs populated")

def get_service_external_ip(namespace: str) -> str:
    return utils.check_output(f"kubectl get service wiremock -n {namespace} -ojson | jq -r '.status.loadBalancer.ingress[0].ip // .status.loadBalancer.ingress[0].hostname'")


def get_service_external_port(namespace: str) -> str:
    return utils.check_output(f"kubectl get service wiremock -n {namespace} -ojson | jq -r '.spec.ports[0].port'")


def deploy_ingress(hostname, deploy_options):
    src_file = os.path.join(os.getcwd(), 'deploy', "wiremock", "wiremock-ingress.yaml")
    dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, "wiremock-ingress.yaml")
    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', deploy_options.namespace)
            data = data.replace("REPLACE_HOSTNAME", hostname)
            log.info(f"Deploying {dst_file}")
            dst.write(data)

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )


def populate_stubs(hostname: str, port: str):
    cmd = f"go run ./hack/add_wiremock_stubs.go"
    os.environ["OCM_URL"] = f"{hostname}:{port}"

    log.info("Waiting for wiremock stubs population...")
    
    waiting.wait(
        lambda: utils.check_output(cmd),
        timeout_seconds=120,
        expected_exceptions=(RuntimeError),
        sleep_seconds=SLEEP, waiting_for="Stubs to be populated"
    )


def is_wiremock_service_ready(namespace: str) -> bool:
    return get_service_external_ip(namespace=namespace) != "null"


def wait_for_wiremock_service(namespace: str):
    waiting.wait(
        lambda: is_wiremock_service_ready(namespace=namespace),
        timeout_seconds=TIMEOUT,
        expected_exceptions=(requests.exceptions.ConnectionError, requests.exceptions.ReadTimeout),
        sleep_seconds=SLEEP, waiting_for="Wiremock service to be ready")


def wait_for_wiremock_pod(url: str):
    waiting.wait(
        lambda: is_wiremock_pod_ready(url=url),
        timeout_seconds=TIMEOUT,
        expected_exceptions=(requests.exceptions.ConnectionError, requests.exceptions.ReadTimeout),
        sleep_seconds=SLEEP, waiting_for="Wiremock pod to be ready")


def is_wiremock_pod_ready(url: str) -> bool:
    res = requests.get(url=url, timeout=REQUEST_TIMEOUT, verify=False)
    return res.status_code == 200


if __name__ == "__main__":
    main()
