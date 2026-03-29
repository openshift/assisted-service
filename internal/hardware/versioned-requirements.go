package hardware

import (
	"encoding/json"
	"fmt"
	"sort"

	goversion "github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/models"
)

const DefaultVersion = "default"

type partialRoleRequirements struct {
	CPUCores                         *int64   `json:"cpu_cores,omitempty"`
	RAMMib                           *int64   `json:"ram_mib,omitempty"`
	DiskSizeGb                       *int64   `json:"disk_size_gb,omitempty"`
	InstallationDiskSpeedThresholdMs *int64   `json:"installation_disk_speed_threshold_ms,omitempty"`
	NetworkLatencyThresholdMs        *float64 `json:"network_latency_threshold_ms,omitempty"`
	PacketLossPercentage             *float64 `json:"packet_loss_percentage,omitempty"`
}

type rawRequirementsEntry struct {
	Version                string                   `json:"version,omitempty"`
	MinVersion             string                   `json:"min_version,omitempty"`
	MasterRequirements     *partialRoleRequirements `json:"master,omitempty"`
	ArbiterRequirements    *partialRoleRequirements `json:"arbiter,omitempty"`
	WorkerRequirements     *partialRoleRequirements `json:"worker,omitempty"`
	SNORequirements        *partialRoleRequirements `json:"sno,omitempty"`
	EdgeWorkerRequirements *partialRoleRequirements `json:"edge-worker,omitempty"`
}

type resolvedMinVersion struct {
	version      *goversion.Version
	requirements models.VersionedHostRequirements
}

// VersionedRequirementsDecoder holds exact-version and min-version hardware requirements.
type VersionedRequirementsDecoder struct {
	versions    map[string]models.VersionedHostRequirements
	minVersions []resolvedMinVersion // sorted ascending by version
}

// GetVersionedHostRequirements returns requirements for the given OCP version.
// Lookup order: exact version match → highest min_version ≤ requested → "default".
func (d *VersionedRequirementsDecoder) GetVersionedHostRequirements(version string) (*models.VersionedHostRequirements, error) {
	if req, ok := d.versions[version]; ok {
		return copyVersionedHostRequirements(&req), nil
	}

	if len(d.minVersions) > 0 {
		requestedV, err := goversion.NewVersion(version)
		if err == nil {
			for i := len(d.minVersions) - 1; i >= 0; i-- {
				if d.minVersions[i].version.LessThanOrEqual(requestedV) {
					return copyVersionedHostRequirements(&d.minVersions[i].requirements), nil
				}
			}
		}
	}

	if req, ok := d.versions[DefaultVersion]; ok {
		return copyVersionedHostRequirements(&req), nil
	}
	return nil, fmt.Errorf("requirements for version %v not found", version)
}

func (d *VersionedRequirementsDecoder) Decode(value string) error {
	var entries []rawRequirementsEntry
	if err := json.Unmarshal([]byte(value), &entries); err != nil {
		return err
	}

	versions := make(map[string]models.VersionedHostRequirements)
	var rawMinVersions []rawRequirementsEntry

	seenMinVersions := make(map[string]struct{})
	for _, entry := range entries {
		switch {
		case entry.Version != "" && entry.MinVersion != "":
			return fmt.Errorf("entry must specify either \"version\" or \"min_version\", not both")
		case entry.Version == "" && entry.MinVersion == "":
			return fmt.Errorf("entry must specify either \"version\" or \"min_version\"")
		case entry.Version != "":
			if _, exists := versions[entry.Version]; exists {
				return fmt.Errorf("duplicate version entry %q", entry.Version)
			}
			req, err := toStrictVersionedRequirements(entry)
			if err != nil {
				return err
			}
			versions[entry.Version] = req
		case entry.MinVersion != "":
			if _, exists := seenMinVersions[entry.MinVersion]; exists {
				return fmt.Errorf("duplicate min_version entry %q", entry.MinVersion)
			}
			seenMinVersions[entry.MinVersion] = struct{}{}
			rawMinVersions = append(rawMinVersions, entry)
		}
	}

	d.versions = versions
	if err := d.validateVersionEntries(); err != nil {
		return err
	}

	var minVersions []resolvedMinVersion
	if len(rawMinVersions) > 0 {
		defaultReq, hasDefault := versions[DefaultVersion]
		if !hasDefault {
			return fmt.Errorf("a \"default\" version entry is required when min_version entries are present")
		}
		for _, raw := range rawMinVersions {
			v, err := goversion.NewVersion(raw.MinVersion)
			if err != nil {
				return fmt.Errorf("invalid min_version %q: %w", raw.MinVersion, err)
			}
			merged := mergePartialWithDefault(raw, defaultReq)
			if err := validateVersionedRequirements(merged, raw.MinVersion); err != nil {
				return err
			}
			minVersions = append(minVersions, resolvedMinVersion{
				version:      v,
				requirements: merged,
			})
		}
		sort.Slice(minVersions, func(i, j int) bool {
			return minVersions[i].version.LessThan(minVersions[j].version)
		})
	}

	d.minVersions = minVersions
	return nil
}

