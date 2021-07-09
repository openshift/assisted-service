#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(cd "$(dirname "${__dir}")" && pwd)"

function lint_swagger() {
    if ! command -v spectral &> /dev/null; then
        docker run --rm -it docker.io/stoplight/spectral:latest lint swagger.yaml
    else
        spectral lint swagger.yaml
    fi
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
        quay.io/ocpmetal/swagger-codegen-cli:2.4.15 /script.sh
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

function generate_configuration() {
    OPENSHIFT_VERSIONS=$(< ${__root}/data/default_ocp_versions.json tr -d "\n\t ")
    OPERATOR_OPENSHIFT_VERSIONS=$(< ${__root}/data/default_ocp_versions.json jq -c 'del (.["4.6", "4.7"])')
    PUBLIC_CONTAINER_REGISTRIES=$(< ${__root}/data/default_public_container_registries.txt)
    HW_VALIDATOR_REQUIREMENTS=$(< ${__root}/data/default_hw_requirements.json tr -d "\n\t ")

    sed -i "s|value: '.*' # openshift version|value: '${OPENSHIFT_VERSIONS}' # openshift version|" ${__root}/openshift/template.yaml

    sed -i "s|OPENSHIFT_VERSIONS=.*|OPENSHIFT_VERSIONS=${OPENSHIFT_VERSIONS}|" ${__root}/onprem-environment
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ${__root}/onprem-environment
    sed -i "s|HW_VALIDATOR_REQUIREMENTS=.*|HW_VALIDATOR_REQUIREMENTS=${HW_VALIDATOR_REQUIREMENTS}|" ${__root}/onprem-environment

    sed -i "s|OPENSHIFT_VERSIONS=.*|OPENSHIFT_VERSIONS=${OPENSHIFT_VERSIONS}|" ${__root}/config/onprem-iso-fcc.yaml
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ${__root}/config/onprem-iso-fcc.yaml

    sed -i "s|HW_VALIDATOR_REQUIREMENTS=.*|HW_VALIDATOR_REQUIREMENTS=${HW_VALIDATOR_REQUIREMENTS}|" ${__root}/config/onprem-iso-fcc.yaml
    docker run --rm -v ${__root}/config/onprem-iso-fcc.yaml:/config.fcc:z quay.io/coreos/fcct:release --pretty --strict /config.fcc > ${__root}/config/onprem-iso-config.ign

    # Updated operator manifests with openshift versions
    sed -i "s|value: '.*' # openshift version|value: '${OPERATOR_OPENSHIFT_VERSIONS}' # openshift version|" ${__root}/config/manager/manager.yaml
    # This python is responsible for updating the sample AgentServiceConfig to include the latest + correct osImages
    # When the CSV is built, this is included in the `almExamples` so that when a user goes through the OpenShift console
    # to create the `agent` this will give them the correct defaults.
    python3 -c '
import json
import sys
import yaml

with open("'${__root}/config/samples/agent-install.openshift.io_v1beta1_agentserviceconfig.yaml'", "r+") as f:
    doc = yaml.load(f, Loader=yaml.FullLoader)
    doc["spec"]["osImages"] = [
        {
            "openshiftVersion": k,
            "version": v["rhcos_version"],
            "url": v["rhcos_image"],
            "rootFSUrl": v["rhcos_rootfs"]
        } for (k,v) in json.loads(r"""'${OPERATOR_OPENSHIFT_VERSIONS}'""").items()
    ]
    f.seek(0)
    f.truncate()
    yaml.dump(doc, f)'
}

# Generate manifests e.g. CRD, RBAC etc.
function generate_manifests() {
    if [ "${ENABLE_KUBE_API:-}" != "true" ]; then exit 0; fi

    local crd_options=${CRD_OPTIONS:-"crd:trivialVersions=true"}
    local controller_path=${__root}/internal/controller
    local controller_config_path=${__root}/config
    local controller_crd_path=${controller_config_path}/crd
    local controller_rbac_path=${controller_config_path}/rbac

    if [ "${GENERATE_CRD:-true}" == "true" ]; then
        echo "Generating CRDs"
        controller-gen ${crd_options} rbac:roleName=assisted-service-manager-role paths="./..." output:rbac:dir=${controller_rbac_path} \
        webhook paths="./..." output:crd:artifacts:config=${controller_crd_path}/bases
        kustomize build ${controller_crd_path} > ${controller_crd_path}/resources.yaml
        controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
        goimports -w  ${controller_path}
    fi

    cp ${controller_crd_path}/resources.yaml ${BUILD_FOLDER}/resources.yaml
}

function generate_bundle() {
    ENABLE_KUBE_API=true generate_manifests
    operator-sdk generate kustomize manifests --apis-dir api -q
    kustomize build config/manifests | operator-sdk generate bundle -q --overwrite=true --output-dir ${BUNDLE_OUTPUT_DIR} ${BUNDLE_METADATA_OPTS}
    # TODO(djzager) structure config/rbac in such a way to avoid need for this
    rm ${BUNDLE_OUTPUT_DIR}/manifests/assisted-service_v1_serviceaccount.yaml
    mv ${__root}/bundle.Dockerfile ${BUNDLE_OUTPUT_DIR}/bundle.Dockerfile && sed -i '/scorecard/d' ${BUNDLE_OUTPUT_DIR}/bundle.Dockerfile

    # Reference all images by digest when asked
    if [ "${IMAGES_BY_DIGEST:-}" == "true" ]; then
        local csv="${BUNDLE_OUTPUT_DIR}/manifests/assisted-service-operator.clusterserviceversion.yaml"
        local created_at=$(date +"%Y-%m-%dT%H:%M:%SZ")
        sed -i "s|createdAt: \"\"|createdAt: ${created_at}|" $csv
        local images=$(grep '\- image:' ${csv} | awk '{ print $3 }')
        for full_image in $images; do
            local tag=${full_image#*:}
            local image=${full_image%:*}
            local registry=${image%%/*}
            local image_name=${image#*/}
            local digest=$(curl -G https://${registry}/api/v1/repository/${image_name}/tag/ | \
                jq -r --arg TAG "${tag}" '
                            .tags[]
                            | select(.name==$TAG and (has("expiration")
                            | not))
                            | .manifest_digest')
            sed -i "s,${full_image},${image}@${digest},g" ${csv}
        done
    fi
    operator-sdk bundle validate ${BUNDLE_OUTPUT_DIR}
}

function generate_all() {
    generate_from_swagger
    generate_mocks
    generate_configuration
    generate_bundle
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
