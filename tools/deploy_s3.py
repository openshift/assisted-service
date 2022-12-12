import deployment_options
import pvc_size_utils
import utils

log = utils.get_logger("deploy_s3")


def main():
    deploy_options = deployment_options.load_deployment_options()

    if deploy_options.storage == "filesystem":
        return

    log.info("Starting object store deployment")

    utils.verify_build_directory(deploy_options.namespace)

    deploy_object_store_deployment(deploy_options)
    deploy_object_store_pvc(deploy_options)

    log.info("Completed to object store deployment")


def deploy_object_store_deployment(deploy_options):
    docs = utils.load_yaml_file_docs("deploy/s3/deployment.yaml")

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs(
        basename=f"build/{deploy_options.namespace}/object-store-deployment.yaml",
        docs=docs,
    )

    log.info("Deploying %s", dst_file)
    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )


def deploy_object_store_pvc(deploy_options):
    docs = utils.load_yaml_file_docs("deploy/s3/pvc.yaml")

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    log.info("Updating size for object storage pvc")
    pvc_size_utils.update_size_in_yaml_docs(
        target=deploy_options.target,
        ns=deploy_options.namespace,
        name="object-store",
        docs=docs,
    )

    dst_file = utils.dump_yaml_file_docs(
        basename=f"build/{deploy_options.namespace}/object-storage-pvc.yaml", docs=docs
    )

    log.info("Deploying %s", dst_file)
    utils.apply(
        target=deploy_options.target, namespace=deploy_options.namespace, file=dst_file
    )


if __name__ == "__main__":
    main()
