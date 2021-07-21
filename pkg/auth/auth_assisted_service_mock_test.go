package auth

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	restapi "github.com/openshift/assisted-service/restapi/restapi_v1"
	"github.com/openshift/assisted-service/restapi/restapi_v1/operations/assisted_service_iso"
	eventsapi "github.com/openshift/assisted-service/restapi/restapi_v1/operations/events"
	"github.com/openshift/assisted-service/restapi/restapi_v1/operations/installer"
	managed_domains_api "github.com/openshift/assisted-service/restapi/restapi_v1/operations/managed_domains"
	versionsapi "github.com/openshift/assisted-service/restapi/restapi_v1/operations/versions"
)

type fakeInventory struct{}

func (f fakeInventory) GetPreflightRequirements(ctx context.Context, params installer.GetPreflightRequirementsParams) middleware.Responder {
	return installer.NewGetPreflightRequirementsOK().WithPayload(&models.PreflightHardwareRequirements{})
}

func (f fakeInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	return installer.NewCancelInstallationAccepted()
}

func (f fakeInventory) Prog(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	return installer.NewCancelInstallationAccepted()
}

func (f fakeInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	return installer.NewCompleteInstallationAccepted()
}

func (f fakeInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	return installer.NewDeregisterClusterNoContent()
}

func (f fakeInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
	return installer.NewDeregisterHostNoContent()
}

func (f fakeInventory) DisableHost(ctx context.Context, params installer.DisableHostParams) middleware.Responder {
	return installer.NewDisableHostOK()
}

func (f fakeInventory) GetPresignedForClusterFiles(ctx context.Context, params installer.GetPresignedForClusterFilesParams) middleware.Responder {
	return installer.NewGetPresignedForClusterFilesOK()
}

func (f fakeInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadClusterFilesInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadClusterFilesOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadClusterISOInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadClusterISOOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) DownloadClusterISOHeaders(ctx context.Context, params installer.DownloadClusterISOHeadersParams) middleware.Responder {
	_, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadClusterISOHeadersInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadClusterISOHeadersOK(),
		"test",
		0)
}

func (f fakeInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadClusterKubeconfigInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadClusterKubeconfigOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	return installer.NewEnableHostOK()
}

func (f fakeInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	return installer.NewGenerateClusterISOCreated()
}

func (f fakeInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	return installer.NewGetClusterOK()
}

func (f fakeInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	return installer.NewGetCredentialsOK()
}

func (f fakeInventory) GetFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams) middleware.Responder {
	return installer.NewGetFreeAddressesOK()
}

func (f fakeInventory) GetHost(ctx context.Context, params installer.GetHostParams) middleware.Responder {
	return installer.NewGetHostOK()
}

func (f fakeInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	return installer.NewGetNextStepsOK()
}

func (f fakeInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	return installer.NewInstallClusterAccepted()
}

func (f fakeInventory) InstallHosts(ctx context.Context, params installer.InstallHostsParams) middleware.Responder {
	return installer.NewInstallHostsAccepted()
}

func (f fakeInventory) InstallHost(ctx context.Context, params installer.InstallHostParams) middleware.Responder {
	return installer.NewInstallHostAccepted()
}

func (f fakeInventory) ListClusters(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
	return installer.NewListClustersOK()
}

func (f fakeInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	return installer.NewListHostsOK()
}

func (f fakeInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	return installer.NewPostStepReplyNoContent()
}

func (f fakeInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	return installer.NewRegisterClusterCreated()
}

func (f fakeInventory) RegisterAddHostsCluster(ctx context.Context, params installer.RegisterAddHostsClusterParams) middleware.Responder {
	return installer.NewRegisterAddHostsClusterCreated()
}

func (f fakeInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	return installer.NewRegisterHostCreated()
}

func (f fakeInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	return installer.NewResetClusterAccepted()
}

func (f fakeInventory) ResetHost(ctx context.Context, params installer.ResetHostParams) middleware.Responder {
	return installer.NewResetHostOK()
}

func (f fakeInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	return installer.NewUpdateClusterCreated()
}

func (f fakeInventory) GetClusterInstallConfig(ctx context.Context, params installer.GetClusterInstallConfigParams) middleware.Responder {
	return installer.NewGetClusterInstallConfigOK()
}

func (f fakeInventory) GetClusterDefaultConfig(ctx context.Context, params installer.GetClusterDefaultConfigParams) middleware.Responder {
	return installer.NewGetClusterDefaultConfigOK()
}

func (f fakeInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	return installer.NewUpdateClusterInstallConfigCreated()
}

func (f fakeInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	return installer.NewUpdateHostInstallProgressOK()
}

func (f fakeInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	return installer.NewUploadClusterIngressCertCreated()
}

func (f fakeInventory) UploadHostLogs(ctx context.Context, params installer.UploadHostLogsParams) middleware.Responder {
	return installer.NewUploadHostLogsNoContent()
}

func (f fakeInventory) UpdateClusterLogsProgress(ctx context.Context, params installer.UpdateClusterLogsProgressParams) middleware.Responder {
	return installer.NewUpdateClusterLogsProgressNoContent()
}

