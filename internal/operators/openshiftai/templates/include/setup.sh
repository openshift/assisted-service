# Name of the storage class:
storage_class="ocs-storagecluster-ceph-rbd"
wait_interval="30s"

log() {
  echo "$(date --iso-8601=seconds --utc) - $@"
}

# Wait for the storage class to exist:
log "Waiting for storage class '${storage_class}' to exist"
while true; do
  if oc get storageclass "${storage_class}"; then
    log "Storage class '${storage_class}' exists now"
    break
  fi
  log "Storage class '${storage_class}' doesn't exist yet, will wait ${wait_interval} and try again"
  sleep "${wait_interval}"
done

# Set it as the default:
log "Setting storage class '${storage_class}' as the default"
oc annotate storageclass "${storage_class}" storageclass.kubernetes.io/is-default-class=true