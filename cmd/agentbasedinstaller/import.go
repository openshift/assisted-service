package agentbasedinstaller

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	errorutil "github.com/openshift/assisted-service/pkg/error"
	log "github.com/sirupsen/logrus"
)

func ImportCluster(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall,
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
	result := clusterResult.GetPayload()

	log.Infof("Imported cluster with id: %s", clusterResult.Payload.ID)

	return result, nil
}
