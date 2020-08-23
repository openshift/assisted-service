import utils
import deployment_options
import pvc_size_utils


log = utils.get_logger('deploy_wiremock')


def main():
    deploy_options = deployment_options.load_deployment_options()

    log.info('Starting wiremock deployment')

    utils.set_profile(deploy_options.target, deploy_options.profile)

    deploy_wiremock(deploy_options)
    deploy_wiremock_storage(deploy_options)

    log.info('Completed to wiremock deployment')


def deploy_wiremock(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/wiremock/wiremock-deployment.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs('build/wiremock-deployment.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(dst_file)


def deploy_wiremock_storage(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/wiremock/wiremock-storage.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    log.info('Updating pvc size for wiremock-pv-claim')
    pvc_size_utils.update_size_in_yaml_docs(
        ns=deploy_options.namespace,
        name='wiremock-pv-claim',
        docs=docs
    )

    dst_file = utils.dump_yaml_file_docs('build/wiremock-storage.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(dst_file)


if __name__ == "__main__":
    main()
