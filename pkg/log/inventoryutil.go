package log

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

type FakeInventory struct {
	log logrus.FieldLogger
}

func (f FakeInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DisableHost(ctx context.Context, params installer.DisableHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetPresignedForClusterFiles(ctx context.Context, params installer.GetPresignedForClusterFilesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	FromContext(ctx, f.log).Info("say something")
	return installer.NewGetClusterOK()
}

func (f FakeInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetHost(ctx context.Context, params installer.GetHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) ListClusters(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
	FromContext(ctx, f.log).Info("say something")
	return installer.NewListClustersOK()
}

func (f FakeInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	return installer.NewRegisterClusterCreated()
}

func (f FakeInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	return installer.NewRegisterHostCreated()
}

func (f FakeInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetClusterInstallConfig(ctx context.Context, params installer.GetClusterInstallConfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) UploadHostLogs(ctx context.Context, params installer.UploadHostLogsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) DownloadHostLogs(ctx context.Context, params installer.DownloadHostLogsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f FakeInventory) GetHostRequirements(ctx context.Context, params installer.GetHostRequirementsParams) middleware.Responder {
	panic("Implement Me!")
}
func (f FakeInventory) DownloadClusterLogs(ctx context.Context, params installer.DownloadClusterLogsParams) middleware.Responder {
	panic("Implement Me!")
}
