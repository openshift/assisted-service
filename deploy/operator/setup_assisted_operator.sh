__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source ${__dir}/common.sh
source ${__dir}/utils.sh
source ${__dir}/mirror_utils.sh

set -x

INDEX_IMAGE="${INDEX_IMAGE:-quay.io/edge-infrastructure/assisted-service-index:latest}"
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-assisted-service}"

INDEX_TAG="${INDEX_TAG:-latest}"

function print_help() {
  ALL_FUNCS="from_community_operators|from_index_image|print_help"
  if [ "${DISCONNECTED}" = "true" ]; then
    echo "Usage: DISCONNECTED=true LOCAL_REGISTRY=... AUTHFILE=... IRONIC_IMAGES_DIR=... MIRROR_BASE_URL=... bash ${0} (${ALL_FUNCS})"
  else
    echo "Usage: OS_IMAGES=... bash ${0} (${ALL_FUNCS})"
  fi
}

function subscription_config() {
    # Notice that this list of env variables is alphabetically ordered due to OLM bug

    if [ -n "${AGENT_IMAGE:-}" ]; then
cat <<EOF
    - name: AGENT_IMAGE
      value: '${AGENT_IMAGE}'
EOF
    fi

    if [ -n "${CONTROLLER_IMAGE:-}" ]; then
cat <<EOF
    - name: CONTROLLER_IMAGE
      value: '${CONTROLLER_IMAGE}'
EOF
    fi

    if [ -n "${DATABASE_IMAGE:-}" ]; then
cat <<EOF
    - name: DATABASE_IMAGE
      value: '${DATABASE_IMAGE}'
EOF
    fi

    if [ -n "${IMAGE_SERVICE_IMAGE:-}" ]; then
cat <<EOF
    - name: IMAGE_SERVICE_IMAGE
      value: '${IMAGE_SERVICE_IMAGE}'
EOF
    fi

    if [ -n "${INSTALLER_IMAGE:-}" ]; then
cat <<EOF
    - name: INSTALLER_IMAGE
      value: '${INSTALLER_IMAGE}'
EOF
    fi

    if [ -n "${SERVICE_IMAGE:-}" ]; then
cat <<EOF
    - name: SERVICE_IMAGE
      value: '${SERVICE_IMAGE}'
EOF
    fi
}

function configmap_config() {
    if [ -n "${OS_IMAGES:-}" ]; then
cat <<EOF
  OS_IMAGES: '${OS_IMAGES}'
EOF
    fi

    if [ -n "${SERVICE_BASE_URL:-}" ]; then
cat <<EOF
  SERVICE_BASE_URL: '${SERVICE_BASE_URL}'
EOF
    fi

    if [ -n "${PUBLIC_CONTAINER_REGISTRIES:-}" ]; then
cat <<EOF
  PUBLIC_CONTAINER_REGISTRIES: 'quay.io,${PUBLIC_CONTAINER_REGISTRIES}'
EOF
    fi
    if [ -n "${ALLOW_CONVERGED_FLOW:-}" ]; then
cat <<EOF
  ALLOW_CONVERGED_FLOW: '${ALLOW_CONVERGED_FLOW}'
EOF
    fi

}

function mirror_config() {
  if [ "${DISCONNECTED}" = "true" ]; then
cat <<EOF
 mirrorRegistryRef:
  name: 'assisted-mirror-config'
EOF
  fi
}

