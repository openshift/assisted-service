#!/bin/bash

if [[ -n "$(git status --porcelain onprem-environment)" ]] || [[ -n "$(git status --porcelain config)" ]]; then
	git diff -u onprem-environment
	git diff -u config
	echo "uncommitted onprem config changes. run 'make verify-latest-onprem-config' and commit any changes."
	exit 1
fi

echo "Success: no out of source tree changes found for onprem configs"