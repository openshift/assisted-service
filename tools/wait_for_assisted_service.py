#!/usr/bin/env python3

import waiting
import requests

import utils
import deployment_options

SERVICE = "assisted-service"
TIMEOUT = 60 * 15
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
    utils.set_profile(deploy_options.target, deploy_options.profile)

    service_url = utils.get_service_url(service=SERVICE, target=deploy_options.target, domain=deploy_options.domain,
                                        namespace=deploy_options.namespace, profile=deploy_options.profile,
                                        disable_tls=deploy_options.disable_tls)
    health_url = f'{service_url}/health'

    print(f'Wait for {health_url}')
    waiting.wait(lambda: wait_for_request(health_url),
                 timeout_seconds=TIMEOUT,
                 expected_exceptions=(requests.exceptions.ConnectionError, requests.exceptions.ReadTimeout),
                 sleep_seconds=SLEEP, waiting_for="assisted-service to be healthy")


if __name__ == '__main__':
    main()
