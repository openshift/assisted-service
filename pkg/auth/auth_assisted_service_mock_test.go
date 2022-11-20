package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/restapi"
	eventsapi "github.com/openshift/assisted-service/restapi/operations/events"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	managed_domains_api "github.com/openshift/assisted-service/restapi/operations/managed_domains"
	versionsapi "github.com/openshift/assisted-service/restapi/operations/versions"
)

type fakeInventory struct{}

func (f fakeInventory) V2GetPresignedForClusterCredentials(ctx context.Context, params installer.V2GetPresignedForClusterCredentialsParams) middleware.Responder {
	return installer.NewV2GetPresignedForClusterCredentialsOK()
}

func (f fakeInventory) V2GetPreflightRequirements(ctx context.Context, params installer.V2GetPreflightRequirementsParams) middleware.Responder {
	return installer.NewV2GetPreflightRequirementsOK().WithPayload(&models.PreflightHardwareRequirements{})
}

func (f fakeInventory) V2CancelInstallation(ctx context.Context, params installer.V2CancelInstallationParams) middleware.Responder {
	return installer.NewV2CancelInstallationAccepted()
}

func (f fakeInventory) V2CompleteInstallation(ctx context.Context, params installer.V2CompleteInstallationParams) middleware.Responder {
	return installer.NewV2CompleteInstallationAccepted()
}

func (f fakeInventory) V2DeregisterCluster(ctx context.Context, params installer.V2DeregisterClusterParams) middleware.Responder {
	return installer.NewV2DeregisterClusterNoContent()
}

func (f fakeInventory) V2DeregisterHost(ctx context.Context, params installer.V2DeregisterHostParams) middleware.Responder {
	return installer.NewV2DeregisterHostNoContent()
}

func (f fakeInventory) DownloadMinimalInitrd(ctx context.Context, params installer.DownloadMinimalInitrdParams) middleware.Responder {
	return installer.NewDownloadMinimalInitrdOK()
}

func (f fakeInventory) V2DownloadClusterFiles(ctx context.Context, params installer.V2DownloadClusterFilesParams) middleware.Responder {
	return filemiddleware.NewResponder(
		installer.NewV2DownloadClusterFilesOK().WithPayload(io.NopCloser(strings.NewReader("test"))),
		params.FileName,
		int64(len("test")),
		nil)
}

func (f fakeInventory) V2DownloadInfraEnvFiles(ctx context.Context, params installer.V2DownloadInfraEnvFilesParams) middleware.Responder {
	return filemiddleware.NewResponder(
		installer.NewV2DownloadInfraEnvFilesOK().WithPayload(io.NopCloser(strings.NewReader("test"))),
		"test",
		0,
		nil)
}

func (f fakeInventory) V2GetCluster(ctx context.Context, params installer.V2GetClusterParams) middleware.Responder {
	return installer.NewV2GetClusterOK()
}

func (f fakeInventory) V2InstallCluster(ctx context.Context, params installer.V2InstallClusterParams) middleware.Responder {
	return installer.NewV2InstallClusterAccepted()
}

func (f fakeInventory) V2ListClusters(ctx context.Context, params installer.V2ListClustersParams) middleware.Responder {
	return installer.NewV2ListClustersOK()
}

func (f fakeInventory) V2RegisterCluster(ctx context.Context, params installer.V2RegisterClusterParams) middleware.Responder {
	return installer.NewV2RegisterClusterCreated()
}

func (f fakeInventory) V2ImportCluster(ctx context.Context, params installer.V2ImportClusterParams) middleware.Responder {
	return installer.NewV2ImportClusterCreated()
}

func (f fakeInventory) V2ResetCluster(ctx context.Context, params installer.V2ResetClusterParams) middleware.Responder {
	return installer.NewV2ResetClusterAccepted()
}

func (f fakeInventory) UpdateCluster(ctx context.Context, params installer.V2UpdateClusterParams) middleware.Responder {
	return common.NewApiError(http.StatusNotFound, errors.New(common.APINotFound))
}

func (f fakeInventory) V2UpdateCluster(ctx context.Context, params installer.V2UpdateClusterParams) middleware.Responder {
	return installer.NewV2UpdateClusterCreated()
}

func (f fakeInventory) V2UpdateHost(ctx context.Context, params installer.V2UpdateHostParams) middleware.Responder {
	return installer.NewV2UpdateHostCreated()
}

func (f fakeInventory) V2GetClusterInstallConfig(ctx context.Context, params installer.V2GetClusterInstallConfigParams) middleware.Responder {
	return installer.NewV2GetClusterInstallConfigOK()
}

func (f fakeInventory) V2UpdateClusterInstallConfig(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) middleware.Responder {
	return installer.NewV2UpdateClusterInstallConfigCreated()
}

