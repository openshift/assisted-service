echo Compiling and running go state machines JSON dump...
TMP_MACHINES=$(mktemp)
go run "${SCRIPT_DIR}"/main.go | jq --slurp '.' > "${TMP_MACHINES}"

echo Generating host JSON...
cat "${TMP_MACHINES}" | jq '.[0] | .name = "Host" | .description = "The host state machine helps the service orchestrate the host lifecycle of hosts that are already bound to a cluster"' > $HOST_JSON
echo "${HOST_JSON}" generated

echo Generating cluster JSON...
cat "${TMP_MACHINES}" | jq '.[1] | .name = "Cluster" | .description = "The cluster state machine helps the service track the installation lifecycle of a cluster"' > $CLUSTER_JSON
echo "${CLUSTER_JSON}" generated

echo Generating unbound host JSON...
cat "${TMP_MACHINES}" | jq '.[1] | .name = "Unbound Host" | .description = "The unbound host state machine helps the service orchestrate the host lifecycle of hosts that are not bound to a cluster"' > $UNBOUND_HOST_JSON
echo "${UNBOUND_HOST_JSON}" generated
