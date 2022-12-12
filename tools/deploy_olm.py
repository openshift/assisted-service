import argparse
import os
from urllib.parse import urlparse
from urllib.request import urlretrieve

import deployment_options
import utils

deploy_options = deployment_options.load_deployment_options()


def check_deployment():
    # Checks
    print("Checking OLM deployment")
    deployments = ["olm-operator", "catalog-operator", "packageserver"]
    for deployment in deployments:
        utils.wait_for_rollout(
            k8s_object="deployment",
            k8s_object_name=deployment,
            target=deploy_options.target,
            namespace="olm",
        )


def main():
    utils.verify_build_directory(deploy_options.namespace)

    ## Main OLM Manifest for K8s
    if deploy_options.target != "oc-ingress":
        # K8s
        deployed = utils.check_if_exists(
            k8s_object="namespace",
            k8s_object_name="olm",
            target=deploy_options.target,
            namespace="olm",
        )
        if not deployed:
            olm_manifests = [
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/crds.yaml",
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/olm.yaml",
            ]
            for manifest_url in olm_manifests:
                dst_file = os.path.join(
                    os.getcwd(),
                    "build",
                    deploy_options.namespace,
                    os.path.basename(urlparse(manifest_url).path),
                )
                print(f"Deploying {dst_file}")
                urlretrieve(manifest_url, dst_file)
                utils.apply(target=deploy_options.target, namespace=None, file=dst_file)

            check_deployment()

    else:
        # OCP
        print("OLM Deployment not necessary")


if __name__ == "__main__":
    main()
