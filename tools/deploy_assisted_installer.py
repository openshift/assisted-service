import os
import utils
import argparse
import yaml

parser = argparse.ArgumentParser()
parser.add_argument("--deploy-tag", help='Tag for all deployment images', type=str, default='latest')
parser.add_argument("--subsystem-test", help='deploy in subsystem mode', action='store_true')
args = parser.parse_args()

SRC_FILE = os.path.join(os.getcwd(), "deploy/bm-inventory.yaml")
DST_FILE = os.path.join(os.getcwd(), "build/bm-inventory.yaml")

TEST_CLUSTER_MONITOR_INTERVAL = "1s"
TEST_HOST_MONITOR_INTERVAL = "1s"

def main():
    with open(SRC_FILE, "r") as src:
            data = yaml.safe_load(src)
            if args.deploy_tag is not "":
                data["spec"]["template"]["spec"]["containers"][0]["image"] = "quay.io/ocpmetal/bm-inventory:{}".format(args.deploy_tag)
            else:
                data["spec"]["template"]["spec"]["containers"][0]["image"] = os.environ.get("SERVICE")

            if args.subsystem_test:
                data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'CLUSTER_MONITOR_INTERVAL', 'value': TEST_CLUSTER_MONITOR_INTERVAL})
                data["spec"]["template"]["spec"]["containers"][0]["env"].append({'name':'HOST_MONITOR_INTERVAL', 'value': TEST_HOST_MONITOR_INTERVAL})

    with open(DST_FILE, "w+") as dst:
        yaml.dump(data, dst, default_flow_style=False)
    print("Deploying {}".format(DST_FILE))

    utils.apply(DST_FILE)


if __name__ == "__main__":
    main()
