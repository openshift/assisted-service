#!/usr/bin/env bash

# Get latest digest SHAs
service="quay.io/ocpmetal/assisted-service@"$(curl -G https://quay.io/api/v1/repository/ocpmetal/assisted-service/tag/ | jq -r '.tags[] | select(.name=="latest" and (has("expiration") | not)) | .manifest_digest')
agent="quay.io/ocpmetal/assisted-installer-agent@"$(curl -G https://quay.io/api/v1/repository/ocpmetal/assisted-installer-agent/tag/ | jq -r '.tags[] | select(.name=="latest" and (has("expiration") | not)) | .manifest_digest')
controller="quay.io/ocpmetal/assisted-installer-controller@"$(curl -G https://quay.io/api/v1/repository/ocpmetal/assisted-installer-controller/tag/ | jq -r '.tags[] | select(.name=="latest" and (has("expiration") | not)) | .manifest_digest')
installer="quay.io/ocpmetal/assisted-installer@"$(curl -G https://quay.io/api/v1/repository/ocpmetal/assisted-installer/tag/ | jq -r '.tags[] | select(.name=="latest" and (has("expiration") | not)) | .manifest_digest')
postgres="quay.io/ocpmetal/postgresql-12-centos7@"$(curl -G https://quay.io/api/v1/repository/ocpmetal/postgresql-12-centos7/tag/ | jq -r '.tags[] | select(.name=="latest" and (has("expiration") | not)) | .manifest_digest')

# Echo Current digest shas:
echo "Current digest SHAs:"
echo "$service"
echo "$agent"
echo "$controller"
echo "$installer"
echo "$postgres"

# Replace digest SHAs before bundle build
sed -i "s%quay.io/ocpmetal/assisted-service:latest%${service}%g" config/manager/manager.yaml
sed -i "s%quay.io/ocpmetal/assisted-installer-agent:latest%${agent}%g" config/manager/manager.yaml
sed -i "s%quay.io/ocpmetal/assisted-installer-controller:latest%${controller}%g" config/manager/manager.yaml
sed -i "s%quay.io/ocpmetal/assisted-installer:latest%${installer}%g" config/manager/manager.yaml
sed -i "s%quay.io/ocpmetal/postgresql-12-centos7:latest%${postgres}%g" config/manager/manager.yaml
