# Static Network Configuration

User may provide static network configurations when generating or updating the discovery ISO.

## Sample KubeAPI CR

[This sample CR](../../hive-integration/crds/nmstate.yaml) shows how to create a custom NMStateConfig to be used with Assisted Service on-premises.
:stop_sign: Note that due to the ignition content length limit (`256Ki`), there is a limit to the amount of NMStateConfigs that can be included with a single InfraEnv. With a config sample such as [this one](../../hive-integration/crds/nmstate.yaml), the limit per each InfraEnv is 3960 configurations.
