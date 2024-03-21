package api

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type ValidationStatus string

const (
	Success ValidationStatus = "success"
	Failure ValidationStatus = "failure"
	Pending ValidationStatus = "pending"
)

// ValidationResult hold result of operator validation
type ValidationResult struct {
	// ValidationId is an id of the validation
	ValidationId string
	// Status specifies the status of the validation: success, failure or pending
	Status ValidationStatus
	// Reasons hold list of reasons of a validation failure
	Reasons []string
}

// Operator provides generic API of an OLM operator installation plugin
//
//go:generate mockgen --build_flags=--mod=mod -package=api -self_package=github.com/openshift/assisted-service/internal/operators/api -destination=mock_operator_api.go . Operator
type Operator interface {
	// GetName reports the name of an operator this Operator manages
	GetName() string
	// GetFullName reports the full name of the specified Operator
	GetFullName() string
	// GetDependencies provides a list of dependencies of the Operator
	GetDependencies(cluster *common.Cluster) ([]string, error)
	// ValidateCluster verifies whether this operator is valid for given cluster
	ValidateCluster(ctx context.Context, cluster *common.Cluster) (ValidationResult, error)
	// ValidateHost verifies whether this operator is valid for given host
	ValidateHost(ctx context.Context, cluster *common.Cluster, hosts *models.Host) (ValidationResult, error)
	// GenerateManifests generates manifests for the operator
	GenerateManifests(*common.Cluster) (map[string][]byte, []byte, error)
	// GetHostRequirements provides operator's requirements towards the host
	GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error)
	// GetClusterValidationID returns cluster validation ID for the Operator
	GetClusterValidationID() string
	// GetHostValidationID returns host validation ID for the Operator
	GetHostValidationID() string
	// GetProperties provides description of operator properties
	GetProperties() models.OperatorProperties
	// GetMonitoredOperator returns MonitoredOperator corresponding to the Operator implementation
	GetMonitoredOperator() *models.MonitoredOperator
	// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
	GetPreflightRequirements(ctx context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error)
	// GetFeatureSupportID returns the operator unique feature-support ID
	GetFeatureSupportID() models.FeatureSupportLevelID
}

// Storage Operator provide a generic API for storage operators
type StorageOperator interface {
	Operator
	StorageClassName() string
	SetAdditionalDiskRequirements(additionalSizeGB int64)
}
