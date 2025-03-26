package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/kelseyhightower/envconfig"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/subsystem/utils_test"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes/scheme"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var log *logrus.Logger
var wiremock *utils_test.WireMock
var kubeClient k8sclient.Client
var openshiftVersion string = "4.12"
var snoVersion string = "4.12"
var multiarchOpenshiftVersion string = "4.12.0-multi"
var dualstackVipsOpenShiftVersion string = "4.12.0"
var VipAutoAllocOpenshiftVersion string = "4.14.0"
var SDNNetworkTypeOpenshiftVersion string = "4.14.0"
var pullSecret = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}" // #nosec

const (
	pollDefaultInterval = 1 * time.Millisecond
	pollDefaultTimeout  = 30 * time.Second
)

var Options struct {
	DBHost                  string        `envconfig:"DB_HOST"`
	DBPort                  string        `envconfig:"DB_PORT"`
	AuthType                auth.AuthType `envconfig:"AUTH_TYPE"`
	EnableOrgTenancy        bool          `envconfig:"ENABLE_ORG_TENANCY"`
	FeatureGate             bool          `envconfig:"ENABLE_ORG_BASED_FEATURE_GATES"`
	InventoryHost           string        `envconfig:"INVENTORY"`
	TestToken               string        `envconfig:"TEST_TOKEN"`
	TestToken2              string        `envconfig:"TEST_TOKEN_2"`
	TestTokenAdmin          string        `envconfig:"TEST_TOKEN_ADMIN"`
	TestTokenUnallowed      string        `envconfig:"TEST_TOKEN_UNALLOWED"`
	TestTokenClusterEditor  string        `envconfig:"TEST_TOKEN_EDITOR"`
	OCMHost                 string        `envconfig:"OCM_HOST"`
	DeployTarget            string        `envconfig:"DEPLOY_TARGET" default:"k8s"`
	Storage                 string        `envconfig:"STORAGE" default:""`
	Namespace               string        `envconfig:"NAMESPACE" default:"assisted-installer"`
	EnableKubeAPI           bool          `envconfig:"ENABLE_KUBE_API" default:"false"`
	DeregisterInactiveAfter time.Duration `envconfig:"DELETED_INACTIVE_AFTER" default:"480h"` // 20d
	ReleaseSources          string        `envconfig:"RELEASE_SOURCES" default:""`
}

func clientcfg(authInfo runtime.ClientAuthInfoWriter) client.Config {
	cfg := client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   Options.InventoryHost,
			Path:   client.DefaultBasePath,
		},
	}
	if Options.AuthType != auth.TypeNone {
		log.Info("API Key authentication enabled for subsystem tests")
		cfg.AuthInfo = authInfo
	}
	return cfg
}

func setupKubeClient() {
	if addErr := v1beta1.AddToScheme(scheme.Scheme); addErr != nil {
		logrus.Fatalf("Fail adding kubernetes v1beta1 scheme: %s", addErr)
	}
	if addErr := hivev1.AddToScheme(scheme.Scheme); addErr != nil {
		logrus.Fatalf("Fail adding kubernetes hivev1 scheme: %s", addErr)
	}
	if addErr := hiveext.AddToScheme(scheme.Scheme); addErr != nil {
		logrus.Fatalf("Fail adding kubernetes hivev1 scheme: %s", addErr)
	}
	if addErr := bmh_v1alpha1.AddToScheme(scheme.Scheme); addErr != nil {
		logrus.Fatalf("Fail adding kubernetes bmh scheme: %s", addErr)
	}

	var err error
	kubeClient, err = k8sclient.New(config.GetConfigOrDie(), k8sclient.Options{Scheme: scheme.Scheme})
	if err != nil {
		logrus.Fatalf("Fail adding kubernetes client: %s", err)
	}
}

func init() {
	var err error
	log = logrus.New()
	log.SetReportCaller(true)
	err = envconfig.Process("subsystem", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	db, err := gorm.Open(postgres.Open(fmt.Sprintf("host=%s port=%s user=admin database=installer password=admin sslmode=disable",
		Options.DBHost, Options.DBPort)), &gorm.Config{})
	if err != nil {
		logrus.Fatal("Fail to connect to DB, ", err)
	}

	if Options.EnableKubeAPI {
		setupKubeClient()
	}

	userClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestToken))
	userClientCfg2 := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestToken2))
	adminUserClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenAdmin))
	unallowedUserClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenUnallowed))
	editclusterClientCfg := clientcfg(auth.UserAuthHeaderWriter("bearer " + Options.TestTokenClusterEditor))
	agentClientCfg := clientcfg(auth.AgentAuthHeaderWriter(utils_test.FakePS))
	agent2ClientCfg := clientcfg(auth.AgentAuthHeaderWriter(utils_test.FakePS2))
	badAgentClientCfg := clientcfg(auth.AgentAuthHeaderWriter(utils_test.WrongPullSecret))

	utils_test.TestContext = utils_test.NewSubsystemTestContext(
		log,
		db,
		client.New(agentClientCfg),
		client.New(userClientCfg),
		client.New(agent2ClientCfg),
		client.New(userClientCfg2),
		client.New(adminUserClientCfg),
		client.New(unallowedUserClientCfg),
		client.New(editclusterClientCfg),
		client.New(badAgentClientCfg),
		pollDefaultInterval,
		pollDefaultTimeout,
		VipAutoAllocOpenshiftVersion,
	)

	if Options.AuthType == auth.TypeRHSSO {
		releaseSourcesString := os.Getenv("RELEASE_SOURCES")
		var releaseSources = models.ReleaseSources{}
		if err := json.Unmarshal([]byte(releaseSourcesString), &releaseSources); err != nil {
			logrus.Fatal("Fail to parse release sources, ", err)
		}

		wiremock = &utils_test.WireMock{
			OCMHost:        Options.OCMHost,
			TestToken:      Options.TestToken,
			ReleaseSources: releaseSources,
		}

		err := wiremock.DeleteAllWiremockStubs()
		if err != nil {
			logrus.Fatal("Fail to delete all wiremock stubs, ", err)
		}

		if err = wiremock.CreateWiremockStubsForOCM(); err != nil {
			logrus.Fatal("Failed to init wiremock stubs, ", err)
		}
	}

	// Use the default openshift version
	if reply, err := utils_test.TestContext.UserBMClient.Versions.V2ListSupportedOpenshiftVersions(context.Background(),
		&versions.V2ListSupportedOpenshiftVersionsParams{}); err == nil {
		for openshiftVersionString, openshiftVersionStruct := range reply.GetPayload() {
			if openshiftVersionStruct.Default {
				openshiftVersion = openshiftVersionString
				break
			}
		}
	}
}

func TestSubsystem(t *testing.T) {
	AfterEach(func() {
		subsystemAfterEach(utils_test.TestContext)
	})

	RegisterFailHandler(Fail)
	subsystemAfterEach(utils_test.TestContext) // make sure we start tests from scratch
	RunSpecs(t, "Subsystem Suite")
}

func subsystemAfterEach(testContext *utils_test.SubsystemTestContext) {
	testContext.DeregisterResources()
	testContext.ClearDB()
}
