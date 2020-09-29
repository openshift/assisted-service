package log

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/mocks"
	. "github.com/openshift/assisted-service/pkg/context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/runtime/security"

	"github.com/openshift/assisted-service/client"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

func serv(server *http.Server) {
	_ = server.ListenAndServe()
}

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

func createServer(logger *logrus.Logger, installer *restapi.InstallerAPI) *http.Server {
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

	server := &http.Server{Addr: "localhost:8082", Handler: h}
	go serv(server)
	return server
}

func createClient() *client.AssistedInstall {
	cfg := client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   "localhost:8082",
			Path:   client.DefaultBasePath,
		},
	}

	return client.New(cfg)
}

func waitForServer(bmclient *client.AssistedInstall, mockInstallApi *mocks.MockInstallerAPI) {
	var err error = fmt.Errorf("start polling server...")
	//loop up to a second to wait for the server to go up
	mockInstallApi.EXPECT().ListClusters(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
			return installer.NewListClustersOK()
		}).AnyTimes()
	var i int
	for i = 0; i < 100 && err != nil; i++ {
		_, err = bmclient.Installer.ListClusters(context.TODO(), &clientInstaller.ListClustersParams{})
		time.Sleep(time.Millisecond * 10)
	}

	if err != nil {
		panic("server took too long to start " + err.Error())
	}
}

func TestTestLogContext(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Log Context Suite")
}

var _ = Describe("Log Fields on Context", func() {
	var (
		ctrl                *gomock.Controller
		mockInstallApi      *mocks.MockInstallerAPI
		logger              *logrus.Logger
		logOut              *bytes.Buffer
		server              *http.Server
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
		bmclient = createClient()
		waitForServer(bmclient, mockInstallApi)
	})

	AfterEach(func() {
		ctrl.Finish()
		server.Close()
	})

	Context("param", func() {
		It("cluster_id should be populated if there is such route param", func() {

			mockInstallApi.EXPECT().GetCluster(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewGetClusterOK()
				}).Times(1)

			_, err := bmclient.Installer.GetCluster(context.TODO(), &clientInstaller.GetClusterParams{
				ClusterID: cluster_id,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).To(ContainSubstring("cluster_id=" + cluster_id.String()))
		})

		It("host_id should be populated if there is such route param", func() {

			mockInstallApi.EXPECT().EnableHost(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewEnableHostOK()
				}).Times(1)

			_, err := bmclient.Installer.EnableHost(context.TODO(), &clientInstaller.EnableHostParams{
				ClusterID: cluster_id,
				HostID:    host_id,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).To(ContainSubstring("host_id=" + host_id.String()))
		})

		It("should not be populated if there is no such route param", func() {
			mockInstallApi.EXPECT().ListClusters(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
					FromContext(ctx, logger).Info("say something")
					return installer.NewListClustersOK()
				}).AnyTimes()

			_, err := bmclient.Installer.ListClusters(context.TODO(), &clientInstaller.ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).NotTo(ContainSubstring("cluster_id="))
			Expect(logOut.String()).NotTo(ContainSubstring("host_id="))
		})
	})

})
