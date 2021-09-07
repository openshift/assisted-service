#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

UNRELEASED_SEMVER="99.0.0-unreleased"
PROJECT_ROOT="$(readlink -e "$(dirname "${BASH_SOURCE[0]}")"/../)"
MANIFESTS_DIR="${PROJECT_ROOT}/deploy/olm-catalog/manifests"
CSV="assisted-service-operator.clusterserviceversion.yaml"
OPERATOR_VERSION=${OPERATOR_VERSION:-}

COMMUNITY_OPERATORS_REPO="redhat-openshift-ecosystem/community-operators-prod"
COMMUNITY_OPERATORS_GIT="https://github.com/${COMMUNITY_OPERATORS_REPO}.git"
COMMUNITY_OPERATORS_FORK="https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/${GITHUB_USER}/community-operators-prod.git"

#
# Clone the community-operators repo
# Set operator-framework/community-operators as the base
# Checkout the latest and greatest
function co_clone_repo() {
    echo
    echo "## Prepping community-operators repo"
    COMMUNITY_OPERATORS_DIR=$(mktemp -d)
    git clone ${COMMUNITY_OPERATORS_FORK} ${COMMUNITY_OPERATORS_DIR}
    pushd ${COMMUNITY_OPERATORS_DIR}
    git remote add upstream ${COMMUNITY_OPERATORS_GIT}
    git fetch upstream main:upstream/main
    git checkout remotes/upstream/main
    popd
}

#
# Get the latest operator released on the preferred channel.
# Order of BUNDLE_CHANNELS matters, whichever is first in the list wins
# (ie. 'alpha' in 'alpha,ocm-2.3')
function co_get_operator_versions() {
    echo
    echo "## Getting operator versions"
    channel=${BUNDLE_CHANNELS%%,*}
    echo
    echo "Using ${channel} to determine which operator to replace"

    PREV_OPERATOR_VERSION=$(c=${channel} yq eval --exit-status \
        '.channels[] | select(.name == strenv(c)) | .currentCSV' \
        ${COMMUNITY_OPERATORS_DIR}/operators/assisted-service-operator/assisted-service.package.yaml | \
        sed -e "s/assisted-service-operator.v//")

    echo
    echo "Previous operator version: ${PREV_OPERATOR_VERSION}"

    # Set the operator version if it wasn't provided
    if [ -z ${OPERATOR_VERSION} ]; then
        echo
        echo 'Since ${OPERATOR_VERSION} is not set, we will determine new version'
        echo "by bumping minor version from previous (ie. ${PREV_OPERATOR_VERSION})"

        # First drop any build metadata
        OPERATOR_VERSION=${PREV_OPERATOR_VERSION%+*}
        # Now drop any pre-release info
        OPERATOR_VERSION=${OPERATOR_VERSION%-*}
        # Finally, bump patch release
        OPERATOR_VERSION="${OPERATOR_VERSION%.*}.$(expr ${OPERATOR_VERSION##*.} + 1)"
    fi
    echo
    echo "Operator version: ${OPERATOR_VERSION}"
}

#
# Put the operator manifests in community-operators dir
function co_update_manifests() {
    echo
    echo "## Updating operator manifests"
    pushd ${COMMUNITY_OPERATORS_DIR}
    git checkout -B ${OPERATOR_VERSION}
    cp -r ${MANIFESTS_DIR} ${COMMUNITY_OPERATORS_DIR}/operators/assisted-service-operator/${OPERATOR_VERSION}

    # Update the CSV
    CO_CSV="${COMMUNITY_OPERATORS_DIR}/operators/assisted-service-operator/${OPERATOR_VERSION}/${CSV}"

    # Grab all of the images from the relatedImages and get their digest sha
    for full_image in $(yq eval '.spec.relatedImages[] | .image' ${CO_CSV}); do
        tag=${full_image#*:}
        image=${full_image%:*}
        registry=${image%%/*}
        image_name=${image#*/}
        digest=$(curl -G https://${registry}/api/v1/repository/${image_name}/tag/\?specificTag=${tag} | \
            jq -e -r '
                .tags[]
                | select((has("expiration") | not))
                | .manifest_digest')
        # Fail if digest empty
        [[ -z ${digest} ]] && false
        sed -i "s,${full_image},${image}@${digest},g" ${CO_CSV}
    done

    # Add creation time and update the operator version references in the CSV
    local created_at=$(date +"%Y-%m-%dT%H:%M:%SZ")
    sed -i "s|createdAt: \"\"|createdAt: ${created_at}|" ${CO_CSV}
    sed -i "s/${UNRELEASED_SEMVER}/${OPERATOR_VERSION}/" ${CO_CSV}

    # Update each channel specified to make the new operator the
    # "currentCSV" on that channel
    for c in ${BUNDLE_CHANNELS//,/ }; do
        c=${c} v="assisted-service-operator.v${OPERATOR_VERSION}" \
            yq eval --exit-status --inplace \
            '(.channels[] | select(.name == strenv(c)).currentCSV) |= strenv(v)' \
            ${COMMUNITY_OPERATORS_DIR}/operators/assisted-service-operator/assisted-service.package.yaml
    done
    popd
}

function co_submit_pr() {
    echo
    echo "## Submitting PR to community-operators"
    pushd ${COMMUNITY_OPERATORS_DIR}
    # Commit
    git add --all
    git commit -s -m "assisted-service-operator: Update to ${OPERATOR_VERSION}"
    git push --set-upstream --force origin HEAD

    # Create PR
    gh pr create \
      --draft \
      --repo ${COMMUNITY_OPERATORS_REPO} \
      --base main \
      --title "$(git log -1 --format=%s)" \
      --body "$(curl -sSLo - https://raw.githubusercontent.com/redhat-openshift-ecosystem/community-operators-prod/main/docs/pull_request_template.md | \
        sed -r -n '/#+ Updates to existing Operators/,$p' | \
        sed -r -e 's#\[\ \]#[x]#g')"
    popd
}

function co_cleanup() {
    rm -rf ${COMMUNITY_OPERATORS_DIR}
}


function assisted_update_manifests() {
    version="assisted-service-operator.v${OPERATOR_VERSION}" \
        yq eval --exit-status --inplace \
        '.spec.skips += strenv(version)' \
        ${PROJECT_ROOT}/config/manifests/bases/${CSV}
    make generate-bundle
}

# Keep this, but don't run it
# function assisted_submit_pr() {
#     git add --all
#     git commit -m "assisted-service-operator: Update based on ${OPERATOR_VERSION}"
#     git push --set-upstream --force origin HEAD

#     # Create PR
#     gh pr create \
#       --draft \
#       --title "$(git log -1 --format=%s)" \
#       --body "Update to operator manifests"
# }

co_clone_repo
co_get_operator_versions
co_update_manifests
co_submit_pr
co_cleanup
assisted_update_manifests
