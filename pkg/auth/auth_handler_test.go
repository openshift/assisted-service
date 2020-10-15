package auth

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-openapi/runtime"
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
	"github.com/pkg/errors"
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
			AuthHandler := NewAuthHandler(fakeConfig, nil, log.WithField("pkg", "auth"), nil)

			ocmAuthz := ocm.NewMockOCMAuthorization(ctrl)
			ocmAuthz.EXPECT().CapabilityReview(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any()).Return(true, nil).AnyTimes()

			AuthHandler.client = &ocm.Client{
				Authentication: ocmAuth,
				Authorization:  ocmAuthz,
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

var mockOcmAuthFailure = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, errors.Errorf("error")
		}).Times(1)
}

var mockOcmAuthInternalError = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("error"))
		}).Times(1)
}

var mockOcmAuthSendRequestFailure = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return nil, common.NewApiError(http.StatusServiceUnavailable, errors.Errorf("error"))
		}).Times(1)
}

var mockOcmAuthSuccess = func(a *ocm.MockOCMAuthentication) {
	a.EXPECT().AuthenticatePullSecret(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, pullSecret string) (user *ocm.AuthPayload, err error) {
			return &ocm.AuthPayload{}, nil
		}).Times(1)
}
