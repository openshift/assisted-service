package lvm

const (
	LvmoMinOpenshiftVersion                            string = "4.11.0"
	LvmsMinOpenshiftVersion4_12                        string = "4.12.0"
	LvmsMinOpenshiftVersion_ForNewResourceRequirements string = "4.13.0"
	LvmMinMultiNodeSupportVersion                      string = "4.15.0"

	LvmoSubscriptionName string = "odf-lvm-operator"
	LvmsSubscriptionName string = "lvms-operator"
)

type Config struct {
	LvmCPUPerHost                 int64 `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB           int64 `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"400"`
	LvmMemoryPerHostMiBBefore4_13 int64 `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
}
