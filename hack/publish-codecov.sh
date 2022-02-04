#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CI_SERVER_URL=https://prow.ci.openshift.org/view/gcs/origin-ci-test

# Configure the git refs and job link based on how the job was triggered via prow
if [[ "${JOB_TYPE}" == "presubmit" ]]; then
       echo "detected PR code coverage job for #${PULL_NUMBER}"
       REF_FLAGS="-P ${PULL_NUMBER} -C ${PULL_PULL_SHA}"
       JOB_LINK="${CI_SERVER_URL}/pr-logs/pull/${REPO_OWNER}_${REPO_NAME}/${PULL_NUMBER}/${JOB_NAME}/${BUILD_ID}"
elif [[ "${JOB_TYPE}" == "postsubmit" ]]; then
       echo "detected branch code coverage job for ${PULL_BASE_REF}"
       REF_FLAGS="-B ${PULL_BASE_REF} -C ${PULL_BASE_SHA}"
       JOB_LINK="${CI_SERVER_URL}/logs/${JOB_NAME}/${BUILD_ID}"
else
       echo "${JOB_TYPE} job not supported"
       exit 0
fi

# Configure certain internal codecov variables with values from prow.
export CI_BUILD_URL="${JOB_LINK}"
export CI_BUILD_ID="${JOB_NAME}"
export CI_JOB_ID="${BUILD_ID}"

if [[ -z "${ARTIFACT_DIR:-}" ]] || [[ ! -d "${ARTIFACT_DIR}" ]] || [[ ! -w "${ARTIFACT_DIR}" ]]; then
        echo '${ARTIFACT_DIR} must be set for non-local jobs, and must point to a writable directory' >&2
        exit 1
fi
curl -sS https://codecov.io/bash -o "${ARTIFACT_DIR}/codecov.sh"
bash <(cat "${ARTIFACT_DIR}/codecov.sh") -Z -K -f "${COVER_PROFILE}" -r "${REPO_OWNER}/${REPO_NAME}" ${REF_FLAGS}
