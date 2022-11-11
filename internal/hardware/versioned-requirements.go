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
		return copyVersionedHostRequirements(&requirements), nil
	}

	if requirements, ok := (*d)[DefaultVersion]; ok {
		return copyVersionedHostRequirements(&requirements), nil
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
		// in case we don't set edge worker requirements
		// we should handle it as regular worker
		if rq.EdgeWorkerRequirements == nil {
			rq.EdgeWorkerRequirements = rq.WorkerRequirements
		}
		versionToRequirements[rq.Version] = rq
	}
	*d = versionToRequirements
	return d.validate()
}

func (d *VersionedRequirementsDecoder) validate() error {
	for version, requirements := range *d {
		err := validateDetails(requirements.WorkerRequirements, version, string(models.HostRoleWorker))
		if err != nil {
			return err
		}
		err = validateDetails(requirements.MasterRequirements, version, string(models.HostRoleMaster))
		if err != nil {
			return err
		}
		err = validateDetails(requirements.SNORequirements, version, "SNO")
		if err != nil {
			return err
		}
		err = validateDetails(requirements.EdgeWorkerRequirements, version, "EDGE-WORKER")
		if err != nil {
			return err
		}
	}
	return nil
}

func validateDetails(details *models.ClusterHostRequirementsDetails, version string, role string) error {
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

func copyVersionedHostRequirements(requirements *models.VersionedHostRequirements) *models.VersionedHostRequirements {
	return &models.VersionedHostRequirements{
		Version:                requirements.Version,
		MasterRequirements:     copyClusterHostRequirementsDetails(requirements.MasterRequirements),
		WorkerRequirements:     copyClusterHostRequirementsDetails(requirements.WorkerRequirements),
		SNORequirements:        copyClusterHostRequirementsDetails(requirements.SNORequirements),
		EdgeWorkerRequirements: copyClusterHostRequirementsDetails(requirements.EdgeWorkerRequirements),
	}
}

func copyClusterHostRequirementsDetails(details *models.ClusterHostRequirementsDetails) *models.ClusterHostRequirementsDetails {
	return &models.ClusterHostRequirementsDetails{
		CPUCores:                         details.CPUCores,
		DiskSizeGb:                       details.DiskSizeGb,
		InstallationDiskSpeedThresholdMs: details.InstallationDiskSpeedThresholdMs,
		RAMMib:                           details.RAMMib,
		NetworkLatencyThresholdMs:        details.NetworkLatencyThresholdMs,
		PacketLossPercentage:             details.PacketLossPercentage,
	}
}
