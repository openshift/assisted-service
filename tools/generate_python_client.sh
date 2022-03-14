#!/usr/bin/env sh

set -o nounset

VERSION=${VERSION-$(bash -c "/generate.sh version")}

temp_swagger_file=$(mktemp)
sed '/pattern:/d' ${SWAGGER_FILE} > ${temp_swagger_file}
temp_config_file=$(mktemp)
temp_pyproject_file=$(mktemp)
cat > ${temp_config_file} << EOF
{
    "packageName": "assisted_service_client",
    "projectName": "assisted-service-client",
    "packageVersion": "$VERSION",
    "packageUrl": "https://github.com/openshift/assisted-service",
    "licenseName": "ASL"
}
EOF

1>&2 echo "Overriding swagger codegen setup.py and README.md templates..."
mkdir /templates

README_TEMPLATE_PATH="/templates/README.mustache"
SETUP_TEMPLATE_PATH="/templates/setup.mustache"
# Retrieve swagger README template
README_TEMPLATE=$(unzip -p /opt/swagger-codegen-cli/swagger-codegen-cli.jar python/README.mustache)

1>&2 echo "Generating ${README_TEMPLATE_PATH} with fixed Installation section..."
cat > "$README_TEMPLATE_PATH" <<EOF
$(echo "$README_TEMPLATE" | awk '/^### pip install/ {exit} {print}')
EOF
cat >> "$README_TEMPLATE_PATH" <<'EOF'
### From Pypi

The package is available in the Python Package Index. You can install it with:

```sh
pip install {{projectName}}
```

### From source distribution

If you download the source distribution, extract it and also install with pip:

```sh
sudo pip install .
```

Note that the sudo usage is only required if you want to install {{projectName}} system-wide. You can instead use a virtualenv both when installing from Pypi and from the source distribution.

EOF

cat >> "$README_TEMPLATE_PATH" <<EOF
$(echo "$README_TEMPLATE" | awk 'f;/## Getting Started/{f=1; print}')
EOF

1>&2 echo "Generating ${SETUP_TEMPLATE_PATH}..."
cat > /templates/setup.mustache <<EOF
import setuptools

setuptools.setup(
    name="{{projectName}}",
    description="REST API Client for OpenShift's Assisted Installer",
    setup_requires=[],
    version="{{packageVersion}}",
    author="RedHat, Inc",
    author_email="{{infoEmail}}",
    url="{{packageUrl}}",
    keywords=['Swagger', 'Openshift', 'AssistedInstaller'],
    install_requires=['certifi>=2017.4.17', 'python-dateutil>=2.1', 'six>=1.10', 'urllib3>=1.23'],
    packages=['test', 'assisted_service_client', 'assisted_service_client.models', 'assisted_service_client.api'],
    include_package_data=True,
    python_requires='>=3.6',
    long_description_content_type='text/markdown',
    classifiers=[
        'Development Status :: 3 - Alpha',
        'Intended Audience :: Developers',
        'Intended Audience :: Information Technology',
        'License :: OSI Approved :: Apache Software License',
        'Programming Language :: Python :: 3',
        'Programming Language :: Python :: 3.6',
        'Programming Language :: Python :: 3.7',
        'Programming Language :: Python :: 3.8',
        'Programming Language :: Python :: 3.9',
    ],
    long_description='''
$(cat "$README_TEMPLATE_PATH")
''',
)
EOF

java -jar /opt/swagger-codegen-cli/swagger-codegen-cli.jar generate \
  --verbose \
  --lang python \
  --config ${temp_config_file} \
  --output ${OUTPUT} \
  --input-spec ${temp_swagger_file} \
  --additional-properties infoEmail=support@redhat \
  --template-dir /templates
