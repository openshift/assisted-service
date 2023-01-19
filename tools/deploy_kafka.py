import utils
import deployment_options


log = utils.get_logger('deploy_kafka')


def main():
    deploy_options = deployment_options.load_deployment_options()

    log.info('Starting kafka deployment')

    utils.verify_build_directory(deploy_options.namespace)

    deploy_kafka(deploy_options)

    log.info('Completed kafka deployment')


def deploy_kafka(deploy_options):
    kafka_dep_file = 'deploy/kafka/kafka.yaml'
    manifest = utils.load_yaml_file_docs(kafka_dep_file)

    utils.set_namespace_in_yaml_docs(manifest, deploy_options.namespace)

    dst_file = utils.dump_yaml_file_docs(
        basename=f'build/{deploy_options.namespace}/kafka.yaml',
        docs=manifest
    )

    if not deploy_options.apply_manifest:
        return

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )


if __name__ == "__main__":
    main()
