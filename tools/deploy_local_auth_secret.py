import base64
import os
import tempfile

import deployment_options
import utils


def render_file(namespace, private_key, public_key):
    src_file = os.path.join(os.getcwd(), "deploy/assisted-installer-local-auth.yaml")
    dst_file = os.path.join(
        os.getcwd(), "build", namespace, "assisted-installer-local-auth.yaml"
    )
    with open(src_file) as src:
        with open(dst_file, "w+") as dst:
            data = src.read()
            data = data.replace("REPLACE_NAMESPACE", f'"{namespace}"')
            data = data.replace("REPLACE_PRIVATE_KEY", f'"{private_key}"')
            data = data.replace("REPLACE_PUBLIC_KEY", f'"{public_key}"')
            print(f"Deploying {dst_file}")
            dst.write(data)
    return dst_file


def encoded_contents(filename):
    with open(filename) as f:
        return base64.b64encode(bytearray(f.read(), "utf-8")).decode("utf-8")


def main():
    deploy_options = deployment_options.load_deployment_options()
    utils.verify_build_directory(deploy_options.namespace)

    # Render a file without values for the operator as we don't want every deployment to have the same values
    if not deploy_options.apply_manifest:
        render_file(deploy_options.namespace, "", "")
        return

    secret_name = "assisted-installer-local-auth-key"
    exists = utils.check_if_exists(
        "secret",
        secret_name,
        target=deploy_options.target,
        namespace=deploy_options.namespace,
    )

    if exists:
        print(
            f"Secret {secret_name} already exists in namespace {deploy_options.namespace}"
        )
        return

    output_dir = tempfile.TemporaryDirectory()
    priv_path = os.path.join(output_dir.name, f"ec-private-key.pem")
    pub_path = os.path.join(output_dir.name, f"ec-public-key.pem")

    print(
        utils.check_output(
            f"openssl ecparam -name prime256v1 -genkey -noout -out {priv_path}"
        )
    )
    print(utils.check_output(f"openssl ec -in {priv_path} -pubout -out {pub_path}"))

    secret_file = render_file(
        deploy_options.namespace,
        encoded_contents(priv_path),
        encoded_contents(pub_path),
    )

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=secret_file,
    )


if __name__ == "__main__":
    main()
