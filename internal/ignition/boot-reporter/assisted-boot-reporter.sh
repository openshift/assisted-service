#!/usr/bin/env bash

export LOG_SEND_FREQUENCY_IN_MINUTES=5
export SERVICE_TIMEOUT_MINUTES=60

function log() {
  echo "$(date '+%F %T') ${HOSTNAME} $2[$$]: level=$1 msg=\"$3\""
}

function log_info() {
  log "info" "$1" "$2"
}

function log_error() {
  log "error" "$1" "$2"
}

function init_variables() {
  func_name=${FUNCNAME[0]}
  init_failed="false"

  if [ "$LOG_SEND_FREQUENCY_IN_MINUTES" == "" ]; then
    init_failed="true"
    log_error "${func_name}" "LOG_SEND_FREQUENCY_IN_MINUTES is empty."
  fi

  if [ "$SERVICE_TIMEOUT_MINUTES" == "" ]; then
    init_failed="true"
    log_error "${func_name}" "SERVICE_TIMEOUT_MINUTES is empty."
  fi

  if [ "$ASSISTED_SERVICE_URL" == "" ]; then
    init_failed="true"
    log_error "${func_name}" "ASSISTED_SERVICE_URL is empty."
  elif [ "${ASSISTED_SERVICE_URL: -1}" == "/" ]; then
    export ASSISTED_SERVICE_URL="${ASSISTED_SERVICE_URL::-1}"
  fi

  if [ "$CLUSTER_ID" == "" ]; then
    init_failed="true"
    log_error "${func_name}" "CLUSTER_ID is empty."
  fi

  if [ "$HOST_ID" == "" ]; then
    init_failed="true"
    log_error "${func_name}" "HOST_ID is empty."
  fi

  if [ "$init_failed" == "true" ]; then
    log_error "${func_name}" "Failed to initialize variables. Exiting."
    exit 1
  fi
}

function collect_and_upload_logs() {
  func_name=${FUNCNAME[0]}

  log_info "${func_name}" "Collecting logs."
  logs_dir_name=boot_logs_$HOST_ID
  logs_path=/tmp/$logs_dir_name

  rm -rf "$logs_path"
  mkdir -p "$logs_path"

  log_info "${func_name}" "Copying journalctl to $logs_path/"
  journalctl > "$logs_path"/journalctl.log
  log_info "${func_name}" "Capturing the output of 'ip a' to $logs_path/"
  ip a > "$logs_path"/ip_a.log
  log_info "${func_name}" "Copying /etc/resolv.conf to $logs_path/"
  cp /etc/resolv.conf "$logs_path"
  log_info "${func_name}" "Copying /var/log/pods to $logs_path/"
  cp -r /var/log/pods "$logs_path"
  log_info "${func_name}" "Compressing logs to $logs_path.tar.gz"
  tar -czvf "$logs_path".tar.gz -C /tmp "$logs_dir_name"

  log_info "${func_name}" "Uploading logs."

  if curl -X POST  -H "X-Secret-Key: ${PULL_SECRET_TOKEN}" \
       -F upfile=@"$logs_path".tar.gz \
       "$ASSISTED_SERVICE_URL/api/assisted-install/v2/clusters/$CLUSTER_ID/logs?logs_type=node-boot&host_id=$HOST_ID" ; then
      log_info "${func_name}" "Successfully uploaded logs."
  else
      log_error "${func_name}" "Failed to upload logs."
  fi
}

function main() {
  func_name=${FUNCNAME[0]}
  count=$((SERVICE_TIMEOUT_MINUTES/LOG_SEND_FREQUENCY_IN_MINUTES))

  for i in $(seq $count)
  do
      log_info "${func_name}" "Upload logs attempt ${i}/${count}"
      collect_and_upload_logs
      if [ "$i" != "$count" ]; then # don't sleep at the last iteration.
        log_info "${func_name}" "Sleeping for ${LOG_SEND_FREQUENCY_IN_MINUTES} minutes until the next attempt."
        sleep $((LOG_SEND_FREQUENCY_IN_MINUTES*60))
      fi
  done
}

log_info assisted-boot-reporter "assisted-boot-reporter start"
init_variables
main
log_info assisted-boot-reporter "assisted-boot-reporter end"
exit 0
