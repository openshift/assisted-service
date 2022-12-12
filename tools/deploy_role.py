import os
import shutil

import deployment_options
import utils

log = utils.get_logger("deploy-role")


def main():
    deploy_options = deployment_options.load_deployment_options()

    utils.verify_build_directory(deploy_options.namespace)

    dst_dir = os.path.join(os.getcwd(), "build", deploy_options.namespace, "rbac")
    if os.path.exists(dst_dir):
        shutil.rmtree(dst_dir)
    shutil.copytree("config/rbac", dst_dir)

    if deploy_options.target == deployment_options.OCP_TARGET:
        dst_file = os.path.join(dst_dir, "ocp/kustomization.yaml")
    else:
        dst_file = os.path.join(dst_dir, "base/kustomization.yaml")

    if deploy_options.enable_kube_api:
        dst_file = os.path.join(dst_dir, "kustomization.yaml")

    with open(dst_file, "a") as dst:
        log.info(f"Deploying {dst_file}")
        dst.write("namespace: " + deploy_options.namespace + "\n")

    if deploy_options.apply_manifest:
        utils.apply_kustomize(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            file=os.path.dirname(dst_file),
        )


if __name__ == "__main__":
    main()
