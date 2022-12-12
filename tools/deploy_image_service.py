import argparse
import os
import socket
from urllib.parse import urlparse

import deployment_options
import utils
import yaml

parser = argparse.ArgumentParser()
deploy_options = deployment_options.load_deployment_options(parser)
log = utils.get_logger("deploy-image-service")

SRC_FILE = os.path.join(os.getcwd(), "deploy/assisted-image-service.yaml")
DST_FILE = os.path.join(
    os.getcwd(), "build", deploy_options.namespace, "assisted-image-service.yaml"
)

SERVICE_SRC_FILE = os.path.join(
    os.getcwd(), "deploy/assisted-image-service-service.yaml"
)
SERVICE_DST_FILE = os.path.join(
    os.getcwd(),
    "build",
    deploy_options.namespace,
    "assisted-image-service-service.yaml",
)


def main():
    utils.verify_build_directory(deploy_options.namespace)

    # Handle assisted-image-service `service`
    with open(SERVICE_SRC_FILE) as src:
        with open(SERVICE_DST_FILE, "w+") as dst:
            raw_data = src.read()
            raw_data = raw_data.replace(
                "REPLACE_NAMESPACE", f'"{deploy_options.namespace}"'
            )
            data = yaml.safe_load(raw_data)

            print(f"Deploying {SERVICE_DST_FILE}")
            dst.write(yaml.dump(data))

    if deploy_options.apply_manifest:
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=SERVICE_DST_FILE,
        )

    # Handle assisted-image-service `deployment`
    with open(SRC_FILE) as src:
        log.info(f"Loading source template file for assisted-image-service: {SRC_FILE}")
        raw_data = src.read()
        raw_data = raw_data.replace(
            "REPLACE_NAMESPACE", f'"{deploy_options.namespace}"'
        )
        raw_data = raw_data.replace(
            "REPLACE_IMAGE_SERVICE_IMAGE", os.environ.get("IMAGE_SERVICE")
        )

        # Getting IMAGE_SERVICE_BASE_URL variable from environment which should be specified
        # as part of service deployment flow in assisted-test-infra (sets custom hostname and port).
        # Otherwise, fetching the base url from the deployed image-service `service`.
        if deploy_options.target == "kind":
            image_service_base_url = f"http://{socket.gethostname()}"
        else:
            image_service_base_url = os.environ.get(
                "IMAGE_SERVICE_BASE_URL",
                utils.get_service_url(
                    service="assisted-image-service",
                    target=deploy_options.target,
                    domain=deploy_options.domain,
                    namespace=deploy_options.namespace,
                    disable_tls=deploy_options.disable_tls,
                    check_connection=True,
                ),
            )

        raw_data = raw_data.replace(
            "REPLACE_IMAGE_SERVICE_BASE_URL", image_service_base_url
        )

        data = yaml.safe_load(raw_data)
        log.info(data)

    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)

    if not deploy_options.apply_manifest:
        return

    log.info(f"Deploying {DST_FILE}")
    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=DST_FILE
    )


if __name__ == "__main__":
    main()
