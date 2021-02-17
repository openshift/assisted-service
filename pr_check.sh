#!/bin/bash

set -e

# build app-sre image to make sure they are build without errors before merging to master
source build_images.sh
