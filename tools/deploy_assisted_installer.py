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
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_BASE_URL', 'value': 'http://wiremock.assisted-installer.svc.cluster.local:8080'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_TOKEN_URL', 'value': 'http://wiremock.assisted-installer.svc.cluster.local:8080/token'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_SERVICE_CLIENT_ID', 'value': 'mock-ocm-client-id'})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'OCM_SERVICE_CLIENT_SECRET', 'value': 'mock-ocm-client-secret'})
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Never"
        else:
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Always"
        if deploy_options.target == utils.OCP_TARGET:
            spec = data["spec"]["template"]["spec"]
            service_container = spec["containers"][0]
            service_container["env"].append({'name': 'DEPLOY_TARGET', 'value': "ocp"})

            # Copy livecd from 'assisted-iso-create' image
            service_container["volumeMounts"].append({'name': 'iso', 'mountPath': "/data"})
            spec["volumes"].append({'name': 'iso', 'emptyDir': {}})
            spec["initContainers"] = [{
                "name": "assisted-iso-create",
                "image": "quay.io/ocpmetal/assisted-iso-create:latest",
                "command": ["bash", "-c"],
                "args": ["cp -r /data/* /iso-data"],
                "volumeMounts": [{"mountPath": "/iso-data", "name": "iso"}]
            }]


    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)
    print("Deploying {}".format(DST_FILE))

    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=DST_FILE
    )

if __name__ == "__main__":
    main()
