#!/bin/bash

rhel_version=${1}
repo=crb

if [ "$rhel_version" = "8" ]; then
  repo=powertools
  source ./utils.sh
  replace_dnf_repositories_ref
fi

dnf install --enablerepo=$repo -y gcc git nmstate-devel openssl-devel && dnf clean all
