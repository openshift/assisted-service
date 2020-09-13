#!/usr/bin/env bash

function validate_commit_message() {
    commit_message=${1}

    valid_commit_regex='^([A-Z]+-[0-9]+|merge|no-jira)'
    error_msg="""
Aborting commit.
Your commit message is missing either a JIRA Issue ('JIRA-1111') or 'Merge'.
You can ignore the JIRA ticket by prefixing with 'NO-JIRA'.
"""

    status=$(echo "${commit_message}" | grep -iqE "${valid_commit_regex}")

    if [ $? -gt 0 ]
    then
        echo "${commit_message}"
        echo "${error_msg}"
        exit 1
    fi
}

master_branch="master"
current_branch="$(git rev-parse --abbrev-ref HEAD)"

revs=$(git rev-list ${master_branch}..${current_branch})

for commit in ${revs};
do
    commit_message=$(git cat-file commit ${commit} | sed '1,/^$/d')
    validate_commit_message "${commit_message}"
done


exit 0