function install_from_catalog_source() {
  # ensure that the necessary MCE CRDs are installed.
  oc apply -f ${__root}/hack/crds/mce

  catalog_source_name="${1}"
  if [ "${ASSISTED_UPGRADE_OPERATOR}" = "true" ]; then
   catalog_source=${ASSISTED_SERVICE_OPERATOR_CATALOG}
  else
    catalog_source=${catalog_source_name}
  fi

  if [ "${DISCONNECTED}" != "true" ]; then
    # In disconnected mode it should be applied already with a different image
  tee << EOCR >(oc apply  -f -)
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${catalog_source}
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${INDEX_IMAGE}
  displayName: Assisted Test Registry
  publisher: Assisted Developer
EOCR
  fi

  tee << EOCR >(oc apply  -f -)
apiVersion: v1
kind: Namespace
metadata:
  name: ${ASSISTED_NAMESPACE}
  labels:
    name: ${ASSISTED_NAMESPACE}
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: assisted-installer-group
  namespace: ${ASSISTED_NAMESPACE}
spec:
  targetNamespaces:
    - ${ASSISTED_NAMESPACE}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: assisted-service-operator
  namespace: ${ASSISTED_NAMESPACE}
spec:
  config:
    env:
$(subscription_config)
  installPlanApproval: Automatic
  name: assisted-service-operator
  source: ${catalog_source_name}
  channel: ${CHANNEL:-ocm-5.0}
  sourceNamespace: openshift-marketplace
EOCR

  wait_for_crd "agentserviceconfigs.agent-install.openshift.io"

  tee << EOCR >(oc apply  -f -)
apiVersion: v1
kind: ConfigMap
metadata:
  name: assisted-service-config
  namespace: ${ASSISTED_NAMESPACE}
data:
  LOG_LEVEL: "debug"
  OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY: "true"
$(configmap_config)
EOCR

  if [ "${DISCONNECTED}" = "true" ]; then
    deploy_mirror_config_map
  fi

  tee << EOCR >(oc apply  -f -)
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
 name: agent
 annotations:
  unsupported.agent-install.openshift.io/assisted-service-configmap: "assisted-service-config"
spec:
 databaseStorage:
  storageClassName: ${STORAGE_CLASS_NAME}
  accessModes:
  - ReadWriteOnce
  resources:
   requests:
    storage: 8Gi
 filesystemStorage:
  storageClassName: ${STORAGE_CLASS_NAME}
  accessModes:
  - ReadWriteOnce
  resources:
   requests:
    storage: 8Gi
 imageStorage:
  storageClassName: ${STORAGE_CLASS_NAME}
  accessModes:
  - ReadWriteOnce
  resources:
   requests:
    storage: 10Gi

$(mirror_config)
EOCR

  echo "Adding osImages to AgentServiceConfig (to support a filtered list in assisted-image-service)"
  wait_for_object_amount agentserviceconfigs/agent 1 10 ${ASSISTED_NAMESPACE}

  # We need to patch agentserviceconfig to add the OS_IMAGES, but we need to rename the keys to be camelCase
  OS_IMAGES_CAMELCASE=$(echo "${OS_IMAGES}" | sed 's/openshift_version/openshiftVersion/g; s/cpu_architecture/cpuArchitecture/g' | jq -c .)
  oc patch -n ${ASSISTED_NAMESPACE} agentserviceconfig agent --type merge -p '{"spec":{"osImages":'"${OS_IMAGES_CAMELCASE}"'}}'

  wait_for_operator "assisted-service-operator" "${ASSISTED_NAMESPACE}"
  wait_for_condition "agentserviceconfigs/agent" "condition=ReconcileCompleted" "5m"
  wait_for_deployment "assisted-service" "${ASSISTED_NAMESPACE}" "5m"
  wait_for_pod "assisted-image-service" "${ASSISTED_NAMESPACE}" "app=assisted-image-service"

  echo "Enabling configuration of BMH resources outside of openshift-machine-api namespace"
  oc patch provisioning provisioning-configuration --type merge -p '{"spec":{"watchAllNamespaces": true}}'

  # Patching the Provisioning CR triggers a rollout of the metal3 deployment.
  # Since the metal3 deployment has just 1 replica, during the rollout the
  # metal admission webhook is unavailable. As a result any interaction during
  # that time with BMH resources causes an error (as the webhook is down).
  #
  # This is why before proceeding we have to wait for that process to complete.
  # This includes waiting a few seconds for the baremetal operator to notice
  # our patch (there's no simple robust way to do this. It usually happens
  # almost immediately, but we wait 10 seconds just in case). Then we wait for
  # the deployment to finish rolling out, and finally we wait for the metal3
  # pod to be ready.
  sleep 10 # Wait for the operator to notice our patch
  timeout 15m oc rollout status -n openshift-machine-api deployment/metal3
  oc wait --timeout=5m pod -n openshift-machine-api -l baremetal.openshift.io/cluster-baremetal-operator=metal3-state --for=condition=Ready

  echo "Installation of Assisted Installer operator passed successfully!"
}

