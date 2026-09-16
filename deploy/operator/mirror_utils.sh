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
  OC_MIRROR_URL=${OC_MIRROR_URL:-https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-4.19/oc-mirror.tar.gz}
  curl -L --retry 5 --connect-timeout 30 -s "${OC_MIRROR_URL}" | tar xvz -C /usr/local/bin/
  chmod +x /usr/local/bin/oc-mirror
}
