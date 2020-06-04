#!/bin/bash

# Get the image labels of a running pod.
# This could be important, for example, to get the git_revision label for the pod

function print_usage() {
    [[ -n "$1" ]] && echo "$1" && echo
    echo "usage: pod_image_labels [-p] <pod-name-filter> "
    echo
    echo "    -p - Use podman instead of docker"
    exit 1
}


DOCKER_ENGINE="docker"
while getopts ':ph' flag; do
  case "${flag}" in
    p) DOCKER_ENGINE="podman" ;;
    h) print_usage ;;
    ?) print_usage "invalid flag ${OPTARG}" ;;
  esac
done


POD_FILTER=${@:$OPTIND:1}
[[ -z "${POD_FILTER}" ]] && print_usage "pod-name-filter is missing"

result=($(kubectl get pods --all-namespaces  | grep ${POD_FILTER}))
NAMESPACE=${result[0]}
POD_NAME=${result[1]}

result=$(kubectl get pods -n ${NAMESPACE} -o=jsonpath='{.status.containerStatuses[0].imageID}' ${POD_NAME})
IMAGE=$(echo ${result} | awk -F"://" '{print $2}')


${DOCKER_ENGINE} pull ${IMAGE}
echo "image labels for pod ${POD_NAME}:"
${DOCKER_ENGINE} inspect ${IMAGE} | jq .[0].Config.Labels
