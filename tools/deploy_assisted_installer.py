import os
import utils
import argparse
import yaml
import deployment_options

parser = argparse.ArgumentParser()
parser.add_argument("--subsystem-test", help='deploy in subsystem mode',
                    action='store_true')
deploy_options = deployment_options.load_deployment_options(parser)

SRC_FILE = os.path.join(os.getcwd(), 'deploy/assisted-service.yaml')
DST_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'assisted-service.yaml')
KEY_FILE = os.path.join(os.getcwd(), 'build', deploy_options.namespace, 'auth-test-pub.json')

TEST_CLUSTER_MONITOR_INTERVAL = "1s"
TEST_HOST_MONITOR_INTERVAL = "1s"

WIREMOCK_SERVICE = "http://wiremock:8080"

def load_key():
    try:
        with open(KEY_FILE, "r") as f:
            return f.read()
    except Exception as e:
        print("Got exception {}, when tried to read key file at {}."
              "Make sure you used tools/auth_keys_generator.go before running subsystem tests".format(e, KEY_FILE))
        return ""


def main():
    utils.verify_build_directory(deploy_options.namespace)

    with open(SRC_FILE, "r") as src:
        raw_data = src.read()
        raw_data = raw_data.replace('REPLACE_NAMESPACE', f'"{deploy_options.namespace}"')
        data = yaml.safe_load(raw_data)

        image_fqdn = deployment_options.get_image_override(deploy_options, "assisted-service", "SERVICE")
        data["spec"]["replicas"] = deploy_options.replicas_count
        data["spec"]["template"]["spec"]["containers"][0]["image"] = image_fqdn
        if deploy_options.subsystem_test:
            if data["spec"]["template"]["spec"]["containers"][0].get("env", None) is None:
                data["spec"]["template"]["spec"]["containers"][0]["env"] = []
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'CLUSTER_MONITOR_INTERVAL', 'value': TEST_CLUSTER_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'HOST_MONITOR_INTERVAL', 'value': TEST_HOST_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name': 'JWKS_CERT', 'value': load_key()})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'SUBSYSTEM_RUN', 'value': 'True'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'DUMMY_IGNITION', 'value': 'True'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_BASE_URL', 'value': WIREMOCK_SERVICE})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_TOKEN_URL', 'value': WIREMOCK_SERVICE + '/token'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_SERVICE_CLIENT_ID', 'value': 'mock-ocm-client-id'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_SERVICE_CLIENT_SECRET', 'value': 'mock-ocm-client-secret'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'ENABLE_KUBE_API', 'value': str(deploy_options.enable_kube_api).lower()})

            if deploy_options.profile == utils.OPENSHIFT_CI:
                # Images built on infra cluster but needed on ephemeral cluster
                data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "IfNotPresent"
            else:
                data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Never"
        else:
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Always"
        if deploy_options.target == utils.OCP_TARGET:
            data["spec"]["replicas"] = 1 # force single replica
            spec = data["spec"]["template"]["spec"]
            service_container = spec["containers"][0]
            service_container["env"].append({'name': 'DEPLOY_TARGET', 'value': "ocp"})
            service_container["env"].append({'name': 'STORAGE', 'value': "filesystem"})
            service_container["env"].append({'name': 'ISO_WORKSPACE_BASE_DIR', 'value': '/data'})
            service_container["env"].append({'name': 'ISO_CACHE_DIR', 'value': '/data/cache'})


    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)

    if not deploy_options.apply_manifest:
        return

    print("Deploying {}".format(DST_FILE))
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=DST_FILE
    )

if __name__ == "__main__":
    main()
