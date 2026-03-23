package hardware

import (
	"encoding/json"
	"fmt"

	goversion "github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/models"
)

const DefaultVersion = "default"

type VersionedRequirementsDecoder map[string]models.VersionedHostRequirements

// GetVersionedHostRequirements returns requirements for the given OCP version.
// Lookup order: exact match → highest versioned entry ≤ requested version → "default".
func (d *VersionedRequirementsDecoder) GetVersionedHostRequirements(version string) (*models.VersionedHostRequirements, error) {
	if requirements, ok := (*d)[version]; ok {
		return copyVersionedHostRequirements(&requirements), nil
	}

	requestedV, err := goversion.NewVersion(version)
	if err == nil {
		var bestMatch models.VersionedHostRequirements
		var bestVersion *goversion.Version
		for k, v := range *d {
			if k == DefaultVersion {
				continue
			}
			entryV, err := goversion.NewVersion(k)
			if err != nil {
				continue
			}
			if entryV.LessThanOrEqual(requestedV) && (bestVersion == nil || entryV.GreaterThan(bestVersion)) {
				bestMatch = v
				bestVersion = entryV
			}
		}
		if bestVersion != nil {
			return copyVersionedHostRequirements(&bestMatch), nil
		}
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
		// This is only until we add arbiter to HW_VALIDATOR_REQUIREMENTS in all environments
		if rq.ArbiterRequirements == nil {
			rq.ArbiterRequirements = rq.WorkerRequirements
		}
		versionToRequirements[rq.Version] = rq
	}

	// Merge non-default entries with default so partial entries only need to
	// specify the fields that differ.
	if defaultReq, ok := versionToRequirements[DefaultVersion]; ok {
		for key, rq := range versionToRequirements {
			if key == DefaultVersion {
				continue
			}
			versionToRequirements[key] = mergeWithDefault(rq, defaultReq)
		}
	}

	*d = versionToRequirements
	return d.validate()
}

// mergeWithDefault fills missing roles and zero-valued fields in rq from defaultReq.
func mergeWithDefault(rq, defaultReq models.VersionedHostRequirements) models.VersionedHostRequirements {
	rq.MasterRequirements = mergeRoleDetails(rq.MasterRequirements, defaultReq.MasterRequirements)
	rq.ArbiterRequirements = mergeRoleDetails(rq.ArbiterRequirements, defaultReq.ArbiterRequirements)
	rq.WorkerRequirements = mergeRoleDetails(rq.WorkerRequirements, defaultReq.WorkerRequirements)
	rq.SNORequirements = mergeRoleDetails(rq.SNORequirements, defaultReq.SNORequirements)
	rq.EdgeWorkerRequirements = mergeRoleDetails(rq.EdgeWorkerRequirements, defaultReq.EdgeWorkerRequirements)
	return rq
}

// mergeRoleDetails fills zero-valued required fields and nil pointer fields in rq from def.
// Zero values are treated as unspecified since validation rejects them.
func mergeRoleDetails(rq, def *models.ClusterHostRequirementsDetails) *models.ClusterHostRequirementsDetails {
	if rq == nil {
		return def
	}
	if def == nil {
		return rq
	}
	merged := *rq
	if merged.CPUCores == 0 {
		merged.CPUCores = def.CPUCores
	}
	if merged.RAMMib == 0 {
		merged.RAMMib = def.RAMMib
	}
	if merged.DiskSizeGb == 0 {
		merged.DiskSizeGb = def.DiskSizeGb
	}
	if merged.InstallationDiskSpeedThresholdMs == 0 {
		merged.InstallationDiskSpeedThresholdMs = def.InstallationDiskSpeedThresholdMs
	}
	if merged.NetworkLatencyThresholdMs == nil {
		merged.NetworkLatencyThresholdMs = def.NetworkLatencyThresholdMs
	}
	if merged.PacketLossPercentage == nil {
		merged.PacketLossPercentage = def.PacketLossPercentage
	}
	return &merged
}

func (d *VersionedRequirementsDecoder) validate() error {
	for version, requirements := range *d {
		err := validateDetails(requirements.WorkerRequirements, version, string(models.HostRoleWorker))
		if err != nil {
			return err
		}
		err = validateDetails(requirements.ArbiterRequirements, version, string(models.HostRoleArbiter))
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
		ArbiterRequirements:    copyClusterHostRequirementsDetails(requirements.ArbiterRequirements),
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
