#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__root="$(cd "$(dirname "${__dir}")" && pwd)"

UID_FLAGS=${UID_FLAGS--u $(id -u):$(id -u)}

function lint_swagger() {
    spectral lint swagger.yaml
}

function generate_go_server() {
    rm -rf restapi/
    goswagger generate server --template=stratoscale -f ${__root}/swagger.yaml
}

function generate_go_client() {
    rm -rf client/ models/*.go
    goswagger generate client --template=stratoscale -f swagger.yaml
}

function generate_python_client() {
    local dest="${BUILD_FOLDER}"
    rm -rf "${dest}"/assisted-service-client/*

    SWAGGER_FILE="${__root}"/swagger.yaml \
        OUTPUT="${dest}"/assisted-service-client/ \
        "${__root}"/tools/generate_python_client.sh
    cd "${dest}"/assisted-service-client/ && \
        python3 "${__root}"/tools/client_package_initializer.py "${dest}"/assisted-service-client/ https://github.com/openshift/assisted-service
    cp "${dest}"/assisted-service-client/dist/assisted-service-client-*.tar.gz "${dest}"
}

function generate_mocks() {
    find "${__root}" -name 'mock_*.go' -type f -delete
    go generate $(go list ./... | grep -v 'assisted-service/models\|assisted-service/client\|assisted-service/restapi')
}

function generate_migration() {
    go run ${__root}/tools/migration_generator/migration_generator.go -name=${MIGRATION_NAME}
}

function generate_keys() {
    cd ${__root}/tools && go run auth_keys_generator.go -keys-dir=${BUILD_FOLDER}
}

function remove_dashes_and_dots() {
    for f in models/*.go ; do
        sed -i 's/Dash//g;s/Dot//g' $f
    done
}

function validate_swagger_file() {
    egrep -r 'Dash|Dot' models/*.go | grep -v //  | awk '\
        {reversed=gensub("Dash","-","g", $2); \
         reversed=gensub("Dot",".","g",reversed); \
         original=gensub("\"","","g", $5);\
         if (match(original, "[.-]") == 0 || index(tolower(reversed), original) == 0)  {\
             printf("Enum value %s does not match go generated value %s. Usage of Dash or Dot in the swagger file is not supported\n", original, $2); \
             exit(-1); \
         }}'
    if [ $? != 0 ] ; then
        echo "Failed validating swagger generated files before replacing Dash (-) and Dot (.) Please see https://github.com/go-swagger/go-swagger/issues/2515"
        exit -1
    fi
}

function generate_from_swagger() {
    lint_swagger
    generate_go_client
    generate_go_server
    validate_swagger_file
    remove_dashes_and_dots
}

function generate_events() {
    rm -rf internal/common/events
    mkdir -p internal/common/events
    tools/generate_events.py ./docs/events.yaml internal/common/events/events.go
}

function generate_configuration() {
    OS_IMAGES=$(< ${__root}/data/default_os_images.json tr -d "\n\t ")
    RELEASE_IMAGES=$(< ${__root}/data/default_release_images.json tr -d "\n\t ")
    OKD_OS_IMAGES=$(< ${__root}/data/default_okd_os_images.json tr -d "\n\t ")
    OKD_RELEASE_IMAGES=$(< ${__root}/data/default_okd_release_images.json tr -d "\n\t ")
    MUST_GATHER_IMAGES=$(< ${__root}/data/default_must_gather_versions.json tr -d "\n\t ")
    OPERATOR_OS_IMAGES=$(< ${__root}/data/default_os_images.json jq -c 'del (.[] | select(.openshift_version == "4.6",.openshift_version == "4.7"))')
    PUBLIC_CONTAINER_REGISTRIES=$(< ${__root}/data/default_public_container_registries.txt)
    HW_VALIDATOR_REQUIREMENTS=$(< ${__root}/data/default_hw_requirements.json tr -d "\n\t ")

    cp "${__root}/data/default_hw_requirements.json" "${__root}/internal/controller/controllers/default_controller_hw_requirements.json"

    sed -i "s|value: '.*' # os images|value: '${OS_IMAGES}' # os images|" ${__root}/openshift/template.yaml
    sed -i "s|value: '.*' # release images|value: '${RELEASE_IMAGES}' # release images|" ${__root}/openshift/template.yaml
    sed -i "s|value: '.*' # must-gather images|value: '${MUST_GATHER_IMAGES}' # must-gather images|" ${__root}/openshift/template.yaml

    sed -i "s|OS_IMAGES:.*|OS_IMAGES: '${OS_IMAGES}'|" ${__root}/deploy/podman/configmap.yml
    sed -i "s|RELEASE_IMAGES:.*|RELEASE_IMAGES: '${RELEASE_IMAGES}'|" ${__root}/deploy/podman/configmap.yml
    sed -i "s|OS_IMAGES:.*|OS_IMAGES: '${OKD_OS_IMAGES}'|" ${__root}/deploy/podman/okd-configmap.yml
    sed -i "s|RELEASE_IMAGES:.*|RELEASE_IMAGES: '${OKD_RELEASE_IMAGES}'|" ${__root}/deploy/podman/okd-configmap.yml
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES:.*|PUBLIC_CONTAINER_REGISTRIES: '${PUBLIC_CONTAINER_REGISTRIES}'|" ${__root}/deploy/podman/{okd-,}configmap.yml
    sed -i "s|HW_VALIDATOR_REQUIREMENTS:.*|HW_VALIDATOR_REQUIREMENTS: '${HW_VALIDATOR_REQUIREMENTS}'|" ${__root}/deploy/podman/{okd-,}configmap.yml

    sed -i "s|OS_IMAGES=.*|OS_IMAGES=${OS_IMAGES}|" ${__root}/config/onprem-iso-fcc.yaml
    sed -i "s|RELEASE_IMAGES=.*|RELEASE_IMAGES=${RELEASE_IMAGES}|" ${__root}/config/onprem-iso-fcc.yaml
    sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ${__root}/config/onprem-iso-fcc.yaml

    sed -i "s|HW_VALIDATOR_REQUIREMENTS=.*|HW_VALIDATOR_REQUIREMENTS=${HW_VALIDATOR_REQUIREMENTS}|" ${__root}/config/onprem-iso-fcc.yaml
    butane --pretty --strict ${__root}/config/onprem-iso-fcc.yaml -o ${__root}/config/onprem-iso-config.ign

    # Updated operator manifests with openshift versions
    sed -i "s|value: '.*' # os images|value: '${OPERATOR_OS_IMAGES}' # os images|" ${__root}/config/manager/manager.yaml
    # This python is responsible for updating the sample AgentServiceConfig to include the latest + correct osImages
    # When the CSV is built, this is included in the `almExamples` so that when a user goes through the OpenShift console
    # to create the `agent` this will give them the correct defaults.
    python3 -c '
import json
import sys
import yaml

with open("'${__root}/config/samples/agent-install.openshift.io_v1beta1_agentserviceconfig.yaml'", "r+") as f:
    doc = yaml.safe_load(f)
    doc["spec"]["osImages"] = [
        {
            "cpuArchitecture": v["cpu_architecture"],
            "openshiftVersion": v["openshift_version"],
            "version": v["version"],
            "url": v["url"],
            "rootFSUrl": v["rootfs_url"]
        } for v in json.loads(r"""'${OPERATOR_OS_IMAGES}'""")
    ]
    f.seek(0)
    f.truncate()
    yaml.dump(doc, f)'
}

# Generate manifests e.g. CRD, RBAC etc.
function generate_manifests() (
    if [ "${ENABLE_KUBE_API:-}" != "true" ]; then exit 0; fi

    local crd_options=${CRD_OPTIONS:-"crd:trivialVersions=true"}
    local controller_path=${__root}/internal/controller
    local controller_config_path=${__root}/config
    local controller_crd_path=${controller_config_path}/crd
    local controller_rbac_path=${controller_config_path}/rbac
    local hack_boilerplate=${__root}/hack/boilerplate.go.txt

    if [ "${GENERATE_CRD:-true}" == "true" ]; then
        echo "Generating CRDs"
        cd ./api
        controller-gen ${crd_options} rbac:roleName=assisted-service-manager-role \
            paths="./..." output:rbac:dir=${controller_rbac_path} \
            webhook paths="./..." output:crd:artifacts:config=${controller_crd_path}/bases
        kustomize build ${controller_crd_path} > ${controller_crd_path}/resources.yaml
        controller-gen object:headerFile=${hack_boilerplate} paths="./..."
        goimports -w ${controller_path}
    fi

    cp ${controller_crd_path}/resources.yaml ${BUILD_FOLDER}/resources.yaml
)

function generate_bundle() {
    ENABLE_KUBE_API=true generate_manifests
    # temp copy for operator-sdk that doesn't know how to handle sub-modules
    cp PROJECT api/
    cd api
    operator-sdk generate kustomize manifests --apis-dir . --input-dir ../config/manifests --output-dir ../config/manifests -q
    rm -rf PROJECT config
    cd ..
    kustomize build config/manifests | operator-sdk generate bundle -q --overwrite=true --output-dir ${BUNDLE_OUTPUT_DIR} ${BUNDLE_METADATA_OPTS}
    mv ${__root}/bundle.Dockerfile ${BUNDLE_OUTPUT_DIR}/bundle.Dockerfile && sed -i '/scorecard/d' ${BUNDLE_OUTPUT_DIR}/bundle.Dockerfile

    operator-sdk bundle validate ${BUNDLE_OUTPUT_DIR}
}

function generate_all() {
    generate_from_swagger
    generate_events
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
