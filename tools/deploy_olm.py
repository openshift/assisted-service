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
        utils.wait_for_rollout(
            k8s_object='deployment',
            k8s_object_name=deployment,
            target=deploy_options.target,
            namespace='olm',
            profile=deploy_options.profile
        )


def main():
    utils.verify_build_directory(deploy_options.namespace)

    ## Main OLM Manifest for K8s
    if deploy_options.target != "oc-ingress":
        # K8s
        deployed = utils.check_if_exists(
            k8s_object='namespace',
            k8s_object_name='olm',
            target=deploy_options.target,
            namespace='olm',
            profile=deploy_options.profile
        )
        if not deployed:
            olm_manifests = [ 
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/crds.yaml",
                "https://github.com/operator-framework/operator-lifecycle-manager/releases/download/0.15.1/olm.yaml"
            ]
            for manifest_url in olm_manifests:
                dst_file = os.path.join(
                    os.getcwd(),
                    'build',
                    deploy_options.namespace,
                    os.path.basename(urlparse(manifest_url).path)
                )
                print("Deploying {}".format(dst_file))
                urlretrieve(manifest_url, dst_file)
                utils.apply(
                    target=deploy_options.target,
                    namespace='olm',
                    profile=deploy_options.profile,
                    file=dst_file
                )

            check_deployment()

    else:
        # OCP
        print("OLM Deployment not necessary")


if __name__ == "__main__":
    main()
