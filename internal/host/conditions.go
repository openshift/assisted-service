package host

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

type conditionId string
type condition struct {
	id conditionId
	fn func(c *validationContext) bool
}

const (
	InstallationDiskSpeedCheckSuccessful = conditionId("installation-disk-speed-check-successful")
	ClusterPreparingForInstallation      = conditionId("cluster-preparing-for-installation")
	ClusterPendingUserAction             = conditionId("cluster-pending-user-action")
	ClusterInstalling                    = conditionId("cluster-installing")
	ValidRoleForInstallation             = conditionId("valid-role-for-installation")
	StageInWrongBootStages               = conditionId("stage-in-wrong-boot-stages")
	ClusterInError                       = conditionId("cluster-in-error")
	SuccessfulContainerImageAvailability = conditionId("successful-container-image-availability")
)

func (c conditionId) String() string {
	return string(c)
}

func (v *validator) isInstallationDiskSpeedCheckSuccessful(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}

	if hostutil.SaveDiskPartitionsIsSet(c.host.InstallerArgs) {
		return true
	}

	info, err := v.getBootDeviceInfo(c.host)
	return err == nil && info != nil && info.DiskSpeed != nil && info.DiskSpeed.Tested && info.DiskSpeed.ExitCode == 0
}

func (v *validator) isClusterPreparingForInstallation(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	return swag.StringValue(c.cluster.Status) == models.ClusterStatusPreparingForInstallation
}

func (v *validator) isClusterInstalling(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	return swag.StringValue(c.cluster.Status) == models.ClusterStatusInstalling
}

func (v *validator) isClusterInError(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	return swag.StringValue(c.cluster.Status) == models.ClusterStatusError
}

func (v *validator) isClusterPendingUserAction(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	return swag.StringValue(c.cluster.Status) == models.ClusterStatusInstallingPendingUserAction
}

func (v *validator) isValidRoleForInstallation(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	validRoles := []string{string(models.HostRoleMaster), string(models.HostRoleWorker)}
	return funk.ContainsString(validRoles, string(c.host.Role))
}

func (v *validator) isStageInWrongBootStages(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	return funk.Contains(WrongBootOrderIgnoreTimeoutStages, c.host.Progress.CurrentStage)
}

func (v *validator) isSuccessfulContainerImageAvailability(c *validationContext) bool {
	if c.infraEnv != nil {
		return false
	}
	imagesStatuses, err := common.UnmarshalImageStatuses(c.host.ImagesStatus)
	return err == nil && len(imagesStatuses) > 0 && allImagesValid(imagesStatuses)
}
