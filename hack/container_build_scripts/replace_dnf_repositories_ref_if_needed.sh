#!/bin/bash

tag=${1}

if [ "$tag" = "stream8" ]; then
    source ./utils.sh
    replace_dnf_repositories_ref
fi