func (d *VersionedRequirementsDecoder) validateVersionEntries() error {
	for version, req := range d.versions {
		if err := validateVersionedRequirements(req, version); err != nil {
			return err
		}
	}
	return nil
}

func validateVersionedRequirements(req models.VersionedHostRequirements, label string) error {
	for _, check := range []struct {
		details *models.ClusterHostRequirementsDetails
		role    string
	}{
		{req.MasterRequirements, string(models.HostRoleMaster)},
		{req.ArbiterRequirements, string(models.HostRoleArbiter)},
		{req.WorkerRequirements, string(models.HostRoleWorker)},
		{req.SNORequirements, "SNO"},
		{req.EdgeWorkerRequirements, "EDGE-WORKER"},
	} {
		if err := validateDetails(check.details, label, check.role); err != nil {
			return err
		}
	}
	return nil
}

// toStrictVersionedRequirements converts a version entry requiring all required roles to be present and valid.
// arbiter and edge-worker are optional and fall back to worker if not specified.
func toStrictVersionedRequirements(entry rawRequirementsEntry) (models.VersionedHostRequirements, error) {
	master, err := toStrictRoleDetails(entry.MasterRequirements, entry.Version, string(models.HostRoleMaster))
	if err != nil {
		return models.VersionedHostRequirements{}, err
	}
	worker, err := toStrictRoleDetails(entry.WorkerRequirements, entry.Version, string(models.HostRoleWorker))
	if err != nil {
		return models.VersionedHostRequirements{}, err
	}
	sno, err := toStrictRoleDetails(entry.SNORequirements, entry.Version, "SNO")
	if err != nil {
		return models.VersionedHostRequirements{}, err
	}
	arbiter, err := toStrictRoleDetails(entry.ArbiterRequirements, entry.Version, string(models.HostRoleArbiter))
	if err != nil {
		return models.VersionedHostRequirements{}, err
	}
	edgeWorker, err := toStrictRoleDetails(entry.EdgeWorkerRequirements, entry.Version, "EDGE-WORKER")
	if err != nil {
		return models.VersionedHostRequirements{}, err
	}

	req := models.VersionedHostRequirements{
		Version:                entry.Version,
		MasterRequirements:     master,
		WorkerRequirements:     worker,
		SNORequirements:        sno,
		ArbiterRequirements:    arbiter,
		EdgeWorkerRequirements: edgeWorker,
	}
	// in case we don't set edge worker requirements, handle it as regular worker
	if req.EdgeWorkerRequirements == nil {
		req.EdgeWorkerRequirements = req.WorkerRequirements
	}
	// This is only until we add arbiter to HW_VALIDATOR_REQUIREMENTS in all environments
	if req.ArbiterRequirements == nil {
		req.ArbiterRequirements = req.WorkerRequirements
	}
	return req, nil
}

