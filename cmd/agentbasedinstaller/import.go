package agentbasedinstaller

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	json "github.com/bytedance/sonic"
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	errorutil "github.com/openshift/assisted-service/pkg/error"
	log "github.com/sirupsen/logrus"
)

const (
	workerIgnitionEndpointFilename = "worker-ignition-endpoint.json"
	importClusterConfigFilename    = "import-cluster-config.json"
)

func ImportCluster(fsys fs.FS, ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall,
	clusterID strfmt.UUID, clusterName string, clusterAPIVIPDNSName string) (*models.Cluster, error) {

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

	if err := updateClusterConfiguration(fsys, ctx, bmInventory, clusterResult.Payload.ID); err != nil {
		return nil, err
	}

	return clusterResult.GetPayload(), nil
}

func updateClusterConfiguration(fsys fs.FS, ctx context.Context, bmInventory *client.AssistedInstall, clusterID *strfmt.UUID) error {
	updateClusterParams := &models.V2ClusterUpdateParams{}
	err := configureClusterParams(fsys, updateClusterParams)
	if err != nil {
		return fmt.Errorf("failed to apply cluster params: %w", err)
	}

	err = configureClusterIgnitionEndpoint(fsys, updateClusterParams)
	if err != nil {
		return fmt.Errorf("failed to apply cluster ignition endpoint: %w", err)
	}

	updateParams := &installer.V2UpdateClusterParams{
		ClusterID:           *clusterID,
		ClusterUpdateParams: updateClusterParams,
	}
	_, updateClusterErr := bmInventory.Installer.V2UpdateCluster(ctx, updateParams)
	if updateClusterErr != nil {
		return errorutil.GetAssistedError(updateClusterErr)
	}
	log.Infof("Configuration updated for imported cluster with ID: %s", *clusterID)
	return nil
}

type Networking struct {
	UserManagedNetworking *bool `json:"userManagedNetworking,omitempty"`
}
type ClusterConfig struct {
	Networking Networking `json:"networking"`
}

func configureClusterParams(fsys fs.FS, params *models.V2ClusterUpdateParams) error {
	importClusterConfigRaw, err := fs.ReadFile(fsys, importClusterConfigFilename)
	if err != nil {
		return err
	}

	clusterConfig := &ClusterConfig{}
	err = json.Unmarshal(importClusterConfigRaw, &clusterConfig)
	if err != nil {
		return fmt.Errorf("failed to unmarshal %s file: %w", importClusterConfigFilename, err)
	}
	log.Info("Read import cluster config file")

	params.UserManagedNetworking = clusterConfig.Networking.UserManagedNetworking
	return nil
}

func configureClusterIgnitionEndpoint(fsys fs.FS, params *models.V2ClusterUpdateParams) error {
	workerIgnitionRaw, err := fs.ReadFile(fsys, workerIgnitionEndpointFilename)
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

	params.IgnitionEndpoint = ignitionEndpoint
	return nil
}
