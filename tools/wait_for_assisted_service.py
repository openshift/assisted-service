#!/usr/bin/env python3
import os

import waiting
import requests

import utils
import deployment_options
from urllib.parse import urlparse, urlunsplit, urlsplit

SERVICE = "assisted-service"
TIMEOUT = 60 * 30
REQUEST_TIMEOUT = 2
SLEEP = 3


def handle_arguments():
    return deployment_options.load_deployment_options()


def wait_for_request(url: str) -> bool:
    res = requests.get(url, timeout=REQUEST_TIMEOUT, verify=False)

    print(url, res.status_code)
    return res.status_code == 200


def main():
    deploy_options = handle_arguments()
    if not deploy_options.apply_manifest:
        return

    service_url = utils.get_service_url(service=SERVICE, target=deploy_options.target, domain=deploy_options.domain,
                                        namespace=deploy_options.namespace, disable_tls=deploy_options.disable_tls)
    health_url = f'{service_url}/ready'

    if os.getenv("SKIPPER_PLATFORM") == 'darwin':
        url = urlsplit(health_url)

        if url.hostname == "127.0.0.1":
            url = url._replace(netloc=f"host.docker.internal:{url.port}")
            health_url = urlunsplit(url)

    print(f'Wait for {health_url}')
    try:
        waiting.wait(lambda: wait_for_request(health_url),
                     timeout_seconds=TIMEOUT,
                     expected_exceptions=(requests.exceptions.ConnectionError, requests.exceptions.ReadTimeout),
                     sleep_seconds=SLEEP, waiting_for="assisted-service to be healthy")
    except waiting.TimeoutExpired:
        utils.logs(target=deploy_options.target, namespace=deploy_options.namespace, resource='deploy/assisted-service')


if __name__ == '__main__':
    main()
