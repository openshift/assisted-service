package bminventory

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/transaction"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
)

func (b *bareMetalInventory) V2UpdateHost(ctx context.Context, params installer.V2UpdateHostParams) middleware.Responder {
	host, err := b.V2UpdateHostInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostCreated().WithPayload(&host.Host)
}

func (b *bareMetalInventory) V2RegisterCluster(ctx context.Context, params installer.V2RegisterClusterParams) middleware.Responder {
	c, err := b.RegisterClusterInternal(ctx, nil, params, false)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2RegisterClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2ListClusters(ctx context.Context, params installer.V2ListClustersParams) middleware.Responder {
	clusters, err := b.listClustersInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2ListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) V2GetCluster(ctx context.Context, params installer.V2GetClusterParams) middleware.Responder {
	c, err := b.GetClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2GetClusterOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2DeregisterCluster(ctx context.Context, params installer.V2DeregisterClusterParams) middleware.Responder {
	if err := b.DeregisterClusterInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2DeregisterClusterNoContent()
}

func (b *bareMetalInventory) V2GetClusterInstallConfig(ctx context.Context, params installer.V2GetClusterInstallConfigParams) middleware.Responder {
	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	cfg, err := b.installConfigBuilder.GetInstallConfig(c, false, "")
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewV2GetClusterInstallConfigOK().WithPayload(string(cfg))
}

func (b *bareMetalInventory) V2UpdateClusterInstallConfig(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) middleware.Responder {
	_, err := b.UpdateClusterInstallConfigInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateClusterInstallConfigCreated()
}

func (b *bareMetalInventory) V2InstallCluster(ctx context.Context, params installer.V2InstallClusterParams) middleware.Responder {
	c, err := b.InstallClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2InstallClusterAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2CancelInstallation(ctx context.Context, params installer.V2CancelInstallationParams) middleware.Responder {
	c, err := b.CancelInstallationInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2CancelInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2ResetCluster(ctx context.Context, params installer.V2ResetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("resetting cluster %s", params.ClusterID)

	var cluster *common.Cluster

	txSuccess := false
	tx := b.db.Begin()
	tx = transaction.AddForUpdateQueryOption(tx)
	defer func() {
		if !txSuccess {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewV2ResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	var err error
	if cluster, err = common.GetClusterFromDB(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return installer.NewV2ResetClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewV2ResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.clusterApi.ResetCluster(ctx, cluster, "cluster was reset by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}

	for _, h := range cluster.Hosts {
		if err := b.hostApi.ResetHost(ctx, h, "cluster was reset by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewV2ResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.DeleteClusterFiles(ctx, cluster, b.objectHandler); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err := b.deleteDNSRecordSets(ctx, *cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewV2ResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewV2ResetClusterAccepted().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) V2GetPreflightRequirements(ctx context.Context, params installer.V2GetPreflightRequirementsParams) middleware.Responder {
	cluster, err := b.getCluster(ctx, params.ClusterID.String(), common.UseEagerLoading)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	requirements, err := b.hwValidator.GetPreflightHardwareRequirements(ctx, cluster)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2GetPreflightRequirementsOK().WithPayload(requirements)
}

func (b *bareMetalInventory) V2UploadClusterIngressCert(ctx context.Context, params installer.V2UploadClusterIngressCertParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("UploadClusterIngressCert for cluster %s with params %s", params.ClusterID, params.IngressCertParams)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return installer.NewV2UploadClusterIngressCertNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewV2UploadClusterIngressCertInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.UploadIngressCert(&cluster); err != nil {
		return installer.NewV2UploadClusterIngressCertBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	objectName := fmt.Sprintf("%s/%s", cluster.ID, constants.Kubeconfig)
	exists, err := b.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload ingress ca")
		return installer.NewV2UploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if exists {
		log.Infof("Ingress ca for cluster %s already exists", cluster.ID)
		return installer.NewV2UploadClusterIngressCertCreated()
	}

	noingress := fmt.Sprintf("%s/%s-noingress", cluster.ID, constants.Kubeconfig)
	resp, _, err := b.objectHandler.Download(ctx, noingress)
	if err != nil {
		return installer.NewV2UploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	kubeconfigData, err := ioutil.ReadAll(resp)
	if err != nil {
		log.WithError(err).Infof("Failed to convert kubeconfig s3 response to io reader")
		return installer.NewV2UploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	mergedKubeConfig, err := mergeIngressCaIntoKubeconfig(kubeconfigData, []byte(params.IngressCertParams), log)
	if err != nil {
		return installer.NewV2UploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.objectHandler.Upload(ctx, mergedKubeConfig, objectName); err != nil {
		return installer.NewV2UploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.Errorf("failed to upload %s to s3", objectName)))
	}
	return installer.NewV2UploadClusterIngressCertCreated()
}

func (b *bareMetalInventory) V2CompleteInstallation(ctx context.Context, params installer.V2CompleteInstallationParams) middleware.Responder {
	// TODO: MGMT-4458
	// This function can be removed once the controller will stop sending this request
	// The service is already capable of completing the installation on its own

	log := logutil.FromContext(ctx, b.log)

	log.Infof("complete cluster %s installation", params.ClusterID)

	var cluster *common.Cluster
	var err error
	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}

	if !*params.CompletionParams.IsSuccess {
		if _, err := b.clusterApi.CompleteInstallation(ctx, b.db, cluster, false, params.CompletionParams.ErrorInfo); err != nil {
			log.WithError(err).Errorf("Failed to set complete cluster state on %s ", params.ClusterID.String())
			return common.GenerateErrorResponder(err)
		}
	} else {
		log.Warnf("Cluster %s tried to complete its installation using deprecated CompleteInstallation API. The service decides whether the cluster completed", params.ClusterID)
	}

	return installer.NewV2CompleteInstallationAccepted().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) V2UpdateClusterLogsProgress(ctx context.Context, params installer.V2UpdateClusterLogsProgressParams) middleware.Responder {
	var err error
	var currentCluster *common.Cluster

	log := logutil.FromContext(ctx, b.log)
	log.Infof("update log progress on %s cluster to %s", params.ClusterID, params.LogsProgressParams.LogsState)
	currentCluster, err = b.getCluster(ctx, params.ClusterID.String())
	if err == nil {
		err = b.clusterApi.UpdateLogsProgress(ctx, currentCluster, string(params.LogsProgressParams.LogsState))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on cluster %s", params.LogsProgressParams.LogsState, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return installer.NewV2UpdateClusterLogsProgressNoContent()
}
