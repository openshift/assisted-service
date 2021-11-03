#!/usr/bin/env python3
import os

import waiting
import requests

import utils
import deployment_options
from urllib.parse import urlunsplit, urlsplit
from retry import retry

SERVICE = "assisted-service"
TIMEOUT = 60 * 30
REQUEST_TIMEOUT = 2
SLEEP = 10


def handle_arguments():
    return deployment_options.load_deployment_options()


def wait_for_request(url: str) -> bool:
    res = requests.get(url, timeout=REQUEST_TIMEOUT, verify=False)

    print(url, res.status_code)
    return res.status_code == 200


@retry(exceptions=RuntimeError, tries=2, delay=3)
def is_assisted_service_ready(target, domain, namespace, disable_tls) -> bool:
    print(f"DEBUG - is_assisted_service_ready({target} {domain} {namespace} {disable_tls})")
    service_url = utils.get_service_url(
        service=SERVICE, target=target, domain=domain,
        namespace=namespace, disable_tls=disable_tls)
    health_url = f'{service_url}/ready'

    if os.getenv("SKIPPER_PLATFORM") == 'darwin':
        url = urlsplit(health_url)

        if url.hostname == "127.0.0.1":
            url = url._replace(netloc=f"host.docker.internal:{url.port}")
            health_url = urlunsplit(url)

    print(f'Wait for {health_url}')
    return wait_for_request(health_url)


def main():
    deploy_options = handle_arguments()
    if not deploy_options.apply_manifest:
        return

    waiting.wait(
        lambda: is_assisted_service_ready(
            target=deploy_options.target,
            domain=deploy_options.domain,
            namespace=deploy_options.namespace,
            disable_tls=deploy_options.disable_tls),
        timeout_seconds=TIMEOUT,
        expected_exceptions=(requests.exceptions.ConnectionError, requests.exceptions.ReadTimeout),
        sleep_seconds=SLEEP, waiting_for="assisted-service to be healthy")


if __name__ == '__main__':
    main()
