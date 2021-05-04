function mirror_package() {
  # e.g. "local-storage-operator"
  package="${1}"

  # e.g. "redhat-operator-index", "certified-operator-index",
  # "community-operator-index", "redhat-marketplace-index"
  catalog="${2}"

  # e.g. "4.8"
  ocp_release="${3}"

  # e.g. "virthost.ostest.test.metalkube.org:5000"
  local_registry="${4}"

  # e.g. "/run/user/0/containers/auth.json", "~/.docker/config.json"
  # should have authentication information for both official registry
  # (pull-secret) and for the local registry
  authfile="${5}"

  remote_index="registry.redhat.io/redhat/${catalog}:v${ocp_release}"
  local_registry_index_tag="${local_registry}/olm-index/${catalog}:v${ocp_release}"
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

  cat > "${manifests_dir}/catalogSource.yaml" << EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: mirror-catalog-for-${package}
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${local_registry_index_tag}
  displayName: Mirror Index to package ${package} on ${catalog}
  publisher: Local
  updateStrategy:
    registryPoll:
      interval: 30m
EOF

  oc apply -f "${manifests_dir}/imageContentSourcePolicy.yaml"
  oc apply -f "${manifests_dir}/catalogSource.yaml"
}

function disable_default_indexes() {
  oc patch OperatorHub cluster --type json \
        -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'
}

function merge_authfiles() {
  jq -s '.[0] * .[1]' "${1}" "${2}" > "${3}"
}

function install_opm() {
  curl -s https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-4.7/opm-linux.tar.gz | tar xvz -C /usr/local/bin/
}
