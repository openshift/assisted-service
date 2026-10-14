package lvm

const (
	LvmoMinOpenshiftVersion                            string = "4.11.0"
	LvmsMinOpenshiftVersion4_12                        string = "4.12.0"
	LvmsMinOpenshiftVersion_ForNewResourceRequirements string = "4.13.0"
	LvmMinMultiNodeSupportVersion                      string = "4.15.0"
	LvmNewResourcesOpenshiftVersion4_16                string = "4.16.0"
	// LvmsCatalogFallbackMinVersion is the first OCP version where lvms-operator is not
	// published to the default redhat-operators catalog. A fallback CatalogSource pointing
	// to the previous catalog index is injected so the Subscription can resolve.
	// TODO: Remove once lvms-operator is published to the 4.22 redhat-operators catalog.
	LvmsCatalogFallbackMinVersion string = "4.22.0"
	LvmsCatalogFallbackName       string = "redhat-operators-v4-21"
	LvmsCatalogFallbackImage      string = "registry.redhat.io/redhat/redhat-operator-index:v4.21"

	LvmoSubscriptionName string = "odf-lvm-operator"
	LvmsSubscriptionName string = "lvms-operator"
)

type Config struct {
	LvmCPUPerHost                 int64 `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB           int64 `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"400"`
	LvmMemoryPerHostMiBBefore4_13 int64 `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
	LvmMemoryPerHostMiBFrom4_16   int64 `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"100"`
}
