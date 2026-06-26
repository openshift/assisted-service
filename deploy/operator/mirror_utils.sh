function mirror_package() {
  # Here we will do the next actions:
  # 1. Create an index of specific packages from specific remote indexes
  # 2. Push the index image to the local index
  # 3. Upload all packages to the local index and create ICSP and
  #    CatalogSource for the new created index

  # e.g. "local-storage-operator"
  package="${1}"

  # e.g. "registry.redhat.io/redhat/redhat-operator-index:v4.8"
  remote_index="${2}"

  # e.g. "virthost.ostest.test.metalkube.org:5000"
  local_registry="${3}"

  # e.g. "/run/user/0/containers/auth.json", "~/.docker/config.json"
  # should have authentication information for both official registry
  # (pull-secret) and for the local registry
  authfile="${4}"

  catalog_source_name="${5}"

  # If the remote index is referenced using name and tag, use "name:tag" for the local image.
  # If the remote index is referenced using a digest, use "name:digest" for the local image.
  local_index_name=${remote_index##*/}
  local_index_name="${local_index_name/@*:/:}"

  local_registry_index_tag="${local_registry}/olm-index/${local_index_name}"
  local_registry_image_tag="${local_registry}/olm"

  opm index prune \
        --from-index "${remote_index}" \
        --packages "${package}" \
        --tag "${local_registry_index_tag}"

  GODEBUG=x509ignoreCN=0 podman push \
        --tls-verify=false \
        "${local_registry_index_tag}" \
        --authfile "${authfile}"

  manifests_dir=$(mktemp -d -t manifests-XXXXXXXXXX)
  GODEBUG=x509ignoreCN=0 oc adm catalog mirror \
        "${local_registry_index_tag}" \
        "${local_registry_image_tag}" \
        --registry-config="${authfile}" \
        --to-manifests="${manifests_dir}"

  # Starting with version 4.14 of OpenShift the ImageContentSourcePolicy object is deprecated, and
  # replaced by ImageDigestMirrorSet. The 'oc adm catalog mirror' command generates both, so we
  # check and use the new one if possible.
  if [ -f "${manifests_dir}/imageDigestMirrorSet.yaml" ]; then
    mirrors_config_file="${manifests_dir}/imageDigestMirrorSet.yaml"
  else
    mirrors_config_file="${manifests_dir}/imageContentSourcePolicy.yaml"
  fi
  echo "Applied mirrors configuration:"
  cat "${mirrors_config_file}"
  oc apply -f "${mirrors_config_file}"

  # Modify openshift-marketplace namespace in order to allow workaround the new pod security
  # admissions. Details are described in https://access.redhat.com/articles/6977554 and they
  # are used to allow `securityContextConfig: legacy` stanza in the CatalogSource definition.
  cat > "${manifests_dir}/namespaceHotfix.yaml" << EOF
apiVersion: v1
kind: Namespace
metadata:
  labels:
    security.openshift.io/scc.podSecurityLabelSync: "false"
    openshift.io/cluster-monitoring: "true"
    pod-security.kubernetes.io/enforce: baseline
  name: openshift-marketplace
EOF

  echo "Applied hotfix for marketplace namespace:"
  cat "${manifests_dir}/namespaceHotfix.yaml"

  oc apply -f "${manifests_dir}/namespaceHotfix.yaml"

  cat > "${manifests_dir}/catalogSource.yaml" << EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${catalog_source_name}
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${local_registry_index_tag}
  displayName: Mirror index for package ${package} from ${remote_index}
  publisher: Local
  grpcPodConfig:
    securityContextConfig: legacy
  updateStrategy:
    registryPoll:
      interval: 30m
EOF

  echo "Applied catalog source:"
  cat "${manifests_dir}/catalogSource.yaml"

  oc apply -f "${manifests_dir}/catalogSource.yaml"


  if [ "${OPENSHIFT_CI:-false}" = "false" ]; then
    # Until allowing mirroring by tags https://issues.redhat.com/browse/OCPNODE-521
    # https://github.com/openshift/api/pull/874 will be part of OCP 4.9
    # The sixth element on net-dhcp-leases is the hostname.
    # We would like to get all the available hostnames, SSH to them, and fix their registries.conf to allow mirror-by-digest.
    hostnames=$(virsh net-dhcp-leases ${BAREMETAL_NETWORK_NAME} | awk 'NR > 2 && $6 != "-" {print $6}')

    for hostname in ${hostnames}; do
        ssh -o StrictHostKeyChecking=no core@${hostname} bash - << EOF
  if grep "mirror-by-digest-only = true" /etc/containers/registries.conf -c; then
      sudo sed -i 's/mirror-by-digest-only = true/mirror-by-digest-only = false/' /etc/containers/registries.conf
      sudo systemctl restart crio kubelet
  fi
EOF
    done
  fi
}

function mirror_file() {
  remote_url="${1}"
  httpd_path="${2}"
  base_mirror_url="${3}"

  local url_path="$(echo ${remote_url} | cut -d / -f 4-)"
  mkdir -p "$(dirname ${httpd_path}/${url_path})"
  curl -L --retry 5 --connect-timeout 30 "${remote_url}" -o "${httpd_path}/${url_path}"

  echo "${base_mirror_url}/${url_path}"
}

function disable_default_indexes() {
  oc patch OperatorHub cluster --type json \
        -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'
}

function merge_authfiles() {
  first_authfile="${1}"
  second_authfile="${2}"
  merged_authfile="${3}"

  jq -s '.[0] * .[1]' "${first_authfile}" "${second_authfile}" > "${merged_authfile}"
}

function install_opm() {
  curl -L --retry 5 --connect-timeout 30 -s https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-4.7/opm-linux.tar.gz | tar xvz -C /usr/local/bin/
}

function ocp_mirror_release() {
  pull_secret_file="${1}"
  source_image="${2}"
  dest_mirror_repo="${3}"
  local max_attempts="${OCP_MIRROR_RELEASE_RETRIES:-3}"
  local retry_delay="${OCP_MIRROR_RELEASE_RETRY_DELAY:-30}"
  local attempt=1
  local output=""

  while [ "${attempt}" -le "${max_attempts}" ]; do
    if output=$(oc adm -a "${pull_secret_file}" release mirror \
               --from="${source_image}" \
               --to="${dest_mirror_repo}" 2>&1); then
      echo "${output}"
      return 0
    fi

    echo "${output}"

    if [ "${attempt}" -ge "${max_attempts}" ] || ! transient_registry_error "${output}"; then
      return 1
    fi

    echo "Release mirror failed with a transient registry error (attempt ${attempt}/${max_attempts}), retrying in ${retry_delay}s..." >&2
    sleep "${retry_delay}"
    attempt=$((attempt + 1))
  done
}

function transient_registry_error() {
  echo "${1}" | grep -Eqi 'unexpected EOF|504 Gateway|502 Bad Gateway|503 Service Unavailable|connection reset|TLS handshake timeout|broken pipe|i/o timeout|use of closed network connection'
}

function mirror_image_with_retry() {
  authfile="${1}"
  source_image="${2}"
  dest_repo="${3}"
  local max_attempts="${4:-3}"
  local retry_delay="${5:-15}"
  local attempt=1
  local output=""

  while [ "${attempt}" -le "${max_attempts}" ]; do
    if output=$(oc image mirror -a "${authfile}" "${source_image}" "${dest_repo}" 2>&1); then
      echo "${output}"
      return 0
    fi

    echo "${output}"

    if [ "${attempt}" -ge "${max_attempts}" ] || ! transient_registry_error "${output}"; then
      return 1
    fi

    echo "Image mirror failed with a transient registry error (attempt ${attempt}/${max_attempts}), retrying in ${retry_delay}s..." >&2
    sleep "${retry_delay}"
    attempt=$((attempt + 1))
  done
}

function image_repo_from_pullspec() {
  echo "${1%%@*}"
}

function mirror_repo_from_source() {
  release_mirror_repo="${1}"
  source_repo="${2}"
  echo "${release_mirror_repo}/${source_repo#quay.io/}"
}

function release_payload_art_dev_pullspecs() {
  release_image="${1}"
  authfile="${2}"

  oc adm -a "${authfile}" release info "${release_image}" --images 2>/dev/null | \
    awk '{print $2}' | grep -E '^quay.io/openshift-release-dev/ocp-v[0-9]+\.[0-9]+-art-dev@sha256:' || true
}

function discover_os_image_stream_images_from_release_json() {
  release_image="${1}"
  authfile="${2}"
  local payload_specs payload_file payload_count

  payload_specs=$(release_payload_art_dev_pullspecs "${release_image}" "${authfile}")
  payload_count=$(printf '%s\n' "${payload_specs}" | sed '/^$/d' | wc -l)

  if [ "${payload_count}" -gt 0 ]; then
    echo "Release metadata scan: excluding ${payload_count} exact release payload art-dev digests already covered by oc adm release mirror" >&2
    payload_file=$(mktemp)
    printf '%s\n' "${payload_specs}" | jq -R . | jq -s . > "${payload_file}"
    oc adm -a "${authfile}" release info "${release_image}" -o json | \
      jq -r --slurpfile payload "${payload_file}" \
        '[.. | strings | select(test("^quay.io/openshift-release-dev/ocp-v[0-9]+\\.[0-9]+-art-dev@sha256:"))] | unique | map(select(. as $i | ($payload[0] | index($i)) | not)) | .[]'
    rm -f "${payload_file}"
  else
    echo "Release metadata scan: could not determine release payload art-dev digests, scanning all art-dev digests" >&2
    oc adm -a "${authfile}" release info "${release_image}" -o json | \
      jq -r '[.. | strings | select(test("^quay.io/openshift-release-dev/ocp-v[0-9]+\\.[0-9]+-art-dev@sha256:"))] | unique | .[]'
  fi
}

function discover_os_image_stream_images_from_mco_tool() {
  release_image="${1}"
  authfile="${2}"
  local mco_image osimagestream_json authfile_dir authfile_name mco_stderr

  if ! mco_image=$(oc adm -a "${authfile}" release info "${release_image}" --image-for machine-config-operator); then
    echo "machine-config-osimagestream discovery failed: could not resolve machine-config-operator image" >&2
    return 2
  fi

  if ! podman run --quiet --rm --net=none --authfile "${authfile}" "${mco_image}" test -x /usr/bin/machine-config-osimagestream 2>/dev/null; then
    return 1
  fi

  authfile_dir=$(dirname "${authfile}")
  authfile_name=$(basename "${authfile}")
  mco_stderr=$(mktemp)

  if ! osimagestream_json=$(podman run --quiet --rm --net=host \
        --authfile "${authfile}" \
        -v "${authfile_dir}:/authfile:ro,Z" \
        "${mco_image}" \
        /usr/bin/machine-config-osimagestream get osimagestream \
          --release-image "${release_image}" \
          --authfile "/authfile/${authfile_name}" \
          --output-format json 2>"${mco_stderr}"); then
    echo "machine-config-osimagestream discovery failed:" >&2
    cat "${mco_stderr}" >&2
    rm -f "${mco_stderr}"
    return 2
  fi
  rm -f "${mco_stderr}"

  echo "${osimagestream_json}" | jq -r '
    .status.availableStreams[]? |
    (.osImage, .osExtensionsImage) |
    select(. != null and . != "")
  '
}

# Discover the container images that node-image-pull needs by asking the same
# machine-config-osimagestream helper the bootstrap installer uses. Fall back to
# scanning release metadata only when the helper is unavailable in older payloads.
function discover_os_image_stream_images() {
  release_image="${1}"
  authfile="${2}"
  local images mco_status=0

  images=$(discover_os_image_stream_images_from_mco_tool "${release_image}" "${authfile}") || mco_status=$?

  if [ "${mco_status}" -eq 0 ] && [ -n "${images}" ]; then
    echo "Discovered $(printf '%s\n' "${images}" | sed '/^$/d' | wc -l) OSImageStream image(s) via machine-config-osimagestream" >&2
    echo "${images}"
    return 0
  fi

  if [ "${mco_status}" -eq 2 ]; then
    return 1
  fi

  echo "machine-config-osimagestream unavailable; falling back to release metadata scan" >&2
  images=$(discover_os_image_stream_images_from_release_json "${release_image}" "${authfile}")
  echo "Discovered $(printf '%s\n' "${images}" | sed '/^$/d' | wc -l) OSImageStream image(s) via release metadata scan" >&2
  echo "${images}"
}

function discover_os_image_stream_sources() {
  release_image="${1}"
  authfile="${2}"

  discover_os_image_stream_images "${release_image}" "${authfile}" | \
    while IFS= read -r image; do
      [ -n "${image}" ] || continue
      image_repo_from_pullspec "${image}"
    done | sort -u
}

function idms_covers_source() {
  source="${1}"
  oc get imagedigestmirrorset -o json 2>/dev/null | \
    jq -e --arg src "${source}" \
      '.items[].spec.imageDigestMirrors[] | select(.source == $src) | .mirrors | length > 0' >/dev/null
}

function idms_covers_source_except_managed() {
  source="${1}"
  oc get imagedigestmirrorset -o json 2>/dev/null | \
    jq -e --arg src "${source}" \
      '.items[] | select(.metadata.name != "assisted-osimagestream-mirror") | .spec.imageDigestMirrors[] | select(.source == $src) | .mirrors | length > 0' >/dev/null
}

function icsp_covers_source() {
  source="${1}"
  oc get imagecontentsourcepolicy -o json 2>/dev/null | \
    jq -e --arg src "${source}" \
      '.items[].spec.repositoryDigestMirrors[]? | select(.source == $src) | .mirrors | length > 0' >/dev/null
}

function cluster_mirror_covers_source() {
  source="${1}"
  idms_covers_source "${source}" || icsp_covers_source "${source}"
}

function cluster_mirror_covers_source_except_managed() {
  source="${1}"
  idms_covers_source_except_managed "${source}" || icsp_covers_source "${source}"
}

function array_contains() {
  local needle="$1"
  shift
  local item

  for item in "$@"; do
    [ "${item}" = "${needle}" ] && return 0
  done
  return 1
}

function managed_osimagestream_idms_sources() {
  oc get imagedigestmirrorset assisted-osimagestream-mirror -o json 2>/dev/null | \
    jq -r '.spec.imageDigestMirrors[]?.source // empty'
}

# IDMS/ICSP only map a source repo to a mirror; they do not prove a digest was copied.
# Before skipping oc image mirror, verify the digest is present in the local mirror repo.
function local_mirror_has_digest() {
  pull_secret_file="${1}"
  mirror_repo="${2}"
  digest="${3}"

  oc image info -a "${pull_secret_file}" "${mirror_repo}@${digest}" &>/dev/null
}

# oc adm catalog mirror stores related images under ${local_registry}/olm/<repo-with-slashes-as-dashes>:latest
function local_olm_image_from_source() {
  source_image="${1}"
  local_registry="${2}"
  local path="${source_image#*/}"
  path="${path%%@*}"
  path="${path%%:*}"
  echo "${local_registry}/olm/${path//\//-}:latest"
}

function configure_disconnected_database_image() {
  local default_image="${DEFAULT_DATABASE_IMAGE:-quay.io/sclorg/postgresql-15-c9s:latest}"

  if [ "${DISCONNECTED}" != "true" ]; then
    return 0
  fi

  if [ -n "${DATABASE_IMAGE:-}" ]; then
    return 0
  fi

  export DATABASE_IMAGE="$(local_olm_image_from_source "${default_image}" "${LOCAL_REGISTRY}")"
  echo "Using disconnected DATABASE_IMAGE from local OLM mirror"
}

function registry_configs_for_os_image_stream_sources() {
  release_image="${1}"
  authfile="${2}"
  release_mirror_repo="${3}"

  discover_os_image_stream_sources "${release_image}" "${authfile}" | \
    while IFS= read -r source; do
      [ -n "${source}" ] || continue
      registry_config "${source}" "$(mirror_repo_from_source "${release_mirror_repo}" "${source}")"
    done
}

function apply_os_image_stream_idms() {
  release_mirror_repo="${1}"
  shift
  local sources=("$@")
  local merged_sources=()
  local idms_entries=""
  local source mirror_repo existing_source

  while IFS= read -r existing_source; do
    [ -n "${existing_source}" ] || continue
    if ! array_contains "${existing_source}" "${merged_sources[@]}"; then
      merged_sources+=("${existing_source}")
    fi
  done < <(managed_osimagestream_idms_sources)

  for source in "${sources[@]}"; do
    if ! array_contains "${source}" "${merged_sources[@]}"; then
      merged_sources+=("${source}")
    fi
  done

  for source in "${merged_sources[@]}"; do
    if cluster_mirror_covers_source_except_managed "${source}"; then
      echo "Cluster mirror already covers ${source}, skipping"
      continue
    fi
    mirror_repo=$(mirror_repo_from_source "${release_mirror_repo}" "${source}")
    idms_entries="${idms_entries}
  - mirrors:
    - ${mirror_repo}
    source: ${source}"
  done

  if [ -z "${idms_entries}" ]; then
    return 0
  fi

  if ! oc get crd imagedigestmirrorsets.config.openshift.io &>/dev/null; then
    echo "ImageDigestMirrorSet CRD not available, skipping OSImageStream IDMS"
    return 0
  fi

  cat << EOF | oc apply -f -
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: assisted-osimagestream-mirror
spec:
  imageDigestMirrors:${idms_entries}
EOF
}

# Mirror OSImageStream images that are not covered by oc adm release mirror and
# publish IDMS entries for their source repositories.
function setup_os_image_stream_mirrors() {
  pull_secret_file="${1}"
  release_image="${2}"
  release_mirror_repo="${3}"

  local images sources=()
  local image source_repo mirror_repo digest source
  local mirrored=0 skipped=0

  images=$(discover_os_image_stream_images "${release_image}" "${pull_secret_file}")
  if [ -z "${images}" ]; then
    echo "No OSImageStream images discovered for ${release_image}, skipping OS image stream mirror setup"
    return 0
  fi

  while IFS= read -r image; do
    [ -n "${image}" ] || continue
    source_repo=$(image_repo_from_pullspec "${image}")
    digest="${image#*@}"
    mirror_repo=$(mirror_repo_from_source "${release_mirror_repo}" "${source_repo}")

    if local_mirror_has_digest "${pull_secret_file}" "${mirror_repo}" "${digest}"; then
      echo "Digest ${digest} already present at ${mirror_repo}, skipping mirror"
      skipped=$((skipped + 1))
    else
      echo "Mirroring OSImageStream image ${image} -> ${mirror_repo}"
      mirror_image_with_retry "${pull_secret_file}" "${image}" "${mirror_repo}"
      mirrored=$((mirrored + 1))
    fi

    if ! array_contains "${source_repo}" "${sources[@]}"; then
      sources+=("${source_repo}")
    fi
  done <<< "${images}"

  echo "OSImageStream mirror summary: ${mirrored} mirrored, ${skipped} already present locally" >&2

  if [ ${#sources[@]} -eq 0 ]; then
    return 0
  fi

  echo "Ensuring disconnected mirrors exist for OSImageStream sources: ${sources[*]}"
  apply_os_image_stream_idms "${release_mirror_repo}" "${sources[@]}"
}

function install_oc_mirrorv2(){
  OC_MIRROR_URL=${OC_MIRROR_URL:-https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-4.19/oc-mirror.tar.gz}
  curl -L --retry 5 --connect-timeout 30 -s "${OC_MIRROR_URL}" | tar xvz -C /usr/local/bin/
  chmod +x /usr/local/bin/oc-mirror
}
