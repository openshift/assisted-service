# Support Level

This document provides instructions on how to use the feature support-level API and how to add a new feature and set its support level dynamically.

Support level is a way to determine whether a feature or architecture can be used, and it defines the current support level of that feature. 

The available support-levels are:
- **supported** - Feature is fully supported.
- **unsupported** - Feature is not supported for the given parameters.
- **tech-preview**: Feature is not fully supported, may not be functionally complete, and not suitable for
  deployment in production. However, these features are provided to the customer as a courtesy and the primary goal
  is for the feature to gain wider exposure with the goal of full support in the future.
- **dev-preview**: Feature is considered for possible inclusion into future releases in order to collect feedback about the feature.
- **unavailable**: Feature is not available for the selected arguments.

The Support Level API has two different endpoints:
- `GET /v2/support-levels/features` - Get the support level for a given OpenShift version and/or CPU architecture
- `GET /v2/support-levels/architectures` - Get the supported CPU architectures for a given OpenShift version

Note that the previous API `/v2/feature-support-levels` is now deprecated.


## Feature Support Level
This endpoint returns a list of supported features for a specific OpenShift version. 
The support level can be evaluated by passing the CPU architecture as a query parameter.
The result is evaluated dynamically. If a feature isn't supported on the given architecture it will marked as `unavailable`, 
passing CPU architecture will change only the support level and not the amount of features.



### Using the Feature Support Level Endpoint

To get a list of feature support levels on OpenShift version <version> with CPU architecture <architecture>, use the following command:
```bash
curl --request GET '<HOST>:<PORT>/api/assisted-install/v2/support-levels/features?openshift_version=<version>&cpu_architecture=<architecture>'
```
Note that the CPU architecture parameter is optional and defaults to x86_64.

Response example for OpenShift version 4.12 with ARM CPU architecture:
```json
{
  "features": {
    "CLUSTER_MANAGED_NETWORKING": "supported",
    "CNV": "unsupported",
    "CUSTOM_MANIFEST": "supported",
    "DUAL_STACK_VIPS": "unsupported",
    "LVM": "dev-preview",
    "NUTANIX_INTEGRATION": "unsupported",
    "ODF": "unsupported",
    "SINGLE_NODE_EXPANSION": "supported",
    "SNO": "supported",
    "USER_MANAGED_NETWORKING": "supported",
    "VIP_AUTO_ALLOC": "dev-preview",
    "VSPHERE_INTEGRATION": "supported"
  }
}
```

## Architecture Support Level
This endpoint returns a list of supported architectures for a specific OpenShift version. 
The service dynamically generates and returns a list of architectures and their support level for the given OpenShift version.


### Using the Architecture Support Level Endpoint

To get a list of architecture support levels on OpenShift version <version>, use the following command:
```bash
curl --request GET '<HOST>:<PORT>/api/assisted-install/v2/support-levels/architectures?openshift_version=<version>'
```

Response example for OpenShift version 4.12:
```json
{
  "architectures": {
    "ARM64_ARCHITECTURE": "supported",
    "MULTIARCH_RELEASE_IMAGE": "tech-preview",
    "PPC64LE_ARCHITECTURE": "unsupported",
    "S390X_ARCHITECTURE": "unsupported",
    "X86_64_ARCHITECTURE": "supported"
  }
}

```
## Support Level and Feature Usage
Both feature support-level and feature-usage are representing features on Assisted Installer, but despite the 
similarity of this two, they are representing different things.
When adding a new feature to the feature-usage API, it is not necessary to add the same feature to the supported features enum.

## Adding a new feature
To add a new feature to the support-level API, follow these steps: 
1. Add a new enum to `feature-support-level-id` under the `swagger.yaml` file - [here](https://github.com/openshift/assisted-service/blob/master/swagger.yaml#L3910-#L3924)
2. Generate `models` and `vendor` files - `skipper make generate-from-swagger && skipper make generate-vendor`
3. Create a new struct that represent the new feature and follows the `SupportLevelFeature` [interface](https://github.com/openshift/assisted-service/blob/master/internal/featuresupport/features.go#L18-#L25)
4. Initiate the new object on [featuresList](https://github.com/openshift/assisted-service/blob/master/internal/featuresupport/feature_support_level.go#L13)
map and map the newly generated feature-id to the new object.
5. Test the new logic
