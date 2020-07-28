import utils
import argparse
import deployment_options

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--delete-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
    deploy_options = deployment_options.load_deployment_options(parser)

    print(utils.check_output(f"kubectl delete all --all -n {deploy_options.namespace} 1> /dev/null ; true"))
    if deploy_options.delete_namespace is True:
        print(utils.check_output(f"kubectl delete namespace {deploy_options.namespace} 1> /dev/null ; true"))

if __name__ == "__main__":
    main()