function registry_config() {
  src_image=${1}
  mirrored_image=${2}
  printf '
    [[registry]]
      location = "%s"
      insecure = false
      mirror-by-digest-only = true

      [[registry.mirror]]
        location = "%s"
  ' ${src_image} ${mirrored_image}
}

function deploy_mirror_config_map() {
  # The mirror should point all the release images and not just the OpenShift release image itself.
  # An arbitrary image (cli) is chosen to retreive its pull spec, in order to mirror its repository.
  cli_image=$(podman run --quiet --rm --net=none "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" image cli)
  # Ensure the repo for the ironic agent image from the hub release is also mirrored
  ironic_agent_image=$(oc adm release info --image-for=ironic-agent "${OPENSHIFT_RELEASE_IMAGE}")

  assisted_index_image=$(get_image_without_registry $(get_image_repository_only ${INDEX_IMAGE}))

  cat << EOCR > ./assisted-mirror-config
apiVersion: v1
kind: ConfigMap
metadata:
  name: assisted-mirror-config
  namespace: ${ASSISTED_NAMESPACE}
  labels:
    app: assisted-service
data:
  registries.conf: |
    unqualified-search-registries = ["registry.access.redhat.com", "docker.io"]

    $(registry_config "$(get_image_without_tag ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})" "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})")
    $(registry_config "$(get_image_without_tag ${cli_image})" "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})")
    $(registry_config "$(get_image_without_tag ${ironic_agent_image})" "${LOCAL_REGISTRY}/$(get_image_repository_only ${OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE})")
    $(registry_configs_for_os_image_stream_sources "${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE}" "${AUTHFILE:-${PULL_SECRET_FILE}}" "${LOCAL_REGISTRY}/$(get_image_repository_only ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})" \
      "$(get_image_without_tag ${ASSISTED_OPENSHIFT_INSTALL_RELEASE_IMAGE})" \
      "$(get_image_without_tag ${cli_image})" \
      "$(get_image_without_tag ${ironic_agent_image})")
    $(
      if kubectl get crd imagedigestmirrorsets.config.openshift.io &>/dev/null; then
        for row in $(kubectl get imagedigestmirrorset -o json |
          jq -rc ".items[] | select(.metadata.name | test(\"${assisted_index_image}\")).spec.imageDigestMirrors[] | [.mirrors[0], .source]"); do
          row=$(echo ${row} | tr -d '[]"');
          source=$(echo ${row} | cut -d',' -f2);
          mirror=$(echo ${row} | cut -d',' -f1);
          registry_config ${source} ${mirror};
        done
      fi
    )
    $(
      if kubectl get crd imagecontentsourcepolicies.operator.openshift.io &>/dev/null; then
        for row in $(kubectl get imagecontentsourcepolicy -o json |
          jq -rc ".items[] | select(.metadata.name | test(\"${assisted_index_image}\")).spec.repositoryDigestMirrors[] | [.mirrors[0], .source]"); do
          row=$(echo ${row} | tr -d '[]"');
          source=$(echo ${row} | cut -d',' -f2);
          mirror=$(echo ${row} | cut -d',' -f1);
          registry_config ${source} ${mirror};
        done
      fi
    )
EOCR

  python ${__dir}/set_ca_bundle.py "${WORKING_DIR}/registry/certs/registry.2.crt" "./assisted-mirror-config"
  tee < ./assisted-mirror-config >(oc apply  -f -)
}

