function mirror_command_succeeded() {
  log_file="${1}"
  local grep_rc=0

  grep -qE 'one or more errors occurred|errors during mirroring|error: unable to copy layer' "${log_file}" || grep_rc=$?
  case "${grep_rc}" in
    0) return 1 ;;
    1) return 0 ;;
    *) return 2 ;;
  esac
}

function mirror_command_name() {
  local args=("$@")
  local i=0

  if [[ "${args[0]}" == "env" ]]; then
    i=1
    while [[ "${args[i]}" == *=* ]]; do
      i=$((i + 1))
    done
  fi

  echo "${args[i]}"
}

function run_mirror_command_with_retry() {
  attempts="${MIRROR_RETRY_ATTEMPTS:-5}"
  interval="${MIRROR_RETRY_INTERVAL:-60}"
  log_file=""
  rc=0
  success_rc=0
  cmd_name=$(mirror_command_name "$@")
  # Callers often enable xtrace; disable it around command execution so registry
  # hosts and image refs from argv are not expanded into CI logs.
  local was_xtrace=false
  [[ $- == *x* ]] && was_xtrace=true
  # shopt -qo queries set -o options; plain shopt -q does not detect pipefail.
  local was_pipefail=false
  shopt -qo pipefail && was_pipefail=true

  for attempt in $(seq 1 "${attempts}"); do
    if ! log_file=$(mktemp); then
      echo "Mirror failed: could not create temporary log file" >&2
      return 1
    fi
    echo "Mirror attempt ${attempt}/${attempts}: ${cmd_name}"

    set +x
    "$@" > "${log_file}" 2>&1
    rc=$?
    ${was_xtrace} && set -x

    success_rc=0
    mirror_command_succeeded "${log_file}" || success_rc=$?

    if [[ "${rc}" -eq 0 && "${success_rc}" -eq 0 ]]; then
      echo "Mirror attempt ${attempt}/${attempts}: ${cmd_name} succeeded"
      rm -f "${log_file}"
      return 0
    fi

    echo "Mirror failed (exit=${rc}), waiting ${interval}s before retry..."
    if [[ "${attempt}" -lt "${attempts}" ]]; then
      sleep "${interval}"
    else
      if [[ -n "${ARTIFACT_DIR:-}" ]]; then
        cp "${log_file}" "${ARTIFACT_DIR}/mirror_failed_${cmd_name}_${attempt}.log"
      fi
    fi
    rm -f "${log_file}"
  done

  return 1
}

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

  run_mirror_command_with_retry env GODEBUG=x509ignoreCN=0 podman push \
        --tls-verify=false \
        "${local_registry_index_tag}" \
        --authfile "${authfile}"

  manifests_dir=$(mktemp -d -t manifests-XXXXXXXXXX)
  run_mirror_command_with_retry env GODEBUG=x509ignoreCN=0 oc adm catalog mirror \
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

function release_image_digest() {
  case "${1}" in
    *@sha256:*) echo "${1##*@}" ;;
    *) return 1 ;;
  esac
}

# Returns 0 when the release payload manifest is already present at the destination repo.
function registry_release_image_exists() {
  authfile="${1}"
  dest_mirror_repo="${2}"
  release_digest="${3}"
  local image="${dest_mirror_repo}@${release_digest}"
  local rc=0

  set +e
  if [ -n "${authfile}" ] && [ -f "${authfile}" ]; then
    oc image info -a "${authfile}" "${image}" >/dev/null 2>&1
  else
    oc image info "${image}" >/dev/null 2>&1
  fi
  rc=$?
  set -e
  [ "${rc}" -eq 0 ]
}

