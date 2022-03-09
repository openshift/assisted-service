package api

import (
	"context"

	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
)

//go:generate mockgen -package api -destination mock_manifests_api.go . ManifestsAPI
type ManifestsAPI interface {
	restapi.ManifestsAPI
	ClusterManifestsInternals
}

//go:generate mockgen -package api -destination mock_manifests_internal.go . ClusterManifestsInternals
type ClusterManifestsInternals interface {
	CreateClusterManifestInternal(ctx context.Context, params operations.V2CreateClusterManifestParams) (*models.Manifest, error)
	ListClusterManifestsInternal(ctx context.Context, params operations.V2ListClusterManifestsParams) (models.ListManifests, error)
	DeleteClusterManifestInternal(ctx context.Context, params operations.V2DeleteClusterManifestParams) error
}
