import utils
import deployment_options


log = utils.get_logger('deploy_wiremock')


def main():
    deploy_options = deployment_options.load_deployment_options()
    log.info('Starting wiremock deployment')
    deploy_wiremock(deploy_options)
    log.info('Wiremock deployment completed')

def deploy_wiremock(deploy_options):
    docs = utils.load_yaml_file_docs('deploy/wiremock/wiremock-deployment.yaml')
    utils.set_namespace_in_yaml_docs(docs, deploy_options.namespace)
    dst_file = utils.dump_yaml_file_docs('build/wiremock-deployment.yaml', docs)

    log.info('Deploying %s', dst_file)
    utils.apply(
        target=deploy_options.target,
        namespace=deploy_options.namespace,
        file=dst_file
    )


if __name__ == "__main__":
    main()