# Dump comprehensive registry diagnostic info when CI override mirroring fails.
# Args: $1 = destination ref (e.g. virthost:5000/olm/ci-op-xxx-pipeline)
#       $2 = source image ref (e.g. registry.build03.../ci-op-xxx/pipeline@sha256:...)
function dump_registry_debug() {
    local dest_ref="$1"
    local source_ref="$2"

    echo ""
    echo "===== REGISTRY DEBUG START ====="
    echo "Timestamp: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo "Source: ${source_ref}"
    echo "Destination: ${dest_ref}"
    echo ""

    local registry_host="${LOCAL_REGISTRY}"
    local dest_no_registry="${dest_ref#${registry_host}/}"
    local digest=""
    if [[ "${source_ref}" == *@sha256:* ]]; then
        digest="${source_ref##*@}"
    fi

    echo "--- 1. Registry Connectivity ---"
    echo "curl -sk https://${registry_host}/v2/ :"
    curl -sk "https://${registry_host}/v2/" 2>&1 || echo "(curl failed)"
    echo ""

    echo "--- 2. Registry Catalog (repos containing 'olm') ---"
    curl -sk "https://${registry_host}/v2/_catalog?n=100" 2>&1 | \
        python3 -c "import sys,json; repos=json.load(sys.stdin).get('repositories',[]); [print(r) for r in repos if 'olm' in r or 'pipeline' in r]" 2>&1 || \
        echo "(catalog query failed)"
    echo ""

    echo "--- 3. Tags in ${dest_no_registry} ---"
    curl -sk "https://${registry_host}/v2/${dest_no_registry}/tags/list" 2>&1 || \
        echo "(tags query failed — repo may not exist)"
    echo ""

    if [[ -n "${digest}" ]]; then
        echo "--- 4. Direct Manifest Fetch by Digest (${digest}) ---"

        echo "  Accept: application/vnd.docker.distribution.manifest.v2+json"
        curl -sk -o /dev/null -w "  HTTP Status: %{http_code}, Size: %{size_download}\n" \
            -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
            "https://${registry_host}/v2/${dest_no_registry}/manifests/${digest}" 2>&1

        echo "  Accept: application/vnd.oci.image.manifest.v1+json"
        curl -sk -o /dev/null -w "  HTTP Status: %{http_code}, Size: %{size_download}\n" \
            -H "Accept: application/vnd.oci.image.manifest.v1+json" \
            "https://${registry_host}/v2/${dest_no_registry}/manifests/${digest}" 2>&1

        echo "  Accept: application/vnd.docker.distribution.manifest.list.v2+json"
        curl -sk -o /dev/null -w "  HTTP Status: %{http_code}, Size: %{size_download}\n" \
            -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
            "https://${registry_host}/v2/${dest_no_registry}/manifests/${digest}" 2>&1

        echo "  Accept: */* (any)"
        curl -sk -w "\n  HTTP Status: %{http_code}, Content-Type: %{content_type}, Size: %{size_download}\n" \
            -H "Accept: */*" \
            "https://${registry_host}/v2/${dest_no_registry}/manifests/${digest}" 2>&1 | head -20
        echo ""
    fi

    echo "--- 5. Manifest by :latest Tag ---"
    curl -sk -o /dev/null -w "  HTTP Status: %{http_code}, Size: %{size_download}\n" \
        -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
        "https://${registry_host}/v2/${dest_no_registry}/manifests/latest" 2>&1
    echo ""

    if [[ -n "${digest}" ]]; then
        echo "--- 6. Blob Check (config digest from source) ---"
        curl -sk -o /dev/null -w "  Config blob HEAD: HTTP %{http_code}\n" \
            -X HEAD \
            "https://${registry_host}/v2/${dest_no_registry}/blobs/${digest}" 2>&1
        echo ""
    fi

    echo "--- 7. skopeo inspect of destination ---"
    echo "  By digest:"
    skopeo inspect --tls-verify=false --authfile="${AUTHFILE}" \
        "docker://${dest_ref}${digest:+@${digest}}" 2>&1 | head -30 || echo "  (inspect failed)"
    echo ""
    echo "  By :latest tag:"
    skopeo inspect --tls-verify=false --authfile="${AUTHFILE}" \
        "docker://${dest_ref}:latest" 2>&1 | head -30 || echo "  (inspect failed)"
    echo ""

    echo "--- 8. skopeo inspect of SOURCE ---"
    skopeo inspect --tls-verify=false --authfile="${AUTHFILE}" \
        "docker://${source_ref}" 2>&1 | head -30 || echo "  (inspect failed)"
    echo ""
    echo "  Raw manifest:"
    skopeo inspect --raw --tls-verify=false --authfile="${AUTHFILE}" \
        "docker://${source_ref}" 2>&1 | head -5 || echo "  (raw inspect failed)"
    echo ""

    echo "--- 9. IDMS/ICSP entries for pipeline ---"
    oc get imagedigestmirrorset -o yaml 2>/dev/null | grep -A3 "pipeline" || \
        oc get imagecontentsourcepolicy -o yaml 2>/dev/null | grep -A3 "pipeline" || \
        echo "  (no IDMS/ICSP found for pipeline)"
    echo ""

    echo "--- 10. Catalog-mirrored version check ---"
    echo "  Checking if catalog mirror pushed this digest to the same path..."
    if [[ -n "${digest}" ]]; then
        oc image info -a "${AUTHFILE}" --insecure=true \
            "${dest_ref}@${digest}" 2>&1 || echo "  (not found by digest)"
        echo ""
        echo "  Checking all digests at this repo via API:"
        curl -sk "https://${registry_host}/v2/${dest_no_registry}/tags/list" 2>&1
    fi
    echo ""

    echo "--- 11. Registry Pod/Container Info ---"
    podman ps --filter "name=registry" --format "{{.ID}} {{.Names}} {{.Status}} {{.Ports}}" 2>&1 || echo "  (podman ps failed)"
    podman logs --tail=20 "$(podman ps -q --filter 'name=registry' 2>/dev/null | head -1)" 2>&1 || echo "  (registry logs unavailable)"
    echo ""

    echo "===== REGISTRY DEBUG END ====="
    echo ""
}

