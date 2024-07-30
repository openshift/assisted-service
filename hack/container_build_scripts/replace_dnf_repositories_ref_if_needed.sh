#!/bin/bash

rhel_version=${1}

if [ "$rhel_version" = "8" ]; then
    source ./utils.sh
    replace_dnf_repositories_ref
fi
