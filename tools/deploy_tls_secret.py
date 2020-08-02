import argparse
import os
import textwrap

import deployment_options
import utils


def get_ca(output_dir, force_replace=False):
    ca_subject = "/CN=Assisted Installer"
    ca_expiration = 365

    ca_csr_path = os.path.join(output_dir, "ca.csr")
    ca_key_path = os.path.join(output_dir, "ca-key.pem")

    if force_replace or not os.path.exists(ca_csr_path):
        print(utils.check_output(f'openssl req -x509 -nodes -subj "{ca_subject}" -days {ca_expiration} '
                                 f'-newkey rsa:4096 -keyout "{ca_key_path}" -outform PEM -out "{ca_csr_path}"'))

    return ca_csr_path, ca_key_path


def generate_secret(output_dir, service, san, namespace, expiration=120, keep_files=False):
    ca_csr_path, ca_key_path = get_ca(output_dir)
    server_csr_path = os.path.join(output_dir, f'{service}.csr')
    server_key_path = os.path.join(output_dir, f'{service}-key.pem')

    print(utils.check_output(f'openssl req -new -newkey rsa:2048 -nodes -subj "/CN={service}" '
                             f'-keyout "{server_key_path}" -out "{server_csr_path}"'))

    server_cert_path = os.path.join(output_dir, f'{service}.crt')
    ext_file = os.path.join(output_dir, f'{service}-tls-ext.conf')
    with open(ext_file, "w") as f:
        f.write(f'subjectAltName=DNS:{san}')

    print(utils.check_output(f'openssl x509 -req -days {expiration} '
                             f'-extfile "{ext_file}" '
                             f'-CAcreateserial -CA "{ca_csr_path}" -CAkey "{ca_key_path}" '
                             f'-in "{server_csr_path}" -outform PEM -out "{server_cert_path}"'))

    secret_name = f'{service}-tls'
    print(utils.check_output(textwrap.dedent(f"""
                             cat <<EOF | kubectl apply -f -
                             apiVersion: v1
                             kind: Secret
                             metadata:
                                 name: {secret_name}
                                 namespace: {namespace}
                             type: kubernetes.io/tls
                             data:
                                 tls.crt: $(cat {server_cert_path} | base64 -w 0)
                                 tls.key: $(cat {server_key_path} | base64 -w 0)
                             EOF""")))

    if not keep_files:
        for file_name in [server_csr_path, server_cert_path, server_key_path, ext_file]:
            os.remove(file_name)

    return secret_name


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--service")
    parser.add_argument("--tls-san")
    parser.add_argument("--tls-expiration", help="Server certificate expiration (days)", type=int, default=120)
    deploy_options = deployment_options.load_deployment_options(parser)

    output_dir = os.path.join(os.getcwd(), "build")
    generate_secret(output_dir=output_dir, service=deploy_options.service, san=deploy_options.tls_san,
                    namespace=deploy_options.namespace, expiration=deploy_options.tls_expiration, keep_files=False)


if __name__ == "__main__":
    main()
