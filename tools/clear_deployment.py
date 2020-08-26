import utils
import argparse
import deployment_options

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--delete-namespace", type=lambda x: (str(x).lower() == 'true'), default=True)
    parser.add_argument("--delete-pvc", type=lambda x: (str(x).lower() == 'true'), default=False)
    deploy_options = deployment_options.load_deployment_options(parser)

    utils.set_profile(deploy_options.target, deploy_options.profile)

    print(utils.check_output(f"kubectl delete all --all -n {deploy_options.namespace} 1> /dev/null ; true"))
    # configmaps are not deleted with `delete all`
    print(utils.check_output(f"kubectl get configmap -o name -n {deploy_options.namespace} | " +
                             f"xargs -r kubectl delete -n {deploy_options.namespace} 1> /dev/null ; true"))
    # ingress is not deleted with `delete all`
    print(utils.check_output(f"kubectl get ingress -o name -n {deploy_options.namespace} | " +
                             f"xargs -r kubectl delete -n {deploy_options.namespace} 1> /dev/null ; true"))

    if deploy_options.delete_pvc:
        print(utils.check_output(f"kubectl delete pvc --all -n {deploy_options.namespace} 1> /dev/null ; true"))

    if deploy_options.delete_namespace is True:
        print(utils.check_output(f"kubectl delete namespace {deploy_options.namespace} 1> /dev/null ; true"))

if __name__ == "__main__":
    main()
