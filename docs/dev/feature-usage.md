## Feature Usage

Feature usage is a JSON-formatted field on the Cluster object designed for monitoring
how often a feature of Assisted Service is being used.

Developers should add an entry to the feature usage structure for each customer-facing feature.
The data can be as simple as marking whether a feature is configured or not (the default setup)
or more elaborate, depending on the data the Product owners like to get.

For example, we report which network type is employed (OVN, SDN) to monitor Network type configuration.
For monitoring usage of Additional NTP Sources, we also add a property that counts how many sources
the customer added.

### How to add a usage entry

1. Add a constant to **_usage/consts.go_**
2. If the feature behavior is set at cluster creation time, add a call to SetUsage in **_setDefaultUsage_**
3. Call SetUsage when the feature behavior is updated. For example, at _updateClusterData_ or at _V2UpdateHostInternal_

**_SetUsage_** is a helper function. When calling it you should specify the following parameters:

> Important note:
>
> Since we also maintain a feature support list (ref: featuresupport/support_levels_list.go),
> If you do add a new constant to usage/consts.go, Please add the ID into swagger: `#/definitions/feature-support-level`.
>
> The feature ID is CAPITAL_SNAKE_CASE_OF_CONST. You can refer to `usage/manager.go:UsageNameToID` to see
> how the ID is generated.
>
> Update the feature support level if needed.

**enabled**: Whether or not to mark the feature as activated. This usually involves invoking some
elaborate logic or calling setUsage itself from the logic that defines the activation of the feature.

**name**: The constant you defined in step (1).
This constant identifies the feature in dashboards and in the UI.

**prop**: Optional parameter that holds the extra properties you might want to report for this feature.

**usages**: An array that holds the un-marshalled value of Cluster.feature_usage field.

Sometimes your feature update logic is defined outside inventory.go.
In this case, you should call the usage API directly, marshall the feature_usage field by yourself,
and make sure this call is enclosed within a transaction and that the cluster is read with FOR UPDATE option.

### Guidelines for adding usage information

1. Keep the description of the constant as concise as possible
2. Do not add extra properties unless requested. Properties are indexed by the report system
   and add complexity to the Elastic database
3. Property keys MUST BE constant. Never use filenames, IDs or IPs as this will overload the indexing system
4. Value of properties are meant for aggregations.
   For example: number of addition ntp sources is a good candidate for an extra property
   because we can then present their average value. IPs or filenames are not.
