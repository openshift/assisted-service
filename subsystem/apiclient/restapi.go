package apiclient

import (
	"context"

	inventory_client "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

type RestAPIClient struct {
	ai *inventory_client.AssistedInstall
}

func NewRestAPIClient(ai *inventory_client.AssistedInstall) (*RestAPIClient, error) {
	return &RestAPIClient{ai: ai}, nil
}

func (rc *RestAPIClient) RegisterCluster(
	ctx context.Context,
	params *installer.RegisterClusterParams,
) (*models.Cluster, error) {

	res, err := rc.ai.Installer.RegisterCluster(ctx, params)
	if err != nil {
		return nil, err
	}
	cluster := res.GetPayload()
	return cluster, nil
}

func (rc *RestAPIClient) DeregisterCluster(
	ctx context.Context,
	params *installer.DeregisterClusterParams,
) error {

	_, err := rc.ai.Installer.DeregisterCluster(ctx, params)
	if err != nil {
		return err
	}
	return nil
}

func (rc *RestAPIClient) UpdateCluster(
	ctx context.Context,
	params *installer.UpdateClusterParams,
) (*models.Cluster, error) {

	res, err := rc.ai.Installer.UpdateCluster(ctx, params)
	if err != nil {
		return nil, err
	}
	cluster := res.GetPayload()
	return cluster, err
}
