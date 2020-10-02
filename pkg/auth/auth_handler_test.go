package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/client"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func serv(server *http.Server) {
	_ = server.ListenAndServe()
}

func TestAuth(t *testing.T) {
	log := logrus.New()

	userToken, JwkCert := GetTokenAndCert()
	agentKeyValue := "fake_pull_secret"
	userKeyValue := "bearer " + userToken
	t.Parallel()
	tests := []struct {
		name            string
		authInfo        runtime.ClientAuthInfoWriter
		isListOperation bool
		enableAuth      bool
		addHeaders      bool
		mockOcmAuth     func(a *ocm.MockOCMAuthentication)
		expectedError   interface{}
	}{
		{
			name:            "User Successful Authentication",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			enableAuth:      true,
			addHeaders:      true,
		},
		{
			name:            "User Unsuccessful Authentication",
			authInfo:        UserAuthHeaderWriter("bearer bad_token"),
			isListOperation: true,
			enableAuth:      true,
			addHeaders:      true,
			expectedError:   installer.NewListClustersUnauthorized(),
		},
		{
			name:            "Fail User Auth Without Headers",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			enableAuth:      true,
			addHeaders:      false,
			expectedError:   installer.NewListClustersUnauthorized(),
		},
		{
			name:            "Agent Successful Authentication",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      true,
			addHeaders:      true,
			mockOcmAuth:     mockOcmAuthSuccess,
		},
		{
			name:            "Agent ocm authentication failure",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      true,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterUnauthorized(),
			mockOcmAuth:     mockOcmAuthFailure,
		},
		{
			name:            "Agent ocm authentication failure can not send request",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      true,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterServiceUnavailable(),
			mockOcmAuth:     mockOcmAuthSendRequestFailure,
		},
		{
			name:            "Agent ocm authentication failure return internal error",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      true,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterInternalServerError(),
			mockOcmAuth:     mockOcmAuthInternalError,
		},
		{
			name:            "Fail Agent Auth Without Headers",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      true,
			addHeaders:      false,
			expectedError:   installer.NewGetClusterUnauthorized(),
		},
		{
			name:            "Ignore User Auth If Auth Disabled",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			enableAuth:      false,
			addHeaders:      false,
		},
		{
			name:            "Ignore Agent Auth If Auth Disabled",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			enableAuth:      false,
			addHeaders:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			ocmAuth := ocm.NewMockOCMAuthentication(ctrl)
			if tt.mockOcmAuth != nil {
				tt.mockOcmAuth(ocmAuth)
			}

			fakeConfig := Config{
				EnableAuth: tt.enableAuth,
				JwkCertURL: "",
				JwkCert:    string(JwkCert),
			}
			AuthHandler := NewAuthHandler(fakeConfig, nil, log.WithField("pkg", "auth"))
			AuthHandler.client = &ocm.Client{
				Authentication: ocmAuth,
				Authorization:  &mockOCMAuthorization{},
				Cache:          cache.New(1*time.Hour, 30*time.Minute),
			}

			h, _ := restapi.Handler(restapi.Config{
				AuthAgentAuth:       AuthHandler.AuthAgentAuth,
				AuthUserAuth:        AuthHandler.AuthUserAuth,
				APIKeyAuthenticator: AuthHandler.CreateAuthenticator(),
				InstallerAPI:        fakeInventory{},
				EventsAPI:           nil,
				Logger:              logrus.Printf,
				VersionsAPI:         nil,
				ManagedDomainsAPI:   nil,
				InnerMiddleware:     nil,
			})

			cfg := client.Config{
				URL: &url.URL{
					Scheme: client.DefaultSchemes[0],
					Host:   "localhost:8081",
					Path:   client.DefaultBasePath,
				},
			}
			if tt.addHeaders {
				cfg.AuthInfo = tt.authInfo
			}
			bmclient := client.New(cfg)

			server := &http.Server{Addr: "localhost:8081", Handler: h}
			go serv(server)
			defer server.Close()
			time.Sleep(time.Second * 1) // Allow the server to start

			var e error
			if tt.isListOperation {
				_, e = bmclient.Installer.ListClusters(context.TODO(), &clientInstaller.ListClustersParams{})
			} else {
				id := uuid.New()
				_, e = bmclient.Installer.GetCluster(context.TODO(), &clientInstaller.GetClusterParams{
					ClusterID: strfmt.UUID(id.String()),
				})
			}

			if tt.expectedError != nil {
				assert.Equal(t, reflect.TypeOf(e).String(), reflect.TypeOf(tt.expectedError).String())
			} else {
				assert.Nil(t, e)
			}

		})
	}
}

type fakeInventory struct{}

func (f fakeInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DisableHost(ctx context.Context, params installer.DisableHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetPresignedForClusterFiles(ctx context.Context, params installer.GetPresignedForClusterFilesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	return installer.NewGetClusterOK()
}

func (f fakeInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetHost(ctx context.Context, params installer.GetHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) InstallHosts(ctx context.Context, params installer.InstallHostsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) ListClusters(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
	return installer.NewListClustersOK()
}

func (f fakeInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	panic("Implement Me!")
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
	panic("Implement Me!")
}

func (f fakeInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetClusterInstallConfig(ctx context.Context, params installer.GetClusterInstallConfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UploadHostLogs(ctx context.Context, params installer.UploadHostLogsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) DownloadHostLogs(ctx context.Context, params installer.DownloadHostLogsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) GetHostRequirements(ctx context.Context, params installer.GetHostRequirementsParams) middleware.Responder {
	panic("Implement Me!")
}
func (f fakeInventory) DownloadClusterLogs(ctx context.Context, params installer.DownloadClusterLogsParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UploadLogs(ctx context.Context, params installer.UploadLogsParams) middleware.Responder {
	panic("Implement Me!")
}

var _ restapi.InstallerAPI = fakeInventory{}

var mockOcmAuthFailure = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, fmt.Errorf("error")
		}).Times(1)
}

var mockOcmAuthInternalError = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error"))
		}).Times(1)
}

var mockOcmAuthSendRequestFailure = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, common.NewApiError(http.StatusServiceUnavailable, fmt.Errorf("error"))
		}).Times(1)
}

var mockOcmAuthSuccess = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return &ocm.AuthPayload{}, nil
		}).Times(1)
}
