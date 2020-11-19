#!/bin/bash

if [[ -n "$(git status --porcelain models restapi client mocks)" ]]; then
    git --no-pager diff --text models/ restapi/ client/ mocks/
    echo "Detected changes which require re-running generators"
    exit 1
fi

echo "All generated files are up-to-date"
