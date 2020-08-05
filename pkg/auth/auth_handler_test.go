package auth

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/dgrijalva/jwt-go"

	"github.com/openshift/assisted-service/client"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func serv(server *http.Server) {
	_ = server.ListenAndServe()
}

func GetTokenAndCert() (string, []byte) {

	//Generate RSA Keypair
	pub, priv, _ := GenKeys(2048)

	//Generate keys in JWK format
	pubJSJWKS, _, kid, _ := GenJSJWKS(priv, pub)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"account_number": "1234567",
		"is_internal":    false,
		"is_active":      true,
		"account_id":     "7654321",
		"org_id":         "1010101",
		"last_name":      "Doe",
		"type":           "User",
		"locale":         "en_US",
		"first_name":     "John",
		"email":          "jdoe123@example.com",
		"username":       "jdoe123@example.com",
		"is_org_admin":   false,
		"clientId":       "1234",
	})
	token.Header["kid"] = kid
	tokenString, _ := token.SignedString(priv)
	return tokenString, pubJSJWKS
}

func TestAuth(t *testing.T) {
	log := logrus.New()

	userToken, JwkCert := GetTokenAndCert()
	agentKeyValue := "fake_pull_secret"
	userKeyValue := "bearer " + userToken
	t.Parallel()
	tests := []struct {
		name                   string
		expectedTokenValue     string
		authInfo               runtime.ClientAuthInfoWriter
		isListOperation        bool
		enableAuth             bool
		addHeaders             bool
		expectedRequestSuccess bool
	}{
		{
			name:                   "User Successful Authentication",
			expectedTokenValue:     userKeyValue,
			authInfo:               UserAuthHeaderWriter(userKeyValue),
			isListOperation:        true,
			enableAuth:             true,
			addHeaders:             true,
			expectedRequestSuccess: true,
		},
		{
			name:                   "Fail auth without headers",
			expectedTokenValue:     agentKeyValue,
			authInfo:               AgentAuthHeaderWriter(agentKeyValue),
			isListOperation:        false,
			enableAuth:             true,
			addHeaders:             false,
			expectedRequestSuccess: false,
		},
		{
			name:                   "Ignore auth if disabled",
			expectedTokenValue:     userKeyValue,
			authInfo:               UserAuthHeaderWriter(userKeyValue),
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
				JwkCertURL: "",
				JwkCert:    string(JwkCert),
			}
			AuthHandler := NewAuthHandler(fakeConfig, nil, log.WithField("pkg", "auth"))

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
			} else if expectedStatusCode == 401 {
				assert.NotNil(t, e)
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
	return installer.NewRegisterClusterCreated()
}

func (f fakeInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	return installer.NewRegisterHostCreated()
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
