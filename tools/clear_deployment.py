import utils
import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--delete-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
args = parser.parse_args()


def main():
    print(utils.check_output("kubectl delete all --all -n assisted-installer 1> /dev/null ; true"))
    if args.delete_namespace is True:
        print(utils.check_output("kubectl delete namespace assisted-installer 1> /dev/null ; true"))

if __name__ == "__main__":
    main()
