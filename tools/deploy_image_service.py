import os
import utils
import argparse
import yaml
import deployment_options

parser = argparse.ArgumentParser()
deploy_options = deployment_options.load_deployment_options(parser)
log = utils.get_logger('deploy-image-service')

SRC_FILE = os.path.join(os.getcwd(), 'deploy/assisted-image-service.yaml')
DST_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-image-service.yaml')

def main():
    utils.verify_build_directory(deploy_options.namespace)

    with open(SRC_FILE, "r") as src:
        raw_data = src.read()
        raw_data = raw_data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
        data = yaml.safe_load(raw_data)

    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)

    if not deploy_options.apply_manifest:
        return

    log.info(f"Deploying {DST_FILE}")
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=DST_FILE
    )

if __name__ == "__main__":
    main()
