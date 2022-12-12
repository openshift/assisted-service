import argparse

import deployment_options
import utils


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--delete-namespace", type=lambda x: (str(x).lower() == "true"), default=True
    )
    parser.add_argument(
        "--delete-pvc", type=lambda x: (str(x).lower() == "true"), default=False
    )
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.verify_build_directory(deploy_options.namespace)

    kubectl_cmd = utils.get_kubectl_command(target=deploy_options.target)
    print(
        utils.check_output(
            f"{kubectl_cmd} delete all --all -n {deploy_options.namespace} --force --grace-period=0 1> /dev/null ; true"
        )
    )
    # configmaps are not deleted with `delete all`
    print(
        utils.check_output(
            f"{kubectl_cmd} get configmap -o name -n {deploy_options.namespace} | "
            + f"xargs -r {kubectl_cmd} delete -n {deploy_options.namespace} --force --grace-period=0 1> /dev/null ; true"
        )
    )
    # ingress is not deleted with `delete all`
    print(
        utils.check_output(
            f"{kubectl_cmd} get ingress -o name -n {deploy_options.namespace} | "
            + f"xargs -r {kubectl_cmd} delete -n {deploy_options.namespace} --force --grace-period=0 1> /dev/null ; true"
        )
    )

    if deploy_options.delete_pvc:
        print(
            utils.check_output(
                f"{kubectl_cmd} delete pvc --all -n {deploy_options.namespace} --force --grace-period=0 1> /dev/null ; true"
            )
        )

    if deploy_options.delete_namespace is True:
        print(
            utils.check_output(
                f"{kubectl_cmd} delete namespace {deploy_options.namespace} --force --grace-period=0 1> /dev/null ; true"
            )
        )


if __name__ == "__main__":
    main()
