package agentbasedinstaller

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	errorutil "github.com/openshift/assisted-service/pkg/error"
	log "github.com/sirupsen/logrus"
)

func ImportCluster(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall,
	clusterID strfmt.UUID, clusterName string, clusterAPIVIPDNSName string, clusterConfigDir string) (*models.Cluster, error) {

	importClusterParams := &models.ImportClusterParams{
		Name:               &clusterName,
		APIVipDnsname:      &clusterAPIVIPDNSName,
		OpenshiftClusterID: &clusterID,
	}
	importParams := &installer.V2ImportClusterParams{
		NewImportClusterParams: importClusterParams,
	}
	clusterResult, importClusterErr := bmInventory.Installer.V2ImportCluster(ctx, importParams)
	if importClusterErr != nil {
		return nil, errorutil.GetAssistedError(importClusterErr)
	}
	log.Infof("Imported cluster with id: %s", clusterResult.Payload.ID)

	if err := configureClusterIgnitionEndpoint(ctx, bmInventory, clusterConfigDir, clusterResult.Payload.ID); err != nil {
		return nil, err
	}

	return clusterResult.GetPayload(), nil
}

func configureClusterIgnitionEndpoint(ctx context.Context, bmInventory *client.AssistedInstall, clusterConfigDir string, clusterID *strfmt.UUID) error {
	workerIgnitionRaw, err := os.ReadFile(path.Join(clusterConfigDir, "worker-ignition-endpoint.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read ignition endpoint file: %w", err)
	}

	ignitionEndpoint := &models.IgnitionEndpoint{}
	err = ignitionEndpoint.UnmarshalBinary(workerIgnitionRaw)
	if err != nil {
		return fmt.Errorf("failed to unmarshal worker ignition endpoint: %w", err)
	}
	log.Info("Read worker ignition endpoint file")

	updateClusterParams := &models.V2ClusterUpdateParams{
		IgnitionEndpoint: ignitionEndpoint,
	}
	updateParams := &installer.V2UpdateClusterParams{
		ClusterID:           *clusterID,
		ClusterUpdateParams: updateClusterParams,
	}
	_, updateClusterErr := bmInventory.Installer.V2UpdateCluster(ctx, updateParams)
	if updateClusterErr != nil {
		return errorutil.GetAssistedError(updateClusterErr)
	}
	log.Infof("Configured ignition endpoint for cluster %s", *clusterID)

	return nil
}
