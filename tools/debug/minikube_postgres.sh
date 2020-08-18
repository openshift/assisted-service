#!/bin/bash

# use mysql cli on a DB exposed as a service in minikube


function print_usage() {
    [[ -n "$1" ]] && echo "$1" && echo
    echo "usage: minikube_postgres.sh [-u <username>] [-p <password>] [-t <table-name>] [-m <minikube-profile] <db-service-name>"
    echo
    echo "    -u  - username"
    echo "    -p  - password"
    echo "    -t  - table"
    echo "    -m  - minikube profile"
    exit 1
}

USER="admin"
PWD="admin"
TABLE="installer"
PROFILE="minikube"

while getopts ':t:u:p:m:h:' flag; do
  case "${flag}" in
    t) TABLE=${OPTARG} ;;
    u) USER=${OPTARG} ;;
    p) PWD=${OPTARG} ;;
    m) PROFILE=${OPTARG} ;;
    h) print_usage ;;
    ?) print_usage "invalid flag ${OPTARG}" ;;
  esac
done

DB_SERVICE=${@:$OPTIND:1}
[[ -z "${DB_SERVICE}" ]] && print_usage "pod-name-filter is missing"

SERVICE_URL=$(minikube -p $PROFILE service list | grep ${DB_SERVICE} | awk -F"|" '{print $5}' | tr -d '[:space:]')
PORT=$(echo  ${SERVICE_URL}| awk -F"://|:" '{print $3}')
SERVER=$(echo  ${SERVICE_URL}| awk -F"://|:" '{print $2}')
PGPASSWORD=admin psql -U ${USER} --dbname ${TABLE} --host ${SERVER} --port ${PORT} -w
