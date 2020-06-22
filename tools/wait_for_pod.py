import argparse
import time
import subprocess
import json
import sys

TIMEOUT = 60 * 3

parser = argparse.ArgumentParser()
parser.add_argument("--app", help='App to wait for app state', type=str)
parser.add_argument("--state", help='state to wait for', type=str)
args = parser.parse_args()


def main():
    print("waiting for pod app {} to reach {} status".format(args.app, args.state))

    start_time = time.time()
    while not is_pod_in_state():
        if time.time() - start_time > TIMEOUT:
            print("ERROR: pods app name {} did not enter {} state within {} seconds".format(args.app, args.state, TIMEOUT))
            sys.exit("pods app name {} did not enter {} state within {} seconds".format(args.app, args.state, TIMEOUT))
        time.sleep(1)

    print("pod app {} is in {} status".format(args.app, args.state))
    return

def is_pod_in_state():

        state_keys = get_pod_state()

        if not args.state in state_keys:
            return False

        # Re-check pod state after 5 sec, makes sure the pod is running and didn't fail while starting
        time.sleep  (5)
        state_keys = get_pod_state()
        if args.state in state_keys:
            return True

def get_pod_state():
        ret = subprocess.check_output("kubectl get pods -l app={app} -A -o json --namespace=assisted-installer".format(app=args.app), shell=True)
        pod_json = json.loads(ret)
        if not pod_json["items"]:
            print("ERROR: pods app name {} not found".format(args.app))
            sys.exit("pods app name {} not found".format(args.app))

        state_keys = pod_json["items"][0]["status"]["containerStatuses"][0]["state"].keys()
        return state_keys

if __name__ == "__main__":
    main()
