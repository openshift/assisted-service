import os
import utils
import argparse
import yaml
import deployment_options


SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory.yaml")

TEST_CLUSTER_MONITOR_INTERVAL = "1s"
TEST_HOST_MONITOR_INTERVAL = "1s"

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--subsystem-test", help='deploy in subsystem mode', action='store_true')
    deploy_options = deployment_options.load_deployment_options(parser)

    with open(SRC_FILE, "r") as src:
        data = yaml.safe_load(src)

        image_fqdn = deployment_options.get_image_override(deploy_options, "bm-inventory", "SERVICE")
        data["spec"]["template"]["spec"]["containers"][0]["image"] = image_fqdn
        if deploy_options.subsystem_test:
            if data["spec"]["template"]["spec"]["containers"][0].get("env", None) is None:
                data["spec"]["template"]["spec"]["containers"][0]["env"] = []
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'CLUSTER_MONITOR_INTERVAL', 'value': TEST_CLUSTER_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'HOST_MONITOR_INTERVAL', 'value': TEST_HOST_MONITOR_INTERVAL})
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Never"
        else:
            data["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Always"

    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)
    print("Deploying {}".format(DST_FILE))

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
