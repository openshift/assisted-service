package hardware

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

//go:generate mockgen -source=requirements.go -package=hardware -destination=mock_requirements.go
type RequirementsProvider interface {
	GetOCPRequirementsForVersion(cluster *common.Cluster) (*models.VersionedHostRequirements, error)
}
