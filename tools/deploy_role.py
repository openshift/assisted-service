import os
import shutil
import utils
import deployment_options


def main():
    deploy_options = deployment_options.load_deployment_options()

    utils.verify_build_directory(deploy_options.namespace)

    if deploy_options.target == 'ocp':
        src_file = os.path.join(os.getcwd(), 'deploy/roles/ocp_role.yaml')
        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'ocp_role.yaml')
    else:
        src_file = os.path.join(os.getcwd(), 'deploy/roles/default_role.yaml')
        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'default_role.yaml')

    with open(src_file, "r") as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
            print("Deploying {}".format(dst_file))
            dst.write(data)

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )

    if deploy_options.enable_kube_api:
        controller_roles_path = 'internal/controller/config/rbac'
        src_file = os.path.join(os.getcwd(), controller_roles_path, 'kube_api_roles.yaml')
        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'kube_api_roles.yaml')
        with open(src_file, "r") as src:
            with open(dst_file, "w+") as dst:
                data = src.read()
                data = data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
                dst.write(data)

        print("Deploying {}".format(dst_file))
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            profile=deploy_options.profile,
            file=dst_file
        )

        src_file = os.path.join(os.getcwd(), controller_roles_path, 'role.yaml')
        dst_file = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'controller_roles.yaml')
        shutil.copy(src_file, dst_file)
        print("Deploying {}".format(dst_file))
        utils.apply(
            target=deploy_options.target,
            namespace=deploy_options.namespace,
            profile=deploy_options.profile,
            file=dst_file,
        )


if __name__ == "__main__":
    main()
