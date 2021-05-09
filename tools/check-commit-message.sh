#!/usr/bin/env bash

set -o nounset

commit_file=${1}
commit_message="$(cat ${commit_file})"
valid_commit_regex='([A-Z]+-[0-9]+|#[0-9]+|merge|no-issue|revert)'

error_msg="""Aborting commit.
Your commit message is missing either a JIRA issue ('JIRA-1111'), a GitHub issue ('#39').
You can also ignore the ticket checking with 'NO-ISSUE'.

Your message is preserved at '${commit_file}'
"""

status=$(echo "${commit_message}" | grep -iqE "${valid_commit_regex}")

if [ $? -gt 0 ]
then
    echo "${error_msg}"
    exit 1
fi
