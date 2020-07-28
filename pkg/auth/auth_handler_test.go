package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"

	"github.com/filanov/bm-inventory/client"
	clientInstaller "github.com/filanov/bm-inventory/client/installer"
	"github.com/google/uuid"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/assert"

	"github.com/filanov/bm-inventory/restapi"
	"github.com/filanov/bm-inventory/restapi/operations/installer"
	"github.com/sirupsen/logrus"
)

func NewFakeAuthUtils(url string) AUtilsInteface {
	return &fakeAUtils{
		url: url,
	}
}

type fakeAUtils struct {
	url string
}

func (au *fakeAUtils) downloadPublicKeys(cas *x509.CertPool) (keyMap map[string]*rsa.PublicKey, err error) {
	return nil, nil
}

func NewFakeAuthHandler(cfg Config, log logrus.FieldLogger) *AuthHandler {
	a := &AuthHandler{
		EnableAuth: cfg.EnableAuth,
		utils:      NewFakeAuthUtils(cfg.JwkCertURL),
		log:        log,
	}
	err := a.populateKeyMap()
	if err != nil {
		log.Fatalln("Failed to init auth handler,", err)
	}
	return a
}

func serv(server *http.Server) {
	_ = server.ListenAndServe()
}

func TestAuth(t *testing.T) {
	log := logrus.New()

	agentKey := "X-Secret-Key"
	agentKeyValue := "SecretKey"

	userKey := "Authorization"
	userKeyValue := "userKey"

	t.Parallel()
	tests := []struct {
		name                   string
		tokenKey               string
		expectedTokenValue     string
		isListOperation        bool
		enableAuth             bool
		addHeaders             bool
		expectedRequestSuccess bool
	}{
		{
			name:                   "User Successful Authentication",
			tokenKey:               userKey,
			expectedTokenValue:     userKeyValue,
			isListOperation:        true,
			enableAuth:             true,
			addHeaders:             true,
			expectedRequestSuccess: true,
		},
		{
			name:                   "Fail auth without headers",
			tokenKey:               agentKey,
			expectedTokenValue:     agentKeyValue,
			isListOperation:        false,
			enableAuth:             true,
			addHeaders:             false,
			expectedRequestSuccess: false,
		},
		{
			name:                   "Ignore auth if disabled",
			tokenKey:               userKey,
			expectedTokenValue:     userKeyValue,
			isListOperation:        true,
			enableAuth:             false,
			addHeaders:             false,
			expectedRequestSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeConfig := Config{
				EnableAuth: tt.enableAuth,
				JwkCertURL: "https://api.openshift.com/.well-known/jwks.json",
			}
			fakeAuthHandler := NewFakeAuthHandler(fakeConfig, log.WithField("pkg", "auth"))

			authAgentAuth := func(token string) (interface{}, error) {
				assert.Equal(t, tt.expectedTokenValue, token)
				assert.Equal(t, tt.tokenKey, agentKey)
				return "user2", nil
			}

			authUserAuth := func(token string) (interface{}, error) {
				assert.Equal(t, tt.expectedTokenValue, token)
				assert.Equal(t, tt.tokenKey, userKey)
				return "user1", nil
			}

			h, _ := restapi.Handler(restapi.Config{
				AuthAgentAuth:       authAgentAuth,
				AuthUserAuth:        authUserAuth,
				APIKeyAuthenticator: fakeAuthHandler.CreateAuthenticator(),
				InstallerAPI:        fakeInventory{},
				EventsAPI:           nil,
				Logger:              logrus.Printf,
				VersionsAPI:         nil,
				ManagedDomainsAPI:   nil,
				InnerMiddleware:     nil,
			})

			clientAuth := func() runtime.ClientAuthInfoWriter {
				return runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
					return r.SetHeaderParam(tt.tokenKey, tt.expectedTokenValue)
				})
			}

			cfg := client.Config{
				URL: &url.URL{
					Scheme: client.DefaultSchemes[0],
					Host:   "localhost:8081",
					Path:   client.DefaultBasePath,
				},
			}
			if tt.addHeaders {
				cfg.AuthInfo = clientAuth()
			}
			bmclient := client.New(cfg)

			server := &http.Server{Addr: "localhost:8081", Handler: h}
			go serv(server)
			defer server.Close()

			expectedStatusCode := 401
			if tt.expectedRequestSuccess {
				expectedStatusCode = 200
			}

			var e error
			if tt.isListOperation {
				_, e = bmclient.Installer.ListClusters(context.TODO(), &clientInstaller.ListClustersParams{})
			} else {
				id := uuid.New()
				_, e = bmclient.Installer.GetCluster(context.TODO(), &clientInstaller.GetClusterParams{
					ClusterID: strfmt.UUID(id.String()),
				})
			}
			if expectedStatusCode == 200 {
				assert.Nil(t, e)
			} else {
				apierr := e.(*runtime.APIError)
				assert.Equal(t, apierr.Code, expectedStatusCode)

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
	panic("Implement Me!")
}

func (f fakeInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) SetDebugStep(ctx context.Context, params installer.SetDebugStepParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	panic("Implement Me!")
}

func (f fakeInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	panic("Implement Me!")
}

var _ restapi.InstallerAPI = fakeInventory{}
