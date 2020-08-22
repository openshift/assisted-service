import utils
import deployment_options
import pvc_size_utils


log = utils.get_logger('deploy_s3')


def main():
    deploy_options = deployment_options.load_deployment_options()

    log.info('Starting scality deployment')

    utils.verify_build_directory(deploy_options.namespace)

    deploy_scality(deploy_options)
    deploy_scality_storage(deploy_options)

    log.info('Completed to scality deployment')


def deploy_scality(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/s3/scality-deployment.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/scality-deployment.yaml',
        docs=docs
    )

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )


def deploy_scality_storage(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/s3/scality-storage.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    log.info('Updating pvc size for scality-pv-claim')
    pvc_size_utils.update_size_in_yaml_docs(
        target=deploy_options.target,
        ns=deploy_options.namespace,
        profile=deploy_options.profile,
        name='scality-pv-claim',
        docs=docs
    )

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/scality-storage.yaml',
        docs=docs
    )

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )


if __name__ == "__main__":
    main()
