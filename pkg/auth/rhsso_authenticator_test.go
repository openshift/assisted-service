package auth

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	client "github.com/openshift/assisted-service/client/client_v1"
	clientInstaller "github.com/openshift/assisted-service/client/client_v1/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	restapi "github.com/openshift/assisted-service/restapi/restapi_v1"
	"github.com/openshift/assisted-service/restapi/restapi_v1/operations/installer"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var _ = Describe("auth handler test", func() {
	var (
		log                        = logrus.New()
		ctrl                       *gomock.Controller
		server                     *ghttp.Server
		userToken, JwkCert         = GetTokenAndCert(false)
		agentKeyValue              = "fake_pull_secret"
		userKeyValue               = "bearer " + userToken
		lateUserToken, lateJwkCert = GetTokenAndCert(true)
		lateUserKeyValue           = "bearer " + lateUserToken
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		server = ghttp.NewServer()
	})

	AfterEach(func() {
		ctrl.Finish()
		server.Close()
	})

	tests := []struct {
		name            string
		authInfo        runtime.ClientAuthInfoWriter
		isIatTest       bool
		isListOperation bool
		addHeaders      bool
		mockOcmAuth     func(a *ocm.MockOCMAuthentication)
		expectedError   interface{}
	}{
		{
			name:            "User Successful Authentication",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			addHeaders:      true,
		},
		{
			name:            "User Unsuccessful Authentication",
			authInfo:        UserAuthHeaderWriter("bearer bad_token"),
			isListOperation: true,
			addHeaders:      true,
			expectedError:   installer.NewListClustersUnauthorized(),
		},
		{
			name:            "Fail User Auth Without Headers",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			addHeaders:      false,
			expectedError:   installer.NewListClustersUnauthorized(),
		},
		{
			name:            "Ignore 'Token used before issued' error",
			authInfo:        UserAuthHeaderWriter(lateUserKeyValue),
			isIatTest:       true,
			isListOperation: true,
			addHeaders:      true,
		},
		{
			name:            "Agent Successful Authentication",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			mockOcmAuth:     mockOcmAuthSuccess,
		},
		{
			name:            "Agent ocm authentication failure",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterUnauthorized(),
			mockOcmAuth:     mockOcmAuthFailure,
		},
		{
			name:            "Agent ocm authentication failure can not send request",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterServiceUnavailable(),
			mockOcmAuth:     mockOcmAuthSendRequestFailure,
		},
		{
			name:            "Agent ocm authentication failure return internal error",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			expectedError:   installer.NewGetClusterInternalServerError(),
			mockOcmAuth:     mockOcmAuthInternalError,
		},
		{
			name:            "Fail Agent Auth Without Headers",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      false,
			expectedError:   installer.NewGetClusterUnauthorized(),
		},
	}

	for _, tt := range tests {
		tt := tt
		It(tt.name, func() {
			ocmAuth := ocm.NewMockOCMAuthentication(ctrl)
			if tt.mockOcmAuth != nil {
				tt.mockOcmAuth(ocmAuth)
			}

			fakeConfig := &Config{
				JwkCertURL: "",
				JwkCert:    string(JwkCert),
			}
			if tt.isIatTest {
				fakeConfig.JwkCert = string(lateJwkCert)
			}
			authHandler := NewRHSSOAuthenticator(fakeConfig, nil, log.WithField("pkg", "auth"), nil)

			ocmAuthz := ocm.NewMockOCMAuthorization(ctrl)
			ocmAuthz.EXPECT().CapabilityReview(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any()).Return(true, nil).AnyTimes()

			authHandler.client = &ocm.Client{
				Authentication: ocmAuth,
				Authorization:  ocmAuthz,
				Cache:          cache.New(1*time.Hour, 30*time.Minute),
			}

			h, _ := restapi.Handler(restapi.Config{
				AuthAgentAuth:         authHandler.AuthAgentAuth,
				AuthUserAuth:          authHandler.AuthUserAuth,
				APIKeyAuthenticator:   authHandler.CreateAuthenticator(),
				InstallerAPI:          fakeInventory{},
				AssistedServiceIsoAPI: fakeAssistedServiceIsoAPI{},
				EventsAPI:             nil,
				Logger:                logrus.Printf,
				VersionsAPI:           nil,
				ManagedDomainsAPI:     nil,
				InnerMiddleware:       nil,
			})

			cfg := client.Config{
				URL: &url.URL{
					Scheme: client.DefaultSchemes[0],
					Host:   server.Addr(),
					Path:   client.DefaultBasePath,
				},
			}
			if tt.addHeaders {
				cfg.AuthInfo = tt.authInfo
			}
			bmclient := client.New(cfg)

			server.AppendHandlers(h.ServeHTTP)

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
				Expect(reflect.TypeOf(e).String()).To(Equal(reflect.TypeOf(tt.expectedError).String()))
			} else {
				Expect(e).To(BeNil())
			}
		})
	}
})

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
