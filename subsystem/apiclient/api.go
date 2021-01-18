package apiclient

import (
	"context"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

type APIClient interface {
	RegisterCluster(
		ctx context.Context,
		params *installer.RegisterClusterParams,
	) (*models.Cluster, error)

	UpdateCluster(
		ctx context.Context,
		params *installer.UpdateClusterParams,
	) (*models.Cluster, error)

	DeregisterCluster(
		ctx context.Context,
		params *installer.DeregisterClusterParams,
	) error
}
