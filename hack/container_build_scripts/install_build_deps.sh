#!/bin/bash

tag=${1}

repo=crb
if [ "$tag" = "stream8" ]; then
  repo=powertools
fi

dnf install --enablerepo=$repo -y gcc git nmstate-devel && dnf clean all
