#!/usr/bin/env sh

set -o nounset

temp_swagger_file=$(mktemp)
sed '/pattern:/d' ${SWAGGER_FILE} >${temp_swagger_file}
temp_config_file=$(mktemp)
echo '{"packageName" : "assisted_service_client", "packageVersion": "1.0.0"}' >${temp_config_file}
java -jar /opt/swagger-codegen-cli/swagger-codegen-cli.jar generate --lang python --config ${temp_config_file} --output ${OUTPUT} --input-spec ${temp_swagger_file}