function ocp_mirror_release() {
  authfile="${1}"
  source_image="${2}"
  dest_mirror_repo="${3}"
  local release_digest=""
  local max_attempts="${OCP_MIRROR_RELEASE_RETRIES:-5}"
  if ! [[ "${max_attempts}" =~ ^[1-9][0-9]*$ ]]; then
    echo "OCP_MIRROR_RELEASE_RETRIES must be a positive integer" >&2
    return 1
  fi
  local retry_delay="${OCP_MIRROR_RELEASE_RETRY_DELAY:-60}"
  local max_per_registry="${OCP_MIRROR_RELEASE_MAX_PER_REGISTRY:-2}"
  local attempt=1
  local output_file=""
  local rc=0
  local mirror_ok=0
  # Disable tracing around mirror I/O: xtrace would otherwise print registry
  # hostnames and image refs from argv and captured output into CI logs.
  local was_xtrace=false
  [[ $- == *x* ]] && was_xtrace=true

  if ! release_digest=$(release_image_digest "${source_image}"); then
    echo "Release mirror failed: could not determine digest from source image" >&2
    return 1
  fi

  if registry_release_image_exists "${authfile}" "${dest_mirror_repo}" "${release_digest}"; then
    echo "Release image ${release_digest} already present at ${dest_mirror_repo}, skipping mirror"
    return 0
  fi

  while [ "${attempt}" -le "${max_attempts}" ]; do
    if registry_release_image_exists "${authfile}" "${dest_mirror_repo}" "${release_digest}"; then
      echo "Release image ${release_digest} is now present at ${dest_mirror_repo}, skipping remaining mirror attempts"
      return 0
    fi

    echo "Release mirror attempt ${attempt}/${max_attempts} (max-per-registry=${max_per_registry})"
    if ! output_file=$(mktemp); then
      echo "Release mirror failed: could not create temporary log file" >&2
      return 1
    fi

    set +x
    # Keep set -e safe: a failing oc must not abort before we can inspect/retry.
    # Limit concurrent registry uploads: parallel blob PATCHes to the local registry
    # have been observed to trigger HTTP/2 CANCEL / unexpected EOF under load.
    rc=0
    oc adm -a "${authfile}" release mirror \
           --from="${source_image}" \
           --to="${dest_mirror_repo}" \
           --max-per-registry="${max_per_registry}" \
           >"${output_file}" 2>&1 || rc=$?
    ${was_xtrace} && set -x

    mirror_ok=0
    if [ "${rc}" -eq 0 ]; then
      mirror_command_succeeded "${output_file}" || mirror_ok=$?
      if [ "${mirror_ok}" -eq 0 ] && registry_release_image_exists "${authfile}" "${dest_mirror_repo}" "${release_digest}"; then
        rm -f "${output_file}"
        echo "Release mirror succeeded (attempt ${attempt}/${max_attempts})"
        return 0
      fi
      if [ "${mirror_ok}" -eq 0 ]; then
        echo "Release mirror finished without errors but ${dest_mirror_repo}@${release_digest} is not present in the registry" >&2
      fi
      rc=1
    fi

    echo "Release mirror failed (attempt ${attempt}/${max_attempts}, exit=${rc})." >&2
    if [[ -n "${ARTIFACT_DIR:-}" ]]; then
      cp "${output_file}" "${ARTIFACT_DIR}/ocp_mirror_release_failed_${attempt}.log"
      echo "Complete log saved to ${ARTIFACT_DIR}/ocp_mirror_release_failed_${attempt}.log" >&2
    fi

    if registry_release_image_exists "${authfile}" "${dest_mirror_repo}" "${release_digest}"; then
      rm -f "${output_file}"
      echo "Release image ${release_digest} is present at ${dest_mirror_repo} after a failed attempt, continuing"
      return 0
    fi

    if [ "${attempt}" -ge "${max_attempts}" ] || ! transient_registry_error_file "${output_file}"; then
      rm -f "${output_file}"
      echo "Release mirror failed (attempt ${attempt}/${max_attempts})" >&2
      return 1
    fi
    rm -f "${output_file}"

    echo "Release mirror failed with a transient registry error (attempt ${attempt}/${max_attempts}), retrying in ${retry_delay}s..." >&2
    sleep "${retry_delay}"
    attempt=$((attempt + 1))
  done
}

