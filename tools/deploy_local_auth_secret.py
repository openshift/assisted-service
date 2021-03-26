import os

import deployment_options
import tempfile
import utils

def main():
    secret_name = 'assisted-installer-local-auth-key'
    deploy_options = deployment_options.load_deployment_options()

    # NO OP if we don't apply manifests as we don't want the secret included in the operator bundle
    if not deploy_options.apply_manifest:
        return

    exists = utils.check_if_exists(
        "secret",
        secret_name,
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile
    )

    if exists:
        print(f'Secret {secret_name} already exists in namespace {deploy_options.namespace}')
        return

    utils.verify_build_directory(deploy_options.namespace)

    output_dir = tempfile.TemporaryDirectory()
    priv_path = os.path.join(output_dir.name, f'ec-private-key.pem')
    pub_path = os.path.join(output_dir.name, f'ec-public-key.pem')

    print(utils.check_output(f'openssl ecparam -name prime256v1 -genkey -noout -out {priv_path}'))
    print(utils.check_output(f'openssl ec -in {priv_path} -pubout -out {pub_path}'))

    kubectl = utils.get_kubectl_command(deploy_options.target, deploy_options.namespace, deploy_options.profile)
    print(utils.check_output(f'{kubectl} create secret generic {secret_name} --from-file={priv_path} --from-file={pub_path}'))


if __name__ == "__main__":
    main()
