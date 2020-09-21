#!/usr/bin/env bash

set -o nounset

commit_file=${1}
commit_message="$(cat ${commit_file})"
valid_commit_regex='^([A-Z]+-[0-9]+|merge|no-jira)'

error_msg="""Aborting commit with message: '${commit_message}'
Your commit message is missing either a JIRA Issue ('JIRA-1111') or 'Merge'.
You can ignore the JIRA ticket by prefixing with 'NO-JIRA'.
"""

status=$(echo "${commit_message}" | grep -iqE "${valid_commit_regex}")

if [ $? -gt 0 ]
then
    echo "${error_msg}"
    exit 1
fi
