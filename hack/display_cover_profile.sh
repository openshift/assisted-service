#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

: "${TMP_COVER_PROFILE:=/tmp/display_coverage.out}"

exclude_patterns=("mock_.*")

cp "${COVER_PROFILE}" "${TMP_COVER_PROFILE}"
for pattern in $exclude_patterns; do
    sed -i "/${pattern}/d" ${TMP_COVER_PROFILE}
done

go tool cover -html="${TMP_COVER_PROFILE}"
rm -f "${TMP_COVER_PROFILE}"
