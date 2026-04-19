package hardware

import (
	"encoding/json"
	"fmt"
	"sort"

	goversion "github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

const (
	DefaultVersion      = "default"
	MatchTypeExact      = "exact"
	MatchTypeMinVersion = "min_version"
)

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

	for i := len(d.minVersions) - 1; i >= 0; i-- {
		if isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual(d.minVersions[i].requirements.Version, version); err == nil && isGreaterOrEqual {
			return copyVersionedHostRequirements(&d.minVersions[i].requirements), nil
		}
	}

	if req, ok := d.versions[DefaultVersion]; ok {
		return copyVersionedHostRequirements(&req), nil
	}
	return nil, fmt.Errorf("requirements for version %v not found", version)
}

func (d *VersionedRequirementsDecoder) Decode(value string) error {
	var entries []models.VersionedHostRequirements
	if err := json.Unmarshal([]byte(value), &entries); err != nil {
		return err
	}

	var (
		defaultEntry      *models.VersionedHostRequirements
		exactEntries      []models.VersionedHostRequirements
		minVersionEntries []models.VersionedHostRequirements
	)
	seenExact := make(map[string]struct{})
	seenMin := make(map[string]struct{})

	for i := range entries {
		entry := &entries[i]
		if entry.Version == "" {
			return fmt.Errorf("entry must specify \"version\"")
		}
		switch entry.MatchType {
		case "", MatchTypeExact, MatchTypeMinVersion:
		default:
			return fmt.Errorf("invalid match_type %q for version %q, must be %q or %q", entry.MatchType, entry.Version, MatchTypeExact, MatchTypeMinVersion)
		}
		if entry.MatchType == MatchTypeMinVersion {
			if _, exists := seenMin[entry.Version]; exists {
				return fmt.Errorf("duplicate min_version entry %q", entry.Version)
			}
			seenMin[entry.Version] = struct{}{}
			minVersionEntries = append(minVersionEntries, *entry)
			continue
		}
		if _, exists := seenExact[entry.Version]; exists {
			return fmt.Errorf("duplicate version entry %q", entry.Version)
		}
		seenExact[entry.Version] = struct{}{}
		if entry.Version == DefaultVersion {
			defaultEntry = entry
			continue
		}
		exactEntries = append(exactEntries, *entry)
	}

	versions := make(map[string]models.VersionedHostRequirements)
	if defaultEntry != nil {
		versions[DefaultVersion] = applyRoleFallbacks(*defaultEntry)
	}

	if err := validateVersionsMap(versions); err != nil {
		return err
	}

	defaultReq, hasDefault := versions[DefaultVersion]

	for _, entry := range exactEntries {
		merged := applyRoleFallbacks(entry)
		if hasDefault {
			merged = mergeWithDefault(entry, defaultReq)
		}
		if err := validateVersionedRequirements(merged, entry.Version); err != nil {
			return err
		}
		versions[entry.Version] = merged
	}

	var minVersions []resolvedMinVersion
	if len(minVersionEntries) > 0 {
		if !hasDefault {
			return fmt.Errorf("a \"default\" version entry is required when min_version entries are present")
		}
		for _, entry := range minVersionEntries {
			v, err := goversion.NewVersion(entry.Version)
			if err != nil {
				return fmt.Errorf("invalid min_version %q: %w", entry.Version, err)
			}
			merged := mergeWithDefault(entry, defaultReq)
			if err := validateVersionedRequirements(merged, entry.Version); err != nil {
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

	d.versions = versions
	return nil
}

func validateVersionsMap(versions map[string]models.VersionedHostRequirements) error {
	for version, req := range versions {
		if err := validateVersionedRequirements(req, version); err != nil {
			return err
		}
	}
	return nil
}

func validateVersionedRequirements(req models.VersionedHostRequirements, label string) error {
	for _, check := range []struct {
		role *models.VersionedClusterHostRequirementsDetails
		name string
	}{
		{req.MasterRequirements, string(models.HostRoleMaster)},
		{req.ArbiterRequirements, string(models.HostRoleArbiter)},
		{req.WorkerRequirements, string(models.HostRoleWorker)},
		{req.SNORequirements, "SNO"},
		{req.EdgeWorkerRequirements, "EDGE-WORKER"},
	} {
		if err := validateDetails(toClusterHostRequirementsDetails(check.role), label, check.name); err != nil {
			return err
		}
	}
	return nil
}

// applyRoleFallbacks applies worker-based fallbacks for optional roles not specified in an entry.
// arbiter and edge-worker fall back to worker if not set.
func applyRoleFallbacks(entry models.VersionedHostRequirements) models.VersionedHostRequirements {
	// in case we don't set edge worker requirements, handle it as regular worker
	if entry.EdgeWorkerRequirements == nil {
		entry.EdgeWorkerRequirements = entry.WorkerRequirements
	}
	// This is only until we add arbiter to HW_VALIDATOR_REQUIREMENTS in all environments
	if entry.ArbiterRequirements == nil {
		entry.ArbiterRequirements = entry.WorkerRequirements
	}
	return entry
}

// mergeWithDefault merges a non-default entry with the default requirements.
// Nil roles are taken from default; present roles are merged field-by-field with default.
func mergeWithDefault(entry models.VersionedHostRequirements, def models.VersionedHostRequirements) models.VersionedHostRequirements {
	return models.VersionedHostRequirements{
		Version:                entry.Version,
		MasterRequirements:     mergeRole(entry.MasterRequirements, def.MasterRequirements),
		ArbiterRequirements:    mergeRole(entry.ArbiterRequirements, def.ArbiterRequirements),
		WorkerRequirements:     mergeRole(entry.WorkerRequirements, def.WorkerRequirements),
		SNORequirements:        mergeRole(entry.SNORequirements, def.SNORequirements),
		EdgeWorkerRequirements: mergeRole(entry.EdgeWorkerRequirements, def.EdgeWorkerRequirements),
	}
}

// mergeRole merges a partial role with a base role.
// Non-nil pointer fields in partial override the corresponding field in base.
func mergeRole(partial, base *models.VersionedClusterHostRequirementsDetails) *models.VersionedClusterHostRequirementsDetails {
	if partial == nil {
		return copyVersionedClusterHostRequirementsDetails(base)
	}
	var result models.VersionedClusterHostRequirementsDetails
	if base != nil {
		result = *base
	}
	if partial.CPUCores != nil {
		result.CPUCores = copyInt64Ptr(partial.CPUCores)
	}
	if partial.RAMMib != nil {
		result.RAMMib = copyInt64Ptr(partial.RAMMib)
	}
	if partial.DiskSizeGb != nil {
		result.DiskSizeGb = copyInt64Ptr(partial.DiskSizeGb)
	}
	if partial.InstallationDiskSpeedThresholdMs != nil {
		result.InstallationDiskSpeedThresholdMs = copyInt64Ptr(partial.InstallationDiskSpeedThresholdMs)
	}
	if partial.NetworkLatencyThresholdMs != nil {
		result.NetworkLatencyThresholdMs = partial.NetworkLatencyThresholdMs
	}
	if partial.PacketLossPercentage != nil {
		result.PacketLossPercentage = partial.PacketLossPercentage
	}
	return &result
}

// toClusterHostRequirementsDetails converts a versioned role to the API details type.
// Nil pointer fields convert to zero values.
func toClusterHostRequirementsDetails(role *models.VersionedClusterHostRequirementsDetails) *models.ClusterHostRequirementsDetails {
	if role == nil {
		return nil
	}
	details := &models.ClusterHostRequirementsDetails{
		NetworkLatencyThresholdMs: role.NetworkLatencyThresholdMs,
		PacketLossPercentage:      role.PacketLossPercentage,
	}
	if role.CPUCores != nil {
		details.CPUCores = *role.CPUCores
	}
	if role.RAMMib != nil {
		details.RAMMib = *role.RAMMib
	}
	if role.DiskSizeGb != nil {
		details.DiskSizeGb = *role.DiskSizeGb
	}
	if role.InstallationDiskSpeedThresholdMs != nil {
		details.InstallationDiskSpeedThresholdMs = *role.InstallationDiskSpeedThresholdMs
	}
	return details
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

func copyVersionedHostRequirements(req *models.VersionedHostRequirements) *models.VersionedHostRequirements {
	return &models.VersionedHostRequirements{
		Version:                req.Version,
		MasterRequirements:     copyVersionedClusterHostRequirementsDetails(req.MasterRequirements),
		ArbiterRequirements:    copyVersionedClusterHostRequirementsDetails(req.ArbiterRequirements),
		WorkerRequirements:     copyVersionedClusterHostRequirementsDetails(req.WorkerRequirements),
		SNORequirements:        copyVersionedClusterHostRequirementsDetails(req.SNORequirements),
		EdgeWorkerRequirements: copyVersionedClusterHostRequirementsDetails(req.EdgeWorkerRequirements),
	}
}

func copyVersionedClusterHostRequirementsDetails(role *models.VersionedClusterHostRequirementsDetails) *models.VersionedClusterHostRequirementsDetails {
	if role == nil {
		return nil
	}
	return &models.VersionedClusterHostRequirementsDetails{
		CPUCores:                         copyInt64Ptr(role.CPUCores),
		RAMMib:                           copyInt64Ptr(role.RAMMib),
		DiskSizeGb:                       copyInt64Ptr(role.DiskSizeGb),
		InstallationDiskSpeedThresholdMs: copyInt64Ptr(role.InstallationDiskSpeedThresholdMs),
		NetworkLatencyThresholdMs:        role.NetworkLatencyThresholdMs,
		PacketLossPercentage:             role.PacketLossPercentage,
	}
}

func copyInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// NewVersionedRequirementsDecoderFromMap constructs a VersionedRequirementsDecoder directly from a versions map.
// Intended for use in tests that construct requirements programmatically.
func NewVersionedRequirementsDecoderFromMap(versions map[string]models.VersionedHostRequirements) VersionedRequirementsDecoder {
	return VersionedRequirementsDecoder{versions: versions}
}
