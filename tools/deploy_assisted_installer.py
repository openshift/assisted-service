import os
import utils
import argparse
import yaml
import deployment_options


SRC_FILE = os.path.join(os.getcwd(), "deploy/assisted-service.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/assisted-service.yaml")
KEY_FILE = os.path.join(os.getcwd(), "build/auth-test-pub.json")

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
    parser = argparse.ArgumentParser()
    parser.add_argument("--subsystem-test", help='deploy in subsystem mode', action='store_true')
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.set_profile(deploy_options.target, deploy_options.profile)

    with open(SRC_FILE, "r") as src:
        raw_data = src.read()
        raw_data = raw_data.replace('REPLACE_NAMESPACE', deploy_options.namespace)

        data = yaml.safe_load(raw_data)

        image_fqdn = deployment_options.get_image_override(deploy_options, "assisted-service", "SERVICE")
        data["spec"]["template"]["spec"]["containers"][0]["image"] = image_fqdn
        if deploy_options.subsystem_test:
            if data["spec"]["template"]["spec"]["containers"][0].get("env", None) is None:
                data["spec"]["template"]["spec"]["containers"][0]["env"] = []
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'CLUSTER_MONITOR_INTERVAL', 'value': TEST_CLUSTER_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'HOST_MONITOR_INTERVAL', 'value': TEST_HOST_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'JWKS_CERT', 'value': load_key()})
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Never"
        else:
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Always"

    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)
    print("Deploying {}".format(DST_FILE))

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
