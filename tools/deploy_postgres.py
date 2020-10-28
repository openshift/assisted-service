import utils
import deployment_options
import pvc_size_utils


log = utils.get_logger('deploy_postgres')


def main():
    deploy_options = deployment_options.load_deployment_options()

    log.info('Starting postgres deployment')

    utils.verify_build_directory(deploy_options.namespace)

    deploy_postgres_secret(deploy_options)
    deploy_postgres(deploy_options)
    if deploy_options.target != utils.OCP_TARGET:
        deploy_postgres_storage(deploy_options)

    log.info('Completed postgres deployment')


def deploy_postgres_secret(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/postgres/postgres-secret.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/postgres-secret.yaml',
        docs=docs
    )

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )


def deploy_postgres(deploy_options):
    postgres_dep_file = 'deploy/postgres/postgres-deployment.yaml'
    if deploy_options.target == utils.OCP_TARGET:
        postgres_dep_file = 'deploy/postgres/postgres-deployment-ephemeral.yaml'
    docs = utils.load_yaml_file_docs(postgres_dep_file)

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/postgres-deployment.yaml',
        docs=docs
    )

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        profile=deploy_options.profile,
        file=dst_file
    )


def deploy_postgres_storage(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/postgres/postgres-storage.yaml')

    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)

    log.info('Updating pvc size for postgres-pv-claim')
    pvc_size_utils.update_size_in_yaml_docs(
        target=deploy_options.target,
        ns=deploy_options.namespace,
        profile=deploy_options.profile,
        name='postgres-pv-claim',
        docs=docs
    )

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/postgres-storage.yaml',
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
