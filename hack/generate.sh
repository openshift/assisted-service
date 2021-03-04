#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(cd "$(dirname "${__dir}")" && pwd)"

function lint_swagger() {
    spectral lint swagger.yaml
}

function generate_go_server() {
    rm -rf restapi
    docker run -u $(id -u):$(id -u) -v ${__root}:${__root}:rw,Z -v /etc/passwd:/etc/passwd -w ${__root} \
        quay.io/goswagger/swagger:v0.25.0 generate server --template=stratoscale -f ${__root}/swagger.yaml \
        --template-dir=/templates/contrib
}

function generate_go_client() {
    rm -rf client models
    docker run -u $(id -u):$(id -u) -v ${__root}:${__root}:rw,Z -v /etc/passwd:/etc/passwd -w ${__root} \
        quay.io/goswagger/swagger:v0.25.0 generate client --template=stratoscale -f swagger.yaml \
        --template-dir=/templates/contrib
}

function generate_python_client() {
    local dest="${BUILD_FOLDER}"
    rm -rf "${dest}"/assisted-service-client/*

    docker run --rm -u "$(id -u)" --entrypoint /bin/sh \
        -v "${dest}":/local:Z \
        -v "${__root}"/swagger.yaml:/swagger.yaml:ro,Z \
        -v "${__root}"/tools/generate_python_client.sh:/script.sh:ro,Z \
        -e SWAGGER_FILE=/swagger.yaml -e OUTPUT=/local/assisted-service-client/ \
        swaggerapi/swagger-codegen-cli:2.4.15 /script.sh
     cd "${dest}"/assisted-service-client/ && python3 setup.py sdist --dist-dir "${dest}"
     cd "${dest}"/assisted-service-client/ && python3 "${__root}"/tools/client_package_initializer.py "${dest}"/assisted-service-client/  https://github.com/openshift/assisted-service --build
}

function generate_mocks() {
    go generate $(go list ./... | grep -v 'assisted-service/models\|assisted-service/client\|assisted-service/restapi')
}

function generate_migration() {
    go run ${__root}/tools/migration_generator/migration_generator.go -name=${MIGRATION_NAME}
}

function generate_keys() {
    cd ${__root}/tools && go run auth_keys_generator.go -keys-dir=${BUILD_FOLDER}
}

function generate_from_swagger() {
    lint_swagger
    generate_go_client
    generate_go_server
}

function generate_ocp_version() {
    OPENSHIFT_VERSIONS=$(< ${__root}/default_ocp_versions.json tr -d "\n\t ")
    PUBLIC_CONTAINER_REGISTRIES=$(< ${__root}/default_public_container_registries.txt)

    sed -i "s|value: '.*' # openshift version|value: '${OPENSHIFT_VERSIONS}' # openshift version|" ${__root}/openshift/template.yaml

    sed -i "s|OPENSHIFT_VERSIONS=.*|OPENSHIFT_VERSIONS=${OPENSHIFT_VERSIONS}|" ${__root}/onprem-environment
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ${__root}/onprem-environment

    sed -i "s|OPENSHIFT_VERSIONS=.*|OPENSHIFT_VERSIONS=${OPENSHIFT_VERSIONS}|" ${__root}/config/onprem-iso-fcc.yaml
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ${__root}/config/onprem-iso-fcc.yaml
    docker run --rm -v ${__root}/config/onprem-iso-fcc.yaml:/config.fcc:z quay.io/coreos/fcct:release --pretty --strict /config.fcc > ${__root}/config/onprem-iso-config.ign
}

# Generate manifests e.g. CRD, RBAC etc.
function generate_manifests() {
    if [ "${ENABLE_KUBE_API:-}" != "true" ]; then exit 0; fi

    local crd_options=${CRD_OPTIONS:-"crd:trivialVersions=true"}
    local controller_path=${__root}/internal/controller
    local controller_config_path=${controller_path}/config
    local controller_crd_path=${controller_config_path}/crd
    local controller_rbac_path=${controller_config_path}/rbac

    controller-gen ${crd_options} rbac:roleName=assisted-service-manager-role paths="./..." output:rbac:dir=${controller_rbac_path} \
    webhook paths="./..." output:crd:artifacts:config=${controller_crd_path}/bases
    kustomize build ${controller_crd_path} > ${BUILD_FOLDER}/resources.yaml
    controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
    goimports -w  ${controller_path}
}

function generate_all() {
    generate_from_swagger
    generate_mocks
    generate_ocp_version
    ENABLE_KUBE_API=true generate_manifests
}

function print_help() {
    echo "The available functions are:"
    compgen -A function | tr "_" "-" | grep "^generate" | awk '{print "\t" $1}'
}

declare -F $@ || (echo "Function \"$@\" unavailable." && print_help && exit 1)

if [ "$1" != "print_help" ]; then
    set -o xtrace
fi

"$@"
