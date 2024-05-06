package auth

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-service/client"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/gencrypto"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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
			expectedError:   installer.NewV2ListClustersUnauthorized(),
		},
		{
			name:            "Fail User Auth Without Headers",
			authInfo:        UserAuthHeaderWriter(userKeyValue),
			isListOperation: true,
			addHeaders:      false,
			expectedError:   installer.NewV2ListClustersUnauthorized(),
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
			expectedError:   installer.NewV2GetClusterUnauthorized(),
			mockOcmAuth:     mockOcmAuthFailure,
		},
		{
			name:            "Agent ocm authentication failure can not send request",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			expectedError:   installer.NewV2GetClusterServiceUnavailable(),
			mockOcmAuth:     mockOcmAuthSendRequestFailure,
		},
		{
			name:            "Agent ocm authentication failure return internal error",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      true,
			expectedError:   installer.NewV2GetClusterInternalServerError(),
			mockOcmAuth:     mockOcmAuthInternalError,
		},
		{
			name:            "Fail Agent Auth Without Headers",
			authInfo:        AgentAuthHeaderWriter(agentKeyValue),
			isListOperation: false,
			addHeaders:      false,
			expectedError:   installer.NewV2GetClusterUnauthorized(),
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
				AuthAgentAuth:       authHandler.AuthAgentAuth,
				AuthUserAuth:        authHandler.AuthUserAuth,
				APIKeyAuthenticator: authHandler.CreateAuthenticator(),
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
				_, e = bmclient.Installer.V2ListClusters(context.TODO(), &clientInstaller.V2ListClustersParams{})
			} else {
				id := uuid.New()
				_, e = bmclient.Installer.V2GetCluster(context.TODO(), &clientInstaller.V2GetClusterParams{
					ClusterID: strfmt.UUID(id.String()),
				})
			}

			if tt.expectedError != nil {
				Expect(reflect.TypeOf(e).String()).To(Equal(reflect.TypeOf(tt.expectedError).String()))
				// Unwrap the error and make sure the code it throws is "Unauthorized"
				var wrappedErr interface{}
				ok := errors.As(e, &wrappedErr)
				Expect(ok).To(BeTrue())
				wrappedErrPtr := reflect.ValueOf(wrappedErr)
				authErrorInterface := reflect.Indirect(wrappedErrPtr).FieldByName("Payload").Interface()
				if reflect.TypeOf(authErrorInterface).String() == "*models.InfraError" {
					authError := authErrorInterface.(*models.InfraError)
					expectedError := int32(http.StatusUnauthorized)
					Expect(authError.Code).To(Equal(&expectedError))
				}
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

var _ = Describe("AuthImageAuth", func() {
	var (
		a        *RHSSOAuthenticator
		infraEnv *common.InfraEnv
		db       *gorm.DB
		dbName   string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		key, err := gencrypto.HMACKey(32)
		Expect(err).ToNot(HaveOccurred())
		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv = &common.InfraEnv{
			InfraEnv:      models.InfraEnv{ID: &infraEnvID},
			ImageTokenKey: key,
		}
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())

		a = &RHSSOAuthenticator{
			log: logrus.New(),
			db:  db,
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("approves a valid token", func() {
		token, err := gencrypto.JWTForSymmetricKey([]byte(infraEnv.ImageTokenKey), 1*time.Hour, infraEnv.ID.String())
		Expect(err).NotTo(HaveOccurred())
		claims, err := a.AuthImageAuth(token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.(jwt.MapClaims)["sub"].(string)).To(Equal(infraEnv.ID.String()))
	})

	It("rejects an expired token", func() {
		token, err := gencrypto.JWTForSymmetricKey([]byte(infraEnv.ImageTokenKey), -1*time.Hour, infraEnv.ID.String())
		Expect(err).NotTo(HaveOccurred())
		_, err = a.AuthImageAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("rejects an incorrectly signed token", func() {
		token, err := gencrypto.JWTForSymmetricKey([]byte(infraEnv.ImageTokenKey), 1*time.Hour, infraEnv.ID.String())
		Expect(err).NotTo(HaveOccurred())
		_, err = a.AuthImageAuth(token + "asdf")
		Expect(err).To(BeAssignableToTypeOf(&common.InfraErrorResponse{}))
		Expect(err.(*common.InfraErrorResponse).StatusCode()).To(Equal(int32(http.StatusUnauthorized)))
	})

	It("rejects a token without the `sub` claim", func() {
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		})
		token, err := t.SignedString([]byte(infraEnv.ImageTokenKey))
		Expect(err).NotTo(HaveOccurred())

		_, err = a.AuthImageAuth(token)
		Expect(err).To(BeAssignableToTypeOf(&common.InfraErrorResponse{}))
		Expect(err.(*common.InfraErrorResponse).StatusCode()).To(Equal(int32(http.StatusUnauthorized)))
	})

	It("rejects a token with a missing infraEnv in the `sub` claim", func() {
		token, err := gencrypto.JWTForSymmetricKey([]byte(infraEnv.ImageTokenKey), 1*time.Hour, uuid.New().String())
		Expect(err).NotTo(HaveOccurred())
		_, err = a.AuthImageAuth(token)
		Expect(err).To(BeAssignableToTypeOf(&common.InfraErrorResponse{}))
		Expect(err.(*common.InfraErrorResponse).StatusCode()).To(Equal(int32(http.StatusUnauthorized)))
	})

	It("rejects a token signed with a different key", func() {
		otherKey, err := gencrypto.HMACKey(32)
		Expect(err).ToNot(HaveOccurred())

		token, err := gencrypto.JWTForSymmetricKey([]byte(otherKey), 1*time.Hour, infraEnv.ID.String())
		Expect(err).NotTo(HaveOccurred())
		_, err = a.AuthImageAuth(token)
		Expect(err).To(BeAssignableToTypeOf(&common.InfraErrorResponse{}))
		Expect(err.(*common.InfraErrorResponse).StatusCode()).To(Equal(int32(http.StatusUnauthorized)))
	})
})
