package host

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
)

type conditionId string
type condition struct {
	id conditionId
	fn func(c *validationContext) bool
}

const (
	InstallationDiskSpeedCheckSuccessful = conditionId("installation-disk-speed-check-successful")
	ClusterInsufficient                  = conditionId("cluster-insufficient")
)

func (c conditionId) String() string {
	return string(c)
}

func (v *validator) isInstallationDiskSpeedCheckSuccessful(c *validationContext) bool {
	info, err := v.getBootDeviceInfo(c.host)
	return err == nil && info != nil && info.DiskSpeed != nil && info.DiskSpeed.Tested && info.DiskSpeed.ExitCode == 0
}

func (v *validator) isClusterInsufficient(c *validationContext) bool {
	return swag.StringValue(c.cluster.Status) == models.ClusterStatusInsufficient
}