func (f fakeInventory) V2UploadClusterIngressCert(ctx context.Context, params installer.V2UploadClusterIngressCertParams) middleware.Responder {
	return installer.NewV2UploadClusterIngressCertCreated()
}

func (f fakeInventory) V2UpdateClusterLogsProgress(ctx context.Context, params installer.V2UpdateClusterLogsProgressParams) middleware.Responder {
	return installer.NewV2UpdateClusterLogsProgressNoContent()
}

func (f fakeInventory) V2UpdateHostLogsProgress(ctx context.Context, params installer.V2UpdateHostLogsProgressParams) middleware.Responder {
	return installer.NewV2UpdateHostLogsProgressNoContent()
}

func (f fakeInventory) GetClusterSupportedPlatforms(ctx context.Context, params installer.GetClusterSupportedPlatformsParams) middleware.Responder {
	return installer.NewGetClusterSupportedPlatformsOK()
}

func (f fakeInventory) V2UpdateHostIgnition(ctx context.Context, params installer.V2UpdateHostIgnitionParams) middleware.Responder {
	return installer.NewV2UpdateHostIgnitionCreated()
}

func (f fakeInventory) V2DownloadHostIgnition(ctx context.Context, params installer.V2DownloadHostIgnitionParams) middleware.Responder {
	return filemiddleware.NewResponder(
		installer.NewV2DownloadHostIgnitionOK().WithPayload(io.NopCloser(strings.NewReader("test"))),
		"test",
		0,
		nil)
}

func (f fakeInventory) V2UpdateHostInstallerArgs(ctx context.Context, params installer.V2UpdateHostInstallerArgsParams) middleware.Responder {
	return installer.NewV2UpdateHostInstallerArgsCreated()
}

func (f fakeInventory) DeregisterInfraEnv(ctx context.Context, params installer.DeregisterInfraEnvParams) middleware.Responder {
	return installer.NewDeregisterInfraEnvNoContent()
}

func (f fakeInventory) GetInfraEnv(ctx context.Context, params installer.GetInfraEnvParams) middleware.Responder {
	return installer.NewGetInfraEnvOK()
}

func (f fakeInventory) ListInfraEnvs(ctx context.Context, params installer.ListInfraEnvsParams) middleware.Responder {
	return installer.NewListInfraEnvsOK()
}

func (f fakeInventory) RegisterInfraEnv(ctx context.Context, params installer.RegisterInfraEnvParams) middleware.Responder {
	return installer.NewRegisterInfraEnvCreated()
}

func (f fakeInventory) UpdateInfraEnv(ctx context.Context, params installer.UpdateInfraEnvParams) middleware.Responder {
	return installer.NewUpdateInfraEnvCreated()
}

func (f fakeInventory) V2RegisterHost(ctx context.Context, params installer.V2RegisterHostParams) middleware.Responder {
	return installer.NewV2RegisterHostCreated()
}

func (f fakeInventory) V2GetHost(ctx context.Context, params installer.V2GetHostParams) middleware.Responder {
	return installer.NewV2GetHostOK()
}

func (f fakeInventory) V2GetNextSteps(ctx context.Context, params installer.V2GetNextStepsParams) middleware.Responder {
	return installer.NewV2GetNextStepsOK()
}

func (f fakeInventory) V2PostStepReply(ctx context.Context, params installer.V2PostStepReplyParams) middleware.Responder {
	return installer.NewV2PostStepReplyNoContent()
}

func (f fakeInventory) V2UpdateHostInstallProgress(ctx context.Context, params installer.V2UpdateHostInstallProgressParams) middleware.Responder {
	return installer.NewV2UpdateHostInstallProgressOK()
}

func (f fakeInventory) BindHost(ctx context.Context, params installer.BindHostParams) middleware.Responder {
	return installer.NewBindHostOK()
}

func (f fakeInventory) UnbindHost(ctx context.Context, params installer.UnbindHostParams) middleware.Responder {
	return installer.NewUnbindHostOK()
}

func (f fakeInventory) V2ListHosts(ctx context.Context, params installer.V2ListHostsParams) middleware.Responder {
	return installer.NewV2ListHostsOK()
}

func (f fakeInventory) V2GetHostIgnition(ctx context.Context, params installer.V2GetHostIgnitionParams) middleware.Responder {
	return installer.NewV2GetHostIgnitionOK()
}

func (f fakeInventory) V2ResetHostValidation(ctx context.Context, params installer.V2ResetHostValidationParams) middleware.Responder {
	return installer.NewV2ResetHostValidationOK()
}

func (f fakeInventory) V2ResetHost(ctx context.Context, params installer.V2ResetHostParams) middleware.Responder {
	return installer.NewV2ResetHostOK()
}

func (f fakeInventory) V2InstallHost(ctx context.Context, params installer.V2InstallHostParams) middleware.Responder {
	return installer.NewV2InstallHostAccepted()
}