# Mirror CI pipeline override images into the disconnected local registry via
# skopeo copy (direct registry-to-registry) and rewrite env vars to the local
# OLM IDMS pullspecs. Catalog mirror only covers OLM relatedImages (typically
# quay.io), so CI digests from registry.build*.ci.openshift.org would otherwise
# cause ImagePullBackOff. oc image mirror / podman push can appear to succeed
# here without leaving a digest-addressable manifest.
function mirror_ci_override_images() {
    echo "Mirroring CI override images to disconnected registry..."

    if [[ -z "${LOCAL_REGISTRY:-}" ]]; then
        echo "ERROR: LOCAL_REGISTRY is required to mirror CI override images"
        return 1
    fi
    if [[ -z "${AUTHFILE:-}" ]]; then
        echo "ERROR: AUTHFILE is required to mirror CI override images"
        return 1
    fi

    local override_images=(
        "${AGENT_IMAGE:-}"
        "${CONTROLLER_IMAGE:-}"
        "${INSTALLER_IMAGE:-}"
        "${IMAGE_SERVICE_IMAGE:-}"
        "${SERVICE_IMAGE:-}"
    )
    local override_names=(
        "AGENT_IMAGE"
        "CONTROLLER_IMAGE"
        "INSTALLER_IMAGE"
        "IMAGE_SERVICE_IMAGE"
        "SERVICE_IMAGE"
    )

    local i image name repo_only dest verify_ref copy_rc
    for i in "${!override_images[@]}"; do
        image="${override_images[$i]}"
        name="${override_names[$i]}"

        if [[ -z "${image}" ]]; then
            echo "  Skipping ${name}: not set"
            continue
        fi

        # Skip images already rewritten to the local registry (e.g. re-entry).
        if [[ "${image}" == "${LOCAL_REGISTRY}/"* ]]; then
            echo "  Skipping ${name}: already local"
            continue
        fi

        echo "  Mirroring ${name}: ${image}"

        # Match the IDMS path created by oc adm catalog mirror:
        # namespace/image → olm/namespace-image (slashes → dashes, prepend olm/).
        repo_only="$(get_image_repository_only "${image}")"
        if [[ -z "${repo_only}" ]]; then
            echo "ERROR: Could not parse repository for ${name}: ${image}"
            return 1
        fi
        dest="${LOCAL_REGISTRY}/olm/${repo_only//\//-}"

        echo "    Source: ${image}"
        echo "    Destination: ${dest}"

        echo "    Source manifest format:"
        skopeo inspect --raw --tls-verify=false --authfile="${AUTHFILE}" \
            "docker://${image}" 2>&1 | head -3 || echo "    (source inspect failed)"

        echo "    Copying via skopeo..."
        copy_rc=0
        skopeo copy --all \
            --src-tls-verify=false \
            --dest-tls-verify=false \
            --authfile="${AUTHFILE}" \
            "docker://${image}" \
            "docker://${dest}" || copy_rc=$?
        echo "    skopeo copy exit code: ${copy_rc} (0=success)"
        if [[ "${copy_rc}" -ne 0 ]]; then
            echo "ERROR: skopeo copy failed for ${name}: ${image} → ${dest}"
            dump_registry_debug "${dest}" "${image}"
            return 1
        fi

        # Short pause to let registry commit the manifest.
        sleep 2

        verify_ref="${dest}"
        if [[ "${image}" == *@* ]]; then
            verify_ref="${dest}@${image##*@}"
        fi

        echo "    Verifying manifest at: ${verify_ref}"
        if ! oc image info -a "${AUTHFILE}" --insecure=true \
                "${verify_ref}" > /dev/null 2>&1; then
            echo "WARNING: Digest verification failed, trying by :latest tag..."
            if ! oc image info -a "${AUTHFILE}" --insecure=true \
                    "${dest}:latest" > /dev/null 2>&1; then
                echo "ERROR: Post-mirror verification failed for ${name}"
                echo "  Neither digest (${verify_ref}) nor tag (${dest}:latest) found"
                dump_registry_debug "${dest}" "${image}"
                return 1
            fi

            echo "    Found by :latest tag but NOT by digest — manifest format mismatch?"
            echo "    Trying skopeo copy with explicit Docker V2S2 format..."
            skopeo copy \
                --format=v2s2 \
                --src-tls-verify=false \
                --dest-tls-verify=false \
                --authfile="${AUTHFILE}" \
                "docker://${image}" \
                "docker://${dest}" || {
                echo "ERROR: skopeo copy (v2s2 retry) failed for ${name}"
                dump_registry_debug "${dest}" "${image}"
                return 1
            }
            sleep 2
            if ! oc image info -a "${AUTHFILE}" --insecure=true \
                    "${verify_ref}" > /dev/null 2>&1; then
                echo "ERROR: Still not found by digest after v2s2 conversion"
                dump_registry_debug "${dest}" "${image}"
                return 1
            fi
            echo "    Verified by digest after v2s2 conversion"
        fi

        # Point subscription overrides at the local mirror so hub pods do not
        # depend on an IDMS entry for the unreachable CI registry. Keep the
        # source digest (if any) so pulls stay digest-pinned.
        if [[ "${image}" == *@* ]]; then
            export "${name}=${dest}@${image##*@}"
        else
            export "${name}=${dest}"
        fi

        echo "    ${name} mirrored and verified successfully"
        echo "    New value: ${!name}"
    done

    echo "CI override image mirroring complete."
}

