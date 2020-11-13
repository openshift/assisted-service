package auth

import (
	"context"
	"io"
	"io/ioutil"

	eventsapi "github.com/openshift/assisted-service/restapi/operations/events"
	managed_domains_api "github.com/openshift/assisted-service/restapi/operations/managed_domains"
	versionsapi "github.com/openshift/assisted-service/restapi/operations/versions"

	"net/http"

	"github.com/openshift/assisted-service/pkg/filemiddleware"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/assisted_service_iso"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

type fakeInventory struct{}

func (f fakeInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
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
	panic("Implement Me!")
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

func (f fakeInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	return installer.NewUpdateClusterCreated()
}

func (f fakeInventory) GetClusterInstallConfig(ctx context.Context, params installer.GetClusterInstallConfigParams) middleware.Responder {
	return installer.NewGetClusterInstallConfigOK()
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

func (f fakeInventory) GetHostRequirements(ctx context.Context, params installer.GetHostRequirementsParams) middleware.Responder {
	return installer.NewGetHostRequirementsOK()
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

func (f fakeAssistedServiceIsoAPI) GetPresignedForAssistedServiceISO(ctx context.Context, params assisted_service_iso.GetPresignedForAssistedServiceISOParams) middleware.Responder {
	return assisted_service_iso.NewGetPresignedForAssistedServiceISOOK()
}