func (f fakeInventory) V2DownloadClusterCredentials(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) middleware.Responder {
	file, err := os.CreateTemp("/tmp", "test.file")
	if err != nil {
		return installer.NewV2DownloadClusterCredentialsInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewV2DownloadClusterCredentialsOK().WithPayload(io.ReadCloser(file)),
		"test",
		0,
		nil)
}

func (f fakeInventory) V2GetPresignedForClusterFiles(ctx context.Context, params installer.V2GetPresignedForClusterFilesParams) middleware.Responder {
	return installer.NewV2GetPresignedForClusterFilesOK()
}

func (f fakeInventory) V2GetClusterDefaultConfig(ctx context.Context, params installer.V2GetClusterDefaultConfigParams) middleware.Responder {
	return installer.NewV2GetClusterDefaultConfigOK()
}

func (f fakeInventory) V2DownloadClusterLogs(ctx context.Context, params installer.V2DownloadClusterLogsParams) middleware.Responder {
	return filemiddleware.NewResponder(
		installer.NewV2DownloadClusterLogsOK().WithPayload(io.NopCloser(strings.NewReader("test"))),
		"test",
		0,
		nil)
}

func (f fakeInventory) V2UploadLogs(ctx context.Context, params installer.V2UploadLogsParams) middleware.Responder {
	return installer.NewV2UploadLogsNoContent()
}

func (f fakeInventory) V2GetCredentials(ctx context.Context, params installer.V2GetCredentialsParams) middleware.Responder {
	return installer.NewV2GetCredentialsOK()
}

func (f fakeInventory) V2ListFeatureSupportLevels(ctx context.Context, params installer.V2ListFeatureSupportLevelsParams) middleware.Responder {
	return installer.NewV2ListFeatureSupportLevelsOK()
}

func (b fakeInventory) RegenerateInfraEnvSigningKey(ctx context.Context, params installer.RegenerateInfraEnvSigningKeyParams) middleware.Responder {
	return installer.NewRegenerateInfraEnvSigningKeyNoContent()
}

func (f fakeInventory) GetInfraEnvDownloadURL(ctx context.Context, params installer.GetInfraEnvDownloadURLParams) middleware.Responder {
	return installer.NewGetInfraEnvDownloadURLOK()
}

func (f fakeInventory) GetInfraEnvPresignedFileURL(ctx context.Context, params installer.GetInfraEnvPresignedFileURLParams) middleware.Responder {
	return installer.NewGetInfraEnvPresignedFileURLOK()
}

func (f fakeInventory) TransformClusterToDay2(ctx context.Context, params installer.TransformClusterToDay2Params) middleware.Responder {
	return installer.NewTransformClusterToDay2Accepted()
}

func (f fakeInventory) ListClusterHosts(ctx context.Context, params installer.ListClusterHostsParams) middleware.Responder {
	return installer.NewListClusterHostsOK()
}

var _ restapi.InstallerAPI = fakeInventory{}

type fakeEventsAPI struct{}

func (f fakeEventsAPI) V2EventsSubscribe(ctx context.Context, params eventsapi.V2EventsSubscribeParams) middleware.Responder {
	return eventsapi.NewV2EventsSubscribeCreated()
}

func (f fakeEventsAPI) V2EventsSubscriptionDelete(ctx context.Context, params eventsapi.V2EventsSubscriptionDeleteParams) middleware.Responder {
	return eventsapi.NewV2EventsSubscriptionDeleteNoContent()
}

func (f fakeEventsAPI) V2EventsSubscriptionGet(ctx context.Context, params eventsapi.V2EventsSubscriptionGetParams) middleware.Responder {
	return eventsapi.NewV2EventsSubscriptionGetOK()
}

func (f fakeEventsAPI) V2EventsSubscriptionList(ctx context.Context, params eventsapi.V2EventsSubscriptionListParams) middleware.Responder {
	return eventsapi.NewV2EventsSubscriptionListOK()
}

func (f fakeEventsAPI) V2ListEvents(ctx context.Context, params eventsapi.V2ListEventsParams) middleware.Responder {
	return eventsapi.NewV2ListEventsOK()
}

type fakeVersionsAPI struct{}

func (f fakeVersionsAPI) V2ListComponentVersions(
	_ context.Context,
	_ versionsapi.V2ListComponentVersionsParams) middleware.Responder {
	return versionsapi.NewV2ListComponentVersionsOK()
}

func (f fakeVersionsAPI) V2ListSupportedOpenshiftVersions(
	_ context.Context,
	_ versionsapi.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	return versionsapi.NewV2ListSupportedOpenshiftVersionsOK()
}

type fakeManagedDomainsAPI struct{}

func (f fakeManagedDomainsAPI) V2ListManagedDomains(
	_ context.Context,
	_ managed_domains_api.V2ListManagedDomainsParams) middleware.Responder {
	return managed_domains_api.NewV2ListManagedDomainsOK()
}
