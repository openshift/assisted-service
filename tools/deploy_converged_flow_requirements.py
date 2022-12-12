import argparse

import deployment_options
import utils


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--allow-converged-flow", default=False, action="store_true")
    deploy_options = deployment_options.load_deployment_options(parser)
    log = utils.get_logger("deploy-converged-flow-requirements")

    if not deploy_options.enable_kube_api:
        log.info("kubeapi disabled - not applying converged flow manifests")
        return
    if not deploy_options.allow_converged_flow:
        log.info("kubeapi disabled - not applying converged flow manifests")
        return
    utils.deploy_from_dir(log, deploy_options, "hack/converged_flow_crds/")
    utils.deploy_from_dir(log, deploy_options, "hack/converged_flow_crs/")


if __name__ == "__main__":
    main()