function transient_registry_error_file() {
  grep -Eqi 'unexpected EOF|504 Gateway|502 Bad Gateway|503 Service Unavailable|connection reset|TLS handshake timeout|broken pipe|i/o timeout|use of closed network connection' "${1}"
}

function image_repo_from_pullspec() {
  echo "${1%%@*}"
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

function discover_os_image_stream_sources_from_release_json() {
  release_image="${1}"
  authfile="${2}"

  oc adm -a "${authfile}" release info "${release_image}" -o json | \
	jq -r '[.. | strings | select(test("^quay.io/openshift-release-dev/ocp-v[0-9]+\\.[0-9]+-art-dev@sha256:"))] | map(split("@")[0]) | unique | .[]'
}

# Unique quay.io art-dev repositories referenced by a release payload.
# OCP 4.x uses ocp-v4.0-art-dev; OCP 5.x uses ocp-v5.0-art-dev (and may still
# reference v4 for OSImageStream / hard-coded images).
function discover_release_art_dev_sources() {
  release_image="${1}"
  authfile="${2}"
  shift 2
  local mco_image=""

  {
    printf '%s\n' "$@"
    discover_os_image_stream_sources_from_release_json "${release_image}" "${authfile}"
    if mco_image=$(oc adm -a "${authfile}" release info "${release_image}" --image-for machine-config-operator 2>/dev/null); then
      image_repo_from_pullspec "${mco_image}"
    fi
  } | sed '/^$/d' | sort -u
}

# Emit YAML list entries suitable for HyperShift --image-content-sources and for
# ImageDigestMirrorSet / ImageContentSourcePolicy digest-mirror lists.
function art_dev_image_content_source_entries() {
  release_image="${1}"
  authfile="${2}"
  mirror_registry="${3}"
  shift 3
  local source repo_suffix

  while IFS= read -r source; do
    [ -n "${source}" ] || continue
    repo_suffix="${source#quay.io/}"
    cat << EOF
- mirrors:
  - ${mirror_registry}
  - ${mirror_registry}/${repo_suffix}
  source: ${source}
EOF
  done < <(discover_release_art_dev_sources "${release_image}" "${authfile}" "$@")
}

# Discover OSImageStream source repositories for registries.conf. The MCO helper
# matches bootstrap discovery; release metadata is a fallback for older payloads.
function discover_os_image_stream_sources() {
  release_image="${1}"
  authfile="${2}"
  local images mco_status=0

  images=$(discover_os_image_stream_images_from_mco_tool "${release_image}" "${authfile}") || mco_status=$?

  if [ "${mco_status}" -eq 0 ] && [ -n "${images}" ]; then
    printf '%s\n' "${images}" | while IFS= read -r image; do
      [ -n "${image}" ] || continue
      image_repo_from_pullspec "${image}"
    done | sort -u
    return 0
  fi

  if [ "${mco_status}" -eq 2 ]; then
    return 1
  fi

  echo "machine-config-osimagestream unavailable; falling back to release metadata scan" >&2
  discover_os_image_stream_sources_from_release_json "${release_image}" "${authfile}"
}

function registry_configs_for_os_image_stream_sources() {
  release_image="${1}"
  authfile="${2}"
  release_mirror_repo="${3}"
  shift 3

  while IFS= read -r source; do
    [ -n "${source}" ] || continue
    for skip_repo in "$@"; do
      [ "${source}" = "${skip_repo}" ] && continue 2
    done
    registry_config "${source}" "${release_mirror_repo}"
  done < <(discover_os_image_stream_sources "${release_image}" "${authfile}")
}

function install_oc_mirrorv2(){
  OC_MIRROR_URL=${OC_MIRROR_URL:-https://mirror.openshift.com/pub/cgw/oc-mirror/latest/oc-mirror-rhel9-linux-amd64.tar.gz}
  curl -L --retry 5 --connect-timeout 30 -s "${OC_MIRROR_URL}" | tar xvz -C /usr/local/bin/
  chmod +x /usr/local/bin/oc-mirror
}
