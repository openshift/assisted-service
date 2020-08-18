import os
import utils
import argparse
import deployment_options
from urllib.request import urlretrieve
from urllib.parse import urlparse


deploy_options = deployment_options.load_deployment_options()


def check_deployment():
    # Checks
    print("Checking OLM deployment")
    deployments = ['olm-operator', 'catalog-operator', 'packageserver'] 
    for deployment in deployments:
        utils.wait_for_rollout('deployment', deployment, namespace='olm')


def main():
    utils.set_profile(deploy_options.target, deploy_options.profile)

    ## Main OLM Manifest for K8s
    if deploy_options.target != "oc-ingress":
        # K8s
        deployed = utils.check_if_exists('namespace', 'olm', namespace='olm')
        if not deployed:
            olm_manifests = [ 
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/crds.yaml",
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/olm.yaml"
            ]
            for manifest_url in olm_manifests:
                file_name = "build/{}".format(os.path.basename(urlparse(manifest_url).path))
                dst_file = os.path.join(os.getcwd(), file_name)
                print("Deploying {}".format(dst_file))
                urlretrieve(manifest_url, dst_file)
                utils.apply(dst_file)

            check_deployment()

    else:
        # OCP
        print("OLM Deployment not necessary")


if __name__ == "__main__":
    main()