func (f fakeInventory) UpdateHostLogsProgress(ctx context.Context, params installer.UpdateHostLogsProgressParams) middleware.Responder {
	return installer.NewUpdateHostLogsProgressNoContent()
}

func (f fakeInventory) UploadLogs(ctx context.Context, params installer.UploadLogsParams) middleware.Responder {
	return installer.NewUploadLogsNoContent()
}

func (f fakeInventory) DownloadHostLogs(ctx context.Context, params installer.DownloadHostLogsParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadHostLogsInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadHostLogsOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) DownloadClusterLogs(ctx context.Context, params installer.DownloadClusterLogsParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadClusterLogsInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadClusterLogsOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) GetDiscoveryIgnition(ctx context.Context, params installer.GetDiscoveryIgnitionParams) middleware.Responder {
	return installer.NewGetDiscoveryIgnitionOK()
}

func (f fakeInventory) UpdateDiscoveryIgnition(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) middleware.Responder {
	return installer.NewUpdateDiscoveryIgnitionCreated()
}

func (f fakeInventory) UpdateHostIgnition(ctx context.Context, params installer.UpdateHostIgnitionParams) middleware.Responder {
	return installer.NewUpdateHostIgnitionCreated()
}

func (f fakeInventory) GetHostIgnition(ctx context.Context, params installer.GetHostIgnitionParams) middleware.Responder {
	return installer.NewGetHostIgnitionOK()
}

func (f fakeInventory) DownloadHostIgnition(ctx context.Context, params installer.DownloadHostIgnitionParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return installer.NewDownloadHostIgnitionInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		installer.NewDownloadHostIgnitionOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeInventory) UpdateHostInstallerArgs(ctx context.Context, params installer.UpdateHostInstallerArgsParams) middleware.Responder {
	return installer.NewUpdateHostInstallerArgsCreated()
}

func (f fakeInventory) GetClusterHostRequirements(ctx context.Context, params installer.GetClusterHostRequirementsParams) middleware.Responder {
	return installer.NewGetClusterHostRequirementsOK().WithPayload(models.ClusterHostRequirementsList{})
}

func (f fakeInventory) ResetHostValidation(ctx context.Context, params installer.ResetHostValidationParams) middleware.Responder {
	return installer.NewResetHostValidationOK()
}

func (f fakeInventory) V2RegisterInfraEnv(ctx context.Context, params installer.V2RegisterInfraEnvParams) middleware.Responder {
	return installer.NewV2RegisterInfraEnvNotImplemented()
}

var _ restapi.InstallerAPI = fakeInventory{}

type fakeEventsAPI struct{}

func (f fakeEventsAPI) ListEvents(
	_ context.Context,
	_ eventsapi.ListEventsParams) middleware.Responder {
	return eventsapi.NewListEventsOK()
}

type fakeVersionsAPI struct{}

func (f fakeVersionsAPI) ListComponentVersions(
	_ context.Context,
	_ versionsapi.ListComponentVersionsParams) middleware.Responder {
	return versionsapi.NewListComponentVersionsOK()
}

func (f fakeVersionsAPI) ListSupportedOpenshiftVersions(
	_ context.Context,
	_ versionsapi.ListSupportedOpenshiftVersionsParams) middleware.Responder {
	return versionsapi.NewListSupportedOpenshiftVersionsOK()
}

type fakeManagedDomainsAPI struct{}

func (f fakeManagedDomainsAPI) ListManagedDomains(
	_ context.Context,
	_ managed_domains_api.ListManagedDomainsParams) middleware.Responder {
	return managed_domains_api.NewListManagedDomainsOK()
}

type fakeAssistedServiceIsoAPI struct{}

func (f fakeAssistedServiceIsoAPI) CreateISOAndUploadToS3(ctx context.Context, params assisted_service_iso.CreateISOAndUploadToS3Params) middleware.Responder {
	return assisted_service_iso.NewCreateISOAndUploadToS3Created()
}

func (f fakeAssistedServiceIsoAPI) DownloadISO(ctx context.Context, params assisted_service_iso.DownloadISOParams) middleware.Responder {
	file, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return assisted_service_iso.NewDownloadISOInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		assisted_service_iso.NewDownloadISOOK().WithPayload(io.ReadCloser(file)),
		"test",
		0)
}

func (f fakeAssistedServiceIsoAPI) DownloadISOHeaders(ctx context.Context, params assisted_service_iso.DownloadISOParams) middleware.Responder {
	_, err := ioutil.TempFile("/tmp", "test.file")
	if err != nil {
		return assisted_service_iso.NewDownloadISOInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(
		assisted_service_iso.NewDownloadISOOK(),
		"test",
		0)
}

func (f fakeAssistedServiceIsoAPI) GetPresignedForAssistedServiceISO(ctx context.Context, params assisted_service_iso.GetPresignedForAssistedServiceISOParams) middleware.Responder {
	return assisted_service_iso.NewGetPresignedForAssistedServiceISOOK()
}
