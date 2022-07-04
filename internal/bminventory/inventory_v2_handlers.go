package bminventory

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/imageservice"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func (b *bareMetalInventory) V2UpdateHost(ctx context.Context, params installer.V2UpdateHostParams) middleware.Responder {
	host, err := b.V2UpdateHostInternal(ctx, params, Interactive)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostCreated().WithPayload(&host.Host)
}

func (b *bareMetalInventory) V2RegisterCluster(ctx context.Context, params installer.V2RegisterClusterParams) middleware.Responder {
	c, err := b.RegisterClusterInternal(ctx, nil, params)
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

func (b *bareMetalInventory) TransformClusterToDay2(ctx context.Context, params installer.TransformClusterToDay2Params) middleware.Responder {
	c, err := b.TransformClusterToDay2Internal(ctx, params.ClusterID)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewTransformClusterToDay2Accepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2ResetCluster(ctx context.Context, params installer.V2ResetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("resetting cluster %s", params.ClusterID)

	var cluster *common.Cluster

	txSuccess := false
	tx := b.db.Begin()
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
	if cluster, err = common.GetClusterFromDBForUpdate(tx, params.ClusterID, common.UseEagerLoading); err != nil {
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
		if err := b.customizeHost(&cluster.Cluster, h); err != nil {
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
	log.Infof("update log progress on %s cluster to %s", params.ClusterID, common.LogStateValue(params.LogsProgressParams.LogsState))
	currentCluster, err = b.getCluster(ctx, params.ClusterID.String())
	if err == nil {
		err = b.clusterApi.UpdateLogsProgress(ctx, currentCluster, string(common.LogStateValue(params.LogsProgressParams.LogsState)))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on cluster %s", common.LogStateValue(params.LogsProgressParams.LogsState), params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return installer.NewV2UpdateClusterLogsProgressNoContent()
}

func (b *bareMetalInventory) V2GetClusterDefaultConfig(_ context.Context, _ installer.V2GetClusterDefaultConfigParams) middleware.Responder {
	body := &models.ClusterDefaultConfig{}

	body.NtpSource = b.Config.DefaultNTPSource
	body.InactiveDeletionHours = int64(b.gcConfig.DeregisterInactiveAfter.Hours())

	// TODO(MGMT-9751-remove-single-network)
	body.ClusterNetworkCidr = b.Config.DefaultClusterNetworkCidr
	body.ServiceNetworkCidr = b.Config.DefaultServiceNetworkCidr
	body.ClusterNetworkHostPrefix = b.Config.DefaultClusterNetworkHostPrefix

	body.ClusterNetworksIPV4 = []*models.ClusterNetwork{
		{
			Cidr:       models.Subnet(b.Config.DefaultClusterNetworkCidr),
			HostPrefix: b.Config.DefaultClusterNetworkHostPrefix,
		},
	}
	body.ServiceNetworksIPV4 = []*models.ServiceNetwork{
		{Cidr: models.Subnet(b.Config.DefaultServiceNetworkCidr)},
	}

	body.ClusterNetworksDualstack = []*models.ClusterNetwork{
		{
			Cidr:       models.Subnet(b.Config.DefaultClusterNetworkCidr),
			HostPrefix: b.Config.DefaultClusterNetworkHostPrefix,
		},
		{
			Cidr:       models.Subnet(b.Config.DefaultClusterNetworkCidrIPv6),
			HostPrefix: b.Config.DefaultClusterNetworkHostPrefixIPv6,
		},
	}
	body.ServiceNetworksDualstack = []*models.ServiceNetwork{
		{Cidr: models.Subnet(b.Config.DefaultServiceNetworkCidr)},
		{Cidr: models.Subnet(b.Config.DefaultServiceNetworkCidrIPv6)},
	}

	body.ForbiddenHostnames = append(body.ForbiddenHostnames, hostutil.ForbiddenHostnames...)

	return installer.NewV2GetClusterDefaultConfigOK().WithPayload(body)
}

func (b *bareMetalInventory) V2DownloadClusterLogs(ctx context.Context, params installer.V2DownloadClusterLogsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Downloading logs from cluster %s", params.ClusterID)
	fileName, downloadFileName, err := b.getLogFileForDownload(ctx, &params.ClusterID, params.HostID, swag.StringValue(params.LogsType))
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	respBody, contentLength, err := b.objectHandler.Download(ctx, fileName)
	if err != nil {
		if _, ok := err.(common.NotFound); ok {
			log.WithError(err).Warnf("File not found %s", fileName)
			return common.NewApiError(http.StatusNotFound, errors.Errorf("Logs of type %s for cluster %s "+
				"were not found", swag.StringValue(params.LogsType), params.ClusterID))
		}
		log.WithError(err).Errorf("failed to download file %s", fileName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return filemiddleware.NewResponder(installer.NewV2DownloadClusterLogsOK().WithPayload(respBody), downloadFileName, contentLength, nil)
}

func (b *bareMetalInventory) V2UploadLogs(ctx context.Context, params installer.V2UploadLogsParams) middleware.Responder {
	err := b.v2uploadLogs(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UploadLogsNoContent()
}

func (b *bareMetalInventory) v2uploadLogs(ctx context.Context, params installer.V2UploadLogsParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Uploading logs from cluster %s", params.ClusterID)

	defer func() {
		// Closing file and removing all temporary files created by Multipart
		params.Upfile.Close()
		params.HTTPRequest.Body.Close()
		err := params.HTTPRequest.MultipartForm.RemoveAll()
		if err != nil {
			log.WithError(err).Warnf("Failed to delete temporary files used for upload")
		}
	}()

	if params.LogsType == string(models.LogsTypeHost) {
		if params.InfraEnvID == nil || params.HostID == nil {
			return common.NewApiError(http.StatusInternalServerError, errors.New("infra_env_id and host_id are required for upload host logs"))
		}

		dbHost, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
		if err != nil {
			return err
		}

		err = b.uploadHostLogs(ctx, dbHost, params.Upfile)
		if err != nil {
			return err
		}

		eventgen.SendHostLogsUploadedEvent(ctx, b.eventsHandler, *params.HostID, dbHost.InfraEnvID, common.StrFmtUUIDPtr(params.ClusterID),
			hostutil.GetHostnameForMsg(&dbHost.Host))
		return nil
	}

	currentCluster, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return err
	}
	fileName := b.getLogsFullName(params.ClusterID.String(), params.LogsType)
	log.Debugf("Start upload log file %s to bucket %s", fileName, b.S3Bucket)
	err = b.objectHandler.UploadStream(ctx, params.Upfile, fileName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload %s to s3", fileName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if params.LogsType == string(models.LogsTypeController) {
		firstClusterLogCollectionEvent := false
		if time.Time(currentCluster.ControllerLogsCollectedAt).Equal(time.Time{}) {
			firstClusterLogCollectionEvent = true
		}
		err = b.clusterApi.SetUploadControllerLogsAt(ctx, currentCluster, b.db)
		if err != nil {
			log.WithError(err).Errorf("Failed update cluster %s controller_logs_collected_at flag", params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		err = b.clusterApi.UpdateLogsProgress(ctx, currentCluster, string(models.LogsStateCollecting))
		if err != nil {
			log.WithError(err).Errorf("Failed update cluster %s log progress %s", params.ClusterID, string(models.LogsStateCollecting))
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if firstClusterLogCollectionEvent { // Issue an event only for the very first cluster log collection event.
			eventgen.SendClusterLogsUploadedEvent(ctx, b.eventsHandler, params.ClusterID)
		}
	}

	log.Infof("Done uploading file %s", fileName)
	return nil
}

func (b *bareMetalInventory) V2GetCredentials(ctx context.Context, params installer.V2GetCredentialsParams) middleware.Responder {
	c, err := b.GetCredentialsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2GetCredentialsOK().WithPayload(c)
}

func (b *bareMetalInventory) V2ListFeatureSupportLevels(ctx context.Context, params installer.V2ListFeatureSupportLevelsParams) middleware.Responder {
	payload := featuresupport.SupportLevelsList
	return installer.NewV2ListFeatureSupportLevelsOK().WithPayload(payload)
}

func (b *bareMetalInventory) V2ImportCluster(ctx context.Context, params installer.V2ImportClusterParams) middleware.Responder {
	id := strfmt.UUID(uuid.New().String())
	c, err := b.V2ImportClusterInternal(ctx, nil, &id, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2ImportClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) RegenerateInfraEnvSigningKey(ctx context.Context, params installer.RegenerateInfraEnvSigningKeyParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	// generate key for signing rhsso image auth tokens
	imageTokenKey, err := gencrypto.HMACKey(32)
	if err != nil {
		log.WithError(err).Error("Failed to generate new infraEnv image token key")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Update("image_token_key", imageTokenKey).Error; err != nil {
		log.WithError(err).Errorf("Failed to update image token key for infraEnv %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}

	return installer.NewRegenerateInfraEnvSigningKeyNoContent()
}

func (b *bareMetalInventory) V2GetPresignedForClusterCredentials(ctx context.Context, params installer.V2GetPresignedForClusterCredentialsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	if err := b.checkFileDownloadAccess(ctx, params.FileName); err != nil {
		payload := common.GenerateInfraError(http.StatusForbidden, err)
		return installer.NewV2GetPresignedForClusterCredentialsForbidden().WithPayload(payload)
	}

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if !b.objectHandler.IsAwsS3() {
		return common.NewApiError(http.StatusBadRequest, errors.New("Failed to generate presigned URL: invalid backend"))
	}

	fileName := params.FileName
	fullFileName := fmt.Sprintf("%s/%s", params.ClusterID.String(), fileName)
	duration, _ := time.ParseDuration("10m")

	// Kubeconfig-noingress has been created during the installation, but it does not have the ingress CA.
	// At the finalizing phase, we create the kubeconfig file and add the ingress CA.
	// An ingress CA isn't required for normal login but for oauth login which isn't a common use case.
	// Here we fallback to the kubeconfig-noingress for the kubeconfig filename.
	if fileName == constants.Kubeconfig {
		exists, _ := b.objectHandler.DoesObjectExist(ctx, fullFileName)

		if !exists {
			fileName = constants.KubeconfigNoIngress
			fullFileName = fmt.Sprintf("%s/%s", params.ClusterID.String(), constants.KubeconfigNoIngress)
		}
	}

	url, err := b.objectHandler.GeneratePresignedDownloadURL(ctx, fullFileName, fileName, duration)
	if err != nil {
		log.WithError(err).Errorf("failed to generate presigned URL: %s from cluster: %s", params.FileName, params.ClusterID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return installer.NewV2GetPresignedForClusterCredentialsOK().WithPayload(&models.PresignedURL{URL: &url})
}

func (b *bareMetalInventory) GetInfraEnvDownloadURL(ctx context.Context, params installer.GetInfraEnvDownloadURLParams) middleware.Responder {
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	osImage, err := b.getOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	if osImage.OpenshiftVersion == nil {
		return common.GenerateErrorResponder(errors.Errorf("OS image entry '%+v' missing OpenshiftVersion field", osImage))
	}

	newURL, expiresAt, err := b.generateImageDownloadURL(ctx, infraEnv.ID.String(), string(*infraEnv.Type), *osImage.OpenshiftVersion, infraEnv.CPUArchitecture, infraEnv.ImageTokenKey)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	updates := map[string]interface{}{
		"download_url": newURL,
		"expires_at":   *expiresAt,
	}

	if err = b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates).Error; err != nil {
		b.log.WithError(err).Errorf("Failed to update download_url for infraEnv %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}

	return installer.NewGetInfraEnvDownloadURLOK().WithPayload(&models.PresignedURL{URL: &newURL, ExpiresAt: *expiresAt})
}

func (b *bareMetalInventory) generateImageDownloadURL(ctx context.Context, infraEnvID, imageType, version, arch, imageTokenKey string) (string, *strfmt.DateTime, error) {
	urlString, err := imageservice.ImageURL(b.ImageServiceBaseURL, infraEnvID, version, arch, imageType)
	if err != nil {
		return "", nil, err
	}
	return b.signURL(ctx, infraEnvID, urlString, imageTokenKey)
}

func (b *bareMetalInventory) signURL(ctx context.Context, infraEnvID, urlString, imageTokenKey string) (string, *strfmt.DateTime, error) {
	log := logutil.FromContext(ctx, b.log)

	if b.authHandler.AuthType() == auth.TypeLocal {
		var err error
		urlString, err = gencrypto.SignURL(urlString, infraEnvID, gencrypto.InfraEnvKey)
		if err != nil {
			return "", nil, errors.Wrap(err, "failed to sign image URL")
		}
	} else if b.authHandler.AuthType() == auth.TypeRHSSO {
		token, err := gencrypto.JWTForSymmetricKey([]byte(imageTokenKey), b.ImageExpirationTime, infraEnvID)
		if err != nil {
			return "", nil, errors.Wrapf(err, "failed to generate token for infraEnv %s", infraEnvID)
		}
		urlString, err = gencrypto.SignURLWithToken(urlString, "image_token", token)
		if err != nil {
			return "", nil, errors.Wrap(err, "failed to sign image URL with token")
		}
	} else if b.authHandler.AuthType() == auth.TypeNone {
		log.Infof("Auth type is none: image URL will remain as %s", urlString)
	}

	// parse the exp claim out of the url
	var expiresAt strfmt.DateTime
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return "", nil, err
	}

	if tokenString := parsedURL.Query().Get("image_token"); tokenString != "" {
		// we just created these claims so they are safe to parse unverified
		token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil {
			return "", nil, err
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return "", nil, errors.Errorf("malformed token claims in url")
		}
		exp, ok := claims["exp"].(float64)
		if !ok {
			return "", nil, errors.Errorf("token missing 'exp' claim")
		}
		expTime := time.Unix(int64(exp), 0)
		expiresAt = strfmt.DateTime(expTime)
	}

	return urlString, &expiresAt, nil
}

const ipxeScriptFormat = `#!ipxe
initrd --name initrd %s
kernel %s initrd=initrd coreos.live.rootfs_url=%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal console=tty1 console=ttyS1,115200n8 coreos.inst.persistent-kargs="console=tty1 console=ttyS1,115200n8"
boot
`

func (b *bareMetalInventory) infraEnvIPXEScript(ctx context.Context, infraEnv *common.InfraEnv) (string, error) {
	osImage, err := b.getOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return "", err
	}
	if osImage.OpenshiftVersion == nil {
		return "", errors.Errorf("OS image entry '%+v' missing OpenshiftVersion field", osImage)
	}

	kernelURL, err := imageservice.KernelURL(b.ImageServiceBaseURL, *osImage.OpenshiftVersion, *osImage.CPUArchitecture, b.insecureIPXEURLs)
	if err != nil {
		return "", errors.Wrap(err, "failed to create kernel URL")
	}
	rootfsURL, err := imageservice.RootFSURL(b.ImageServiceBaseURL, *osImage.OpenshiftVersion, *osImage.CPUArchitecture, b.insecureIPXEURLs)
	if err != nil {
		return "", errors.Wrap(err, "failed to create rootfs URL")
	}

	initrdURL, err := imageservice.InitrdURL(b.ImageServiceBaseURL, infraEnv.ID.String(), *osImage.OpenshiftVersion, *osImage.CPUArchitecture, b.insecureIPXEURLs)
	if err != nil {
		return "", errors.Wrap(err, "failed to create initrd URL")
	}
	initrdURL, _, err = b.signURL(ctx, infraEnv.ID.String(), initrdURL, infraEnv.ImageTokenKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to sign initrd URL")
	}

	return fmt.Sprintf(ipxeScriptFormat, initrdURL, kernelURL, rootfsURL), nil
}

func (b *bareMetalInventory) GetInfraEnvPresignedFileURL(ctx context.Context, params installer.GetInfraEnvPresignedFileURLParams) middleware.Responder {
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	builder := &installer.V2DownloadInfraEnvFilesURL{
		InfraEnvID: params.InfraEnvID,
		FileName:   params.FileName,
	}
	filesURL, err := builder.Build()
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	baseURL, err := url.Parse(b.Config.ServiceBaseURL)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	baseURL.Path = path.Join(baseURL.Path, filesURL.Path)
	baseURL.RawQuery = filesURL.RawQuery

	signedURL, exp, err := b.signURL(ctx, params.InfraEnvID.String(), baseURL.String(), infraEnv.ImageTokenKey)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return &installer.GetInfraEnvPresignedFileURLOK{
		Payload: &models.PresignedURL{
			URL:       &signedURL,
			ExpiresAt: *exp,
		},
	}
}