// toStrictRoleDetails converts a partialRoleRequirements to ClusterHostRequirementsDetails,
// returning nil if partial is nil, or an error if any required field is missing or invalid.
func toStrictRoleDetails(partial *partialRoleRequirements, version, role string) (*models.ClusterHostRequirementsDetails, error) {
	if partial == nil {
		return nil, nil
	}
	if partial.CPUCores == nil || *partial.CPUCores <= 0 {
		return nil, fmt.Errorf("CPU cores requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if partial.RAMMib == nil || *partial.RAMMib <= 0 {
		return nil, fmt.Errorf("RAM requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if partial.DiskSizeGb == nil || *partial.DiskSizeGb <= 0 {
		return nil, fmt.Errorf("disk size requirement must be greater than 0 for version %v and %v role", version, role)
	}
	if partial.InstallationDiskSpeedThresholdMs != nil && *partial.InstallationDiskSpeedThresholdMs < 0 {
		return nil, fmt.Errorf("installation disk speed threshold must not be negative for version %v and %v role", version, role)
	}
	details := &models.ClusterHostRequirementsDetails{
		CPUCores:                  *partial.CPUCores,
		RAMMib:                    *partial.RAMMib,
		DiskSizeGb:                *partial.DiskSizeGb,
		NetworkLatencyThresholdMs: partial.NetworkLatencyThresholdMs,
		PacketLossPercentage:      partial.PacketLossPercentage,
	}
	if partial.InstallationDiskSpeedThresholdMs != nil {
		details.InstallationDiskSpeedThresholdMs = *partial.InstallationDiskSpeedThresholdMs
	}
	return details, nil
}

// mergePartialWithDefault merges a min_version entry's partial fields with the default requirements.
// Nil roles are taken entirely from default; present roles are merged field-by-field.
func mergePartialWithDefault(entry rawRequirementsEntry, def models.VersionedHostRequirements) models.VersionedHostRequirements {
	return models.VersionedHostRequirements{
		Version:                entry.MinVersion,
		MasterRequirements:     mergePartialRole(entry.MasterRequirements, def.MasterRequirements),
		ArbiterRequirements:    mergePartialRole(entry.ArbiterRequirements, def.ArbiterRequirements),
		WorkerRequirements:     mergePartialRole(entry.WorkerRequirements, def.WorkerRequirements),
		SNORequirements:        mergePartialRole(entry.SNORequirements, def.SNORequirements),
		EdgeWorkerRequirements: mergePartialRole(entry.EdgeWorkerRequirements, def.EdgeWorkerRequirements),
	}
}

// mergePartialRole merges a partial role with a base role.
// Non-nil pointer fields in partial override the corresponding field in base.
func mergePartialRole(partial *partialRoleRequirements, base *models.ClusterHostRequirementsDetails) *models.ClusterHostRequirementsDetails {
	if partial == nil {
		return copyClusterHostRequirementsDetails(base)
	}
	var result models.ClusterHostRequirementsDetails
	if base != nil {
		result = *base
	}
	if partial.CPUCores != nil {
		result.CPUCores = *partial.CPUCores
	}
	if partial.RAMMib != nil {
		result.RAMMib = *partial.RAMMib
	}
	if partial.DiskSizeGb != nil {
		result.DiskSizeGb = *partial.DiskSizeGb
	}
	if partial.InstallationDiskSpeedThresholdMs != nil {
		result.InstallationDiskSpeedThresholdMs = *partial.InstallationDiskSpeedThresholdMs
	}
	if partial.NetworkLatencyThresholdMs != nil {
		result.NetworkLatencyThresholdMs = partial.NetworkLatencyThresholdMs
	}
	if partial.PacketLossPercentage != nil {
		result.PacketLossPercentage = partial.PacketLossPercentage
	}
	return &result
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
		return fmt.Errorf("installation disk speed threshold must not be negative for version %v and %v role", version, role)
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

// NewVersionedRequirementsDecoderFromMap constructs a VersionedRequirementsDecoder directly from a versions map.
// Intended for use in tests that construct requirements programmatically.
func NewVersionedRequirementsDecoderFromMap(versions map[string]models.VersionedHostRequirements) VersionedRequirementsDecoder {
	return VersionedRequirementsDecoder{versions: versions}
}

func copyClusterHostRequirementsDetails(details *models.ClusterHostRequirementsDetails) *models.ClusterHostRequirementsDetails {
	if details == nil {
		return nil
	}
	return &models.ClusterHostRequirementsDetails{
		CPUCores:                         details.CPUCores,
		DiskSizeGb:                       details.DiskSizeGb,
		InstallationDiskSpeedThresholdMs: details.InstallationDiskSpeedThresholdMs,
		RAMMib:                           details.RAMMib,
		NetworkLatencyThresholdMs:        details.NetworkLatencyThresholdMs,
		PacketLossPercentage:             details.PacketLossPercentage,
	}
}
