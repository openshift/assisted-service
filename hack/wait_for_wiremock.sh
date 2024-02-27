#!/bin/bash

host=""
until [[ $host && $host != "null" ]]; do
    host=$(kubectl get service wiremock -n $NAMESPACE -ojson | jq -r '.status.loadBalancer.ingress[0].ip // .status.loadBalancer.ingress[0].hostname')
    port=$(kubectl get service wiremock -n $NAMESPACE -ojson | jq -r '.spec.ports[0].port')
    echo "Wiremock host is currently $host"
    echo "Wiremock Port is currently $port"
    sleep 5
done

echo "Testing wiremock readiness..."
wiremock_url="http://$host:$port/__admin/mappings"
status_code=0
until [[ $status_code -eq 200 ]]; do
    status_code=$(curl -o /dev/null -s -w "%{http_code}" $wiremock_url)
    echo "Wiremock is not ready yet"
    sleep 5
done

echo "Wiremock is ready"
