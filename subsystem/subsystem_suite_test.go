package subsystem

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/go-openapi/runtime"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/sirupsen/logrus"
)

var db *gorm.DB
var agentBMClient, badAgentBMClient, userBMClient, adminUserBMClient, unallowedUserBMClient *client.AssistedInstall
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

func init() {
	var err error
	log = logrus.New()
	log.SetReportCaller(true)
	err = envconfig.Process("subsystem", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}
	userClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestToken))
	adminUserClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenAdmin))
	unallowedUserClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenUnallowed))
	agentClientCfg := clientcfg(auth.AgentAuthHeaderWriter(FakePS))
	badAgentClientCfg := clientcfg(auth.AgentAuthHeaderWriter(WrongPullSecret))
	userBMClient = client.New(userClientCfg)
	adminUserBMClient = client.New(adminUserClientCfg)
	unallowedUserBMClient = client.New(unallowedUserClientCfg)
	agentBMClient = client.New(agentClientCfg)
	badAgentBMClient = client.New(badAgentClientCfg)

	db, err = gorm.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=admin dbname=installer password=admin sslmode=disable",
			Options.DBHost, Options.DBPort))
	if err != nil {
		logrus.Fatal("Fail to connect to DB, ", err)
	}

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
