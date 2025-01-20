# This script enables the internal image registry because the KMM operator uses the image stream mechanism to build the
# modules, and that requires a working internal registry.

log() {
  echo "$(date --iso-8601=seconds --utc) - $@"
}

# Name of the storage class:
registry_namespace="openshift-image-registry"
bucket_class_name="ocs-storagecluster-ceph-rgw"
bucket_claim_name="registry"
wait_interval="30s"

# Wait for the storage class to exist:
log "Waiting for storage class '${bucket_class_name}' to exist"
while true; do
  if oc get storageclass "${bucket_class_name}"; then
    log "Storage class '${bucket_class_name}' exists now"
    break
  fi
  log "Storage class '${bucket_class_name}' doesn't exist yet, will wait ${wait_interval} and try again"
  sleep "${wait_interval}"
done

# Create the bucket claim:
log "Creating bucket claim '${bucket_claim_name}'"
oc apply -f - <<.
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
metadata:
  namespace: "${registry_namespace}"
  name: "${bucket_claim_name}"
spec:
  storageClassName: "${bucket_class_name}"
  generateBucketName: "${bucket_claim_name}"
.

# Wait for the bucket to be bound:
log "Waiting for bucket claim '${bucket_claim_name}' to be bound"
while true; do
  bucket_claim_phase=$(
    oc get objectbucketclaim --namespace="${registry_namespace}" "${bucket_claim_name}" \
    --output=jsonpath='{ .status.phase }'
  )
  if [ "${bucket_claim_phase}" == "Bound" ]; then
    log "Bucket claim '${bucket_claim_name}' is bound now"
    break
  fi
  log "Bucket claim '${bucket_claim_name}' isn't bound yet, will wait ${wait_interval} and try again"
  sleep "${wait_interval}"
done

# Get the details of the bucket:
bucket_name=$(
  oc get objectbucketclaim --namespace="${registry_namespace}" "${bucket_claim_name}" \
    --output=jsonpath='{ .spec.objectBucketName }'
)
bucket_id=$(
  oc get objectbucketclaim --namespace="${registry_namespace}" "${bucket_claim_name}" \
    --output=jsonpath='{ .spec.bucketName }'
)
bucket_host=$(
  oc get objectbucket --namespace="${registry_namespace}" "${bucket_name}" \
    --output=jsonpath='{ .spec.endpoint.bucketHost }'
)

# Get the S3 credentials from the secret that is automatically created with the same namespace and name than the claim:
AWS_ACCESS_KEY_ID=$(
  oc get secret --namespace="${registry_namespace}" "${bucket_claim_name}" \
    --output=jsonpath='{ .data.AWS_ACCESS_KEY_ID }' \
  | \
  base64 --decode
)
AWS_SECRET_ACCESS_KEY=$(
  oc get secret --namespace="${registry_namespace}" "${bucket_claim_name}" \
    --output=jsonpath='{ .data.AWS_SECRET_ACCESS_KEY }' \
  | \
  base64 --decode
)

# Get the CA certificate of the service signer, as the bucket service uses a certificate signed by that CA. The registry
# will be later configured to trust this CA.
service_signer_cert=$(
  oc get configmap --namespace=openshift-config openshift-service-ca.crt \
    --output=jsonpath='{ .data.service-ca\.crt }'
)

# Create the secret containing the credentials, using the keys expected by the registry:
registry_secret_name="image-registry-private-configuration-user"
oc create secret generic --namespace="${registry_namespace}" "${registry_secret_name}" \
  --from-literal=REGISTRY_STORAGE_S3_ACCESSKEY="${AWS_ACCESS_KEY_ID}" \
  --from-literal=REGISTRY_STORAGE_S3_SECRETKEY="${AWS_SECRET_ACCESS_KEY}" \
  --dry-run=client \
  --output=yaml \
| \
oc apply -f -
log "Created registry secret '${registry_secret_name}'"

# Create a configmap with the certificate of the service signer, in the format that is expected by the registry server.
# Note that the key is 'ca-bundle.crt' instead of 'service-ca.crt'.
registry_ca_configmap_name="registry-ca"
oc create configmap --namespace=openshift-config "${registry_ca_configmap_name}" \
  --from-literal=ca-bundle.crt="${service_signer_cert}" \
  --dry-run=client \
  --output=yaml \
| \
oc apply -f -
log "Created registry CA configmap '${registry_ca_configmap_name}'"

# Patch the registry configuration to use the bucket. Note that 'disableRedirect: true' is needed because otherwise
# the registry sends HTTP redirects instead of blob data, and the target server is a '...svc' address of ODF, which
# CRI-O can't use to pull images.
registry_patch='{
  "spec": {
    "managementState": "Managed",
    "disableRedirect": true,
    "replicas": 2,
    "storage": {
      "managementState": "Unmanaged",
      "s3": {
        "bucket": "'${bucket_id}'",
        "region": "us-east-1",
        "regionEndpoint": "'https://${bucket_host}'",
        "virtualHostedStyle": false,
        "encrypt": false,
        "trustedCA": {
          "name": "'${registry_ca_configmap_name}'"
        }
      }
    }
  }
}'
oc patch configs.imageregistry.operator.openshift.io cluster \
  --type=merge \
  --patch="${registry_patch}"
log "Patched registry configuration"

