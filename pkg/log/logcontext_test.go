package log

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/runtime/security"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/mocks"
	. "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
)

type AuthPayload struct {
	Username string `json:"username"`
	IsUser   bool   `json:"is_user"`
}

var testUser = AuthPayload{
	IsUser:   true,
	Username: "dummy",
}

var authenticator = func(name string, _ string, authenticate security.TokenAuthentication) runtime.Authenticator {
	return security.HttpAuthenticator(func(r *http.Request) (bool, interface{}, error) {
		return true, &testUser, nil
	})
}

func createServer(logger *logrus.Logger, installer *restapi.InstallerAPI) *httptest.Server {
	h, _ := restapi.Handler(restapi.Config{
		AuthAgentAuth:       nil,
		AuthUserAuth:        nil,
		APIKeyAuthenticator: authenticator,
		Authorizer:          func(*http.Request) error { return nil },
		InstallerAPI:        *installer,
		EventsAPI:           nil,
		Logger:              logger.Printf,
		VersionsAPI:         nil,
		ManagedDomainsAPI:   nil,
		InnerMiddleware:     ContextHandler(),
	})

	server := httptest.NewServer(h)
	return server
}

func createClient(srvURL string) *client.AssistedInstall {
	cfg := client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   strings.TrimPrefix(srvURL, "http://"),
			Path:   client.DefaultBasePath,
		},
	}

	return client.New(cfg)
}

func TestLogContext(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Log Context Suite")
}

var _ = Describe("Log Fields on Context", func() {
	var (
		ctrl                *gomock.Controller
		mockInstallApi      *mocks.MockInstallerAPI
		logger              *logrus.Logger
		logOut              *bytes.Buffer
		server              *httptest.Server
		bmclient            *client.AssistedInstall
		cluster_id, host_id strfmt.UUID
	)

	BeforeEach(func() {
		//create mocks for InstallerAPI
		ctrl = gomock.NewController(GinkgoT())
		mockInstallApi = mocks.NewMockInstallerAPI(ctrl)

		//generate ids for verification
		cluster_id = strfmt.UUID(uuid.New().String())
		host_id = strfmt.UUID(uuid.New().String())

		//create logger for tests with instpectable buffer
		logOut = bytes.NewBuffer(nil)
		logger = logrus.New()
		logger.Out = logOut
		//invoke an http server and a bare metal client
		var installer restapi.InstallerAPI = restapi.InstallerAPI(mockInstallApi)
		server = createServer(logger, &installer)
		bmclient = createClient(server.URL)
	})

	AfterEach(func() {
		ctrl.Finish()
		server.Close()
	})

	Context("param", func() {
		It("cluster_id should be populated if there is such route param", func() {

			mockInstallApi.EXPECT().V2GetCluster(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.V2GetClusterParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewV2GetClusterOK()
				}).Times(1)

			_, err := bmclient.Installer.V2GetCluster(context.TODO(), &clientInstaller.V2GetClusterParams{
				ClusterID: cluster_id,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).To(ContainSubstring("cluster_id=" + cluster_id.String()))
		})

		It("host_id should be populated if there is such route param", func() {

			mockInstallApi.EXPECT().V2GetHost(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.V2GetHostParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewV2GetHostOK()
				}).Times(1)

			_, err := bmclient.Installer.V2GetHost(context.TODO(), &clientInstaller.V2GetHostParams{
				InfraEnvID: cluster_id,
				HostID:     host_id,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).To(ContainSubstring("host_id=" + host_id.String()))
		})

		It("should not be populated if there is no such route param", func() {
			mockInstallApi.EXPECT().V2ListClusters(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.V2ListClustersParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewV2ListClustersOK()
				}).AnyTimes()

			_, err := bmclient.Installer.V2ListClusters(context.TODO(), &clientInstaller.V2ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).NotTo(ContainSubstring("cluster_id="))
			Expect(logOut.String()).NotTo(ContainSubstring("host_id="))
		})
	})

})