function from_index_image() {
  if [ "${DISCONNECTED}" = "true" ]; then
    catalog_source_name=$ASSISTED_SERVICE_OPERATOR_CATALOG
    mirror_package "assisted-service-operator" \
        "${INDEX_IMAGE}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"
    mirror_rhcos
    mirror_ci_override_images
  else
    catalog_source_name=${ASSISTED_SERVICE_OPERATOR_CATALOG}
  fi

  install_from_catalog_source "${catalog_source_name}"
}

function from_community_operators() {
  if [ "${DISCONNECTED}" = "true" ]; then
    INDEX_IMAGE="registry.redhat.io/redhat/community-operator-index:${INDEX_TAG}"
    catalog_source_name=$ASSISTED_SERVICE_OPERATOR_CATALOG
    mirror_package "assisted-service-operator" \
        "${INDEX_IMAGE}" "${LOCAL_REGISTRY}" "${AUTHFILE}" "${catalog_source_name}"
    mirror_rhcos
  else
    catalog_source_name="community-operators"
  fi

  install_from_catalog_source "${catalog_source_name}"
}

function mirror_rhcos() {
    for i in $(seq 0 $(($(echo ${OS_IMAGES} | jq length) - 1))); do
        rhcos_image=$(echo ${OS_IMAGES} | jq -r ".[$i].url")
        mirror_rhcos_image=$(mirror_file "${rhcos_image}" "${IRONIC_IMAGES_DIR}" "${MIRROR_BASE_URL}")

        OS_IMAGES=$(echo ${OS_IMAGES} |
          jq ".[$i].url=\"${mirror_rhcos_image}\"")
    done
}

if [ -z "$@" ]; then
  if [ "${ASSISTED_UPGRADE_OPERATOR}" = "true" ]; then
    from_community_operators
  else
    from_index_image
  fi
fi

if ! declare -F "$@"; then
  print_help
  exit 1
fi

"$@"
