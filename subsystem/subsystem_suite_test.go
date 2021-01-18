package subsystem

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/pkg/auth"

	"github.com/openshift/assisted-service/subsystem/apiclient"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/go-openapi/runtime"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/sirupsen/logrus"
)

type AssistedClient struct {
	*client.AssistedInstall
	API apiclient.APIClient
}

var db *gorm.DB
var agentBMClient, badAgentBMClient, userBMClient, readOnlyAdminUserBMClient, unallowedUserBMClient *AssistedClient
var kubeClient *apiclient.KubeAPIClient
var log *logrus.Logger
var wiremock *WireMock

var Options struct {
	DBHost             string `envconfig:"DB_HOST"`
	DBPort             string `envconfig:"DB_PORT"`
	EnableAuth         bool   `envconfig:"ENABLE_AUTH"`
	InventoryHost      string `envconfig:"INVENTORY"`
	TestToken          string `envconfig:"TEST_TOKEN"`
	TestTokenAdmin     string `envconfig:"TEST_TOKEN_ADMIN"`
	TestTokenUnallowed string `envconfig:"TEST_TOKEN_UNALLOWED"`
	OCMHost            string `envconfig:"OCM_HOST"`
	DeployTarget       string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	Namespace          string `envconfig:"NAMESPACE" default:"assisted-installer"`
	EnableKubeAPI      bool   `envconfig:"ENABLE_KUBE_API" default:"false"`
}

func clientcfg(authInfo runtime.ClientAuthInfoWriter) client.Config {
	cfg := client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   Options.InventoryHost,
			Path:   client.DefaultBasePath,
		},
	}
	if Options.EnableAuth {
		log.Info("API Key authentication enabled for subsystem tests")
		cfg.AuthInfo = authInfo
	}
	return cfg
}

func setupKubeAPI() {
	if addErr := adiiov1alpha1.AddToScheme(scheme.Scheme); addErr != nil {
		logrus.Fatalf("Fail adding kubernetes scheme: %s", addErr)
	}

	var err error
	kubeClient, err = apiclient.NewKubeAPIClient(Options.Namespace, config.GetConfigOrDie(), db)
	if err != nil {
		logrus.Fatalf("Fail adding kubernetes client: %s", err)
	}

	// Deploy pull secret once - will be used by all tests
	if _, secretErr := kubeClient.GetSecretRefAndDeployIfNotExists(context.Background(), "pull-secret", pullSecret); secretErr != nil {
		logrus.Fatalf("Fail to deploy pull secret: %s", secretErr)
	}
}

func newAPIClient(cfg client.Config) *AssistedClient {
	var err error
	var apiCli apiclient.APIClient

	ai := client.New(cfg)

	if Options.EnableKubeAPI {
		apiCli, err = apiclient.NewKubeAPIClient(Options.Namespace, config.GetConfigOrDie(), db)
	} else {
		apiCli, err = apiclient.NewRestAPIClient(ai)
	}

	if err != nil {
		logrus.Fatalf("Failed to create api client: %s", err)
	}

	cli := &AssistedClient{
		AssistedInstall: ai,
		API:             apiCli,
	}
	return cli
}

func init() {
	var err error
	log = logrus.New()
	log.SetReportCaller(true)
	err = envconfig.Process("subsystem", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	db, err = gorm.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=admin dbname=installer password=admin sslmode=disable",
			Options.DBHost, Options.DBPort))
	if err != nil {
		logrus.Fatal("Fail to connect to DB, ", err)
	}

	if Options.EnableKubeAPI {
		setupKubeAPI()
	}

	userBMClient = newAPIClient(clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestToken)))
	readOnlyAdminUserBMClient = newAPIClient(clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenAdmin)))
	unallowedUserBMClient = newAPIClient(clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenUnallowed)))
	agentBMClient = newAPIClient(clientcfg(auth.AgentAuthHeaderWriter(FakePS)))
	badAgentBMClient = newAPIClient(clientcfg(auth.AgentAuthHeaderWriter(WrongPullSecret)))

	if Options.EnableAuth {
		wiremock = &WireMock{
			OCMHost:   Options.OCMHost,
			TestToken: Options.TestToken,
		}
		err = wiremock.DeleteAllWiremockStubs()
		if err != nil {
			logrus.Fatal("Fail to delete all wiremock stubs, ", err)
		}

		if err = wiremock.CreateWiremockStubsForOCM(); err != nil {
			logrus.Fatal("Failed to init wiremock stubs, ", err)
		}
	}
}

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	clearDB()
	RunSpecs(t, "Subsystem Suite")
}
