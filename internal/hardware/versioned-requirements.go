package hardware

import (
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/models"
)

const DefaultVersion = "default"

type VersionedRequirementsDecoder map[string]models.VersionedHostRequirements

func (d *VersionedRequirementsDecoder) GetVersionedHostRequirements(version string) (*models.VersionedHostRequirements, error) {
	if requirements, ok := (*d)[version]; ok {
		return &requirements, nil
	}

	if requirements, ok := (*d)[DefaultVersion]; ok {
		return &requirements, nil
	}
	return nil, fmt.Errorf("requirements for version %v not found", version)
}

func (d *VersionedRequirementsDecoder) Decode(value string) error {
	var requirements []models.VersionedHostRequirements
	err := json.Unmarshal([]byte(value), &requirements)
	if err != nil {
		return err
	}

	versionToRequirements := make(VersionedRequirementsDecoder)
	for _, rq := range requirements {
		versionToRequirements[rq.Version] = rq
	}
	*d = versionToRequirements
	return d.validate()
}

func (d *VersionedRequirementsDecoder) validate() error {
	for version, requirements := range *d {
		err := validateDetails(requirements.WorkerRequirements, version, models.HostRoleWorker)
		if err != nil {
			return err
		}
		err = validateDetails(requirements.MasterRequirements, version, models.HostRoleMaster)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateDetails(details *models.ClusterHostRequirementsDetails, version string, role models.HostRole) error {
	if details == nil {
		return fmt.Errorf("requirements for %v role must be provided for version %v", role, version)
	}
	if details.RAMMib <= 0 {
		return fmt.Errorf("RAM requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if details.DiskSizeGb <= 0 {
		return fmt.Errorf("disk size requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if details.CPUCores <= 0 {
		return fmt.Errorf("CPU cores requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if details.InstallationDiskSpeedThresholdMs < 0 {
		return fmt.Errorf("CPU cores requirement must not be negative for version %v and %v role", version, role)
	}
	return nil
}
