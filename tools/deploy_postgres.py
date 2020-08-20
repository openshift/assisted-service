import utils
import deployment_options
import pvc_size_utils


log = utils.get_logger('deploy_postgres')


def main():
    deploy_options = deployment_options.load_deployment_options()

    log.info('Starting postgres deployment')

    utils.set_profile(deploy_options.target, deploy_options.profile)

    deploy_postgres_secret(deploy_options)
    deploy_postgres(deploy_options)
    deploy_postgres_storage(deploy_options)

    log.info('Completed postgres deployment')


def deploy_postgres_secret(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/postgres/postgres-secret.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs('build/postgres-secret.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(dst_file)


def deploy_postgres(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/postgres/postgres-deployment.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs('build/postgres-deployment.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(dst_file)


def deploy_postgres_storage(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/postgres/postgres-storage.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    log.info('Updating pvc size for postgres-pv-claim')
    pvc_size_utils.update_size_in_yaml_docs(
        ns=deploy_options.namespace,
        name='postgres-pv-claim',
        docs=docs
    )

    dst_file = utils.dump_yaml_file_docs('build/postgres-storage.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(dst_file)


if __name__ == "__main__":
    main()
