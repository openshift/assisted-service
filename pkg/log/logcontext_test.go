package log

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"

	"github.com/openshift/assisted-service/client"
	clientInstaller "github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/restapi"
)

var _ restapi.InstallerAPI = FakeInventory{}

func serv(server *http.Server) {
	_ = server.ListenAndServe()
}

type AuthPayload struct {
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Organization string `json:"org_id"`
	Email        string `json:"email"`
	Issuer       string `json:"iss"`
	ClientID     string `json:"clientId"`
	IsAdmin      bool   `json:"is_admin"`
	IsUser       bool   `json:"is_user"`
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

func createServer(logger *logrus.Logger) *http.Server {
	h, _ := restapi.Handler(restapi.Config{
		AuthAgentAuth:       nil,
		AuthUserAuth:        nil,
		APIKeyAuthenticator: authenticator,
		Authorizer:          func(*http.Request) error { return nil },
		InstallerAPI:        FakeInventory{log: logger.WithField("a", "test")},
		EventsAPI:           nil,
		Logger:              logger.Printf,
		VersionsAPI:         nil,
		ManagedDomainsAPI:   nil,
		InnerMiddleware:     ContextHandler(),
	})

	server := &http.Server{Addr: "localhost:8082", Handler: h}
	go serv(server)
	time.Sleep(time.Second * 2) // Allow the server to start
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

func TestTestLogContext(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Log Context Suite")
}

var _ = Describe("Log Fields on Context", func() {
	var (
		logger   *logrus.Logger
		logOut   *bytes.Buffer
		server   *http.Server
		bmclient *client.AssistedInstall
	)

	BeforeEach(func() {
		//create logger for tests with instpectable buffer
		logOut = bytes.NewBuffer(nil)
		logger = logrus.New()
		logger.Out = logOut

		//invoke an http server and a bare metal client
		server = createServer(logger)
		bmclient = createClient()
	})

	AfterEach(func() {
		server.Close()
	})

	Context("cluster_id", func() {
		It("should be populated if there is such route param", func() {
			_, err := bmclient.Installer.GetCluster(context.TODO(), &clientInstaller.GetClusterParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).To(ContainSubstring("cluster_id="))

			fmt.Println(logOut.String())
		})

		It("should not be populated if there is no such route param", func() {
			_, err := bmclient.Installer.ListClusters(context.TODO(), &clientInstaller.ListClustersParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(logOut.String()).NotTo(ContainSubstring("cluster_id="))

			fmt.Println(logOut.String())
		})
	})

})
