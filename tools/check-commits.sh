#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

master_branch="origin/master"
current_branch="$(git rev-parse --abbrev-ref HEAD)"

revs=$(git rev-list "${master_branch}".."${current_branch}")

for commit in ${revs};
do
    commit_message=$(git cat-file commit ${commit} | sed '1,/^$/d')
    tmp_commit_file="$(mktemp)"
    echo "${commit_message}" > ${tmp_commit_file}
    ${__dir}/check-commit-message.sh "${tmp_commit_file}"
done


exit 0
