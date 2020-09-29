package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/openshift/assisted-service/pkg/thread"

	"github.com/openshift/assisted-service/internal/imgexpirer"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/internal/domains"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/app"
	"github.com/openshift/assisted-service/pkg/auth"
	paramctx "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/db"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/leader"
	logconfig "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func init() {
	strfmt.MarshalFormat = strfmt.RFC3339Millis
}

const deploymet_type_k8s = "k8s"

var Options struct {
	Auth                        auth.Config
	BMConfig                    bminventory.Config
	DBConfig                    db.Config
	HWValidatorConfig           hardware.ValidatorCfg
	JobConfig                   job.Config
	InstructionConfig           host.InstructionConfig
	ClusterStateMonitorInterval time.Duration `envconfig:"CLUSTER_MONITOR_INTERVAL" default:"10s"`
	S3Config                    s3wrapper.Config
	HostStateMonitorInterval    time.Duration `envconfig:"HOST_MONITOR_INTERVAL" default:"8s"`
	Versions                    versions.Versions
	CreateS3Bucket              bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval     time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ClusterConfig               cluster.Config
	DeployTarget                string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	OCMConfig                   ocm.Config
	HostConfig                  host.Config
	LogConfig                   logconfig.Config
	LeaderConfig                leader.Config
}

func InitLogs() *logrus.Entry {
	log := logrus.New()
	log.SetReportCaller(true)

	logger := log.WithFields(logrus.Fields{})

	//set log format according to configuration
	logger.Info("Setting log format: ", Options.LogConfig.LogFormat)
	if Options.LogConfig.LogFormat == logconfig.LogFormatJson {
		log.SetFormatter(&logrus.JSONFormatter{})
	}

	//set log level according to configuration
	logger.Info("Setting Log Level: ", Options.LogConfig.LogLevel)
	logLevel, err := logrus.ParseLevel(Options.LogConfig.LogLevel)
	if err != nil {
		logger.Error("Invalid Log Level: ", Options.LogConfig.LogLevel)
	} else {
		log.SetLevel(logLevel)
	}

	return logger
}

func main() {
	err := envconfig.Process("myapp", &Options)
	log := InitLogs()

	if err != nil {
		log.Fatal(err.Error())
	}

	port := flag.String("port", "8090", "define port that the service will listen to")
	flag.Parse()

	log.Println("Starting bm service")

	// Connect to db
	dbConnectionStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=disable",
		Options.DBConfig.Host, Options.DBConfig.Port, Options.DBConfig.User, Options.DBConfig.Name, Options.DBConfig.Pass)
	db, err := gorm.Open("postgres", dbConnectionStr)
	if err != nil {
		log.Fatal("Fail to connect to DB, ", err)
	}
	defer db.Close()
	db.DB().SetMaxIdleConns(0)
	db.DB().SetMaxOpenConns(0)
	db.DB().SetConnMaxLifetime(0)

	if err = db.AutoMigrate(&models.Host{}, &common.Cluster{}, &events.Event{}).Error; err != nil {
		log.Fatal("failed to auto migrate, ", err)
	}

	var ocmClient *ocm.Client
	if Options.Auth.EnableAuth {
		ocmLog := logrus.New()
		ocmClient, err = ocm.NewClient(Options.OCMConfig, ocmLog.WithField("pkg", "ocm"))
		if err != nil {
			log.Fatal("Failed to Create OCM Client, ", err)
		}
	}

	var lead leader.ElectorInterface
	var autoMigrationLeader leader.ElectorInterface
	authHandler := auth.NewAuthHandler(Options.Auth, ocmClient, log.WithField("pkg", "auth"))
	authzHandler := auth.NewAuthzHandler(Options.Auth, ocmClient, log.WithField("pkg", "authz"))
	versionHandler := versions.NewHandler(Options.Versions)
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	eventsHandler := events.New(db, log.WithField("pkg", "events"))
	hwValidator := hardware.NewValidator(log.WithField("pkg", "validators"), Options.HWValidatorConfig)
	connectivityValidator := connectivity.NewValidator(log.WithField("pkg", "validators"))
	instructionApi := host.NewInstructionManager(log.WithField("pkg", "instructions"), db, hwValidator, Options.InstructionConfig, connectivityValidator)
	prometheusRegistry := prometheus.DefaultRegisterer
	metricsManager := metrics.NewMetricsManager(prometheusRegistry)

	log.Println("DeployTarget: " + Options.DeployTarget)

	var newUrl string
	if newUrl, err = s3wrapper.FixEndpointURL(Options.JobConfig.S3EndpointURL); err != nil {
		log.WithError(err).Fatalf("failed to create valid job config S3 endpoint URL from %s", Options.JobConfig.S3EndpointURL)
	} else {
		Options.JobConfig.S3EndpointURL = newUrl
	}

	var generator generator.ISOInstallConfigGenerator
	var objectHandler s3wrapper.API

	switch Options.DeployTarget {
	case deploymet_type_k8s:
		var kclient client.Client

		objectHandler = s3wrapper.NewS3Client(&Options.S3Config, log)
		if objectHandler == nil {
			log.Fatal("failed to create S3 client, ", err)
		}
		createS3Bucket(objectHandler)

		scheme := runtime.NewScheme()
		if err = clientgoscheme.AddToScheme(scheme); err != nil {
			log.Fatal("Failed to add K8S scheme", err)
		}

		kclient, err = client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
		if err != nil {
			log.Fatal("failed to create client:", err)
		}
		generator = job.New(log.WithField("pkg", "k8s-job-wrapper"), kclient, Options.JobConfig)

		cfg, cerr := clientcmd.BuildConfigFromFlags("", "")
		if cerr != nil {
			log.WithError(cerr).Fatalf("Failed to create kubernetes cluster config")
		}
		k8sClient := kubernetes.NewForConfigOrDie(cfg)

		autoMigrationLeader = leader.NewElector(k8sClient, leader.Config{LeaseDuration: 5 * time.Second,
			RetryInterval: 2 * time.Second, Namespace: Options.LeaderConfig.Namespace, RenewDeadline: 4 * time.Second},
			"assisted-service-migration-helper",
			log.WithField("pkg", "migrationLeader"))

		lead = leader.NewElector(k8sClient, Options.LeaderConfig, "assisted-service-leader-election-helper",
			log.WithField("pkg", "monitor-runner"))

		err = lead.StartLeaderElection(context.Background())
		if err != nil {
			log.WithError(err).Fatalf("Failed to start leader")
		}

	case "onprem":
		lead = &leader.DummyElector{}
		autoMigrationLeader = lead
		// in on-prem mode, setup file system s3 driver and use localjob implementation
		objectHandler = s3wrapper.NewFSClient("/data", log)
		if objectHandler == nil {
			log.Fatal("failed to create S3 file system client, ", err)
		}
		createS3Bucket(objectHandler)
		generator = job.NewLocalJob(log.WithField("pkg", "local-job-wrapper"), Options.JobConfig)
	default:
		log.Fatalf("not supported deploy target %s", Options.DeployTarget)
	}

	err = autoMigrationWithLeader(autoMigrationLeader, db, log)
	if err != nil {
		log.WithError(err).Fatal("Failed auto migration process")
	}

	hostApi := host.NewManager(log.WithField("pkg", "host-state"), db, eventsHandler, hwValidator,
		instructionApi, &Options.HWValidatorConfig, metricsManager, &Options.HostConfig, lead)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager, lead)

	clusterStateMonitor := thread.New(
		log.WithField("pkg", "cluster-monitor"), "Cluster State Monitor", Options.ClusterStateMonitorInterval, clusterApi.ClusterMonitoring)
	clusterStateMonitor.Start()
	defer clusterStateMonitor.Stop()

	hostStateMonitor := thread.New(
		log.WithField("pkg", "host-monitor"), "Host State Monitor", Options.HostStateMonitorInterval, hostApi.HostMonitoring)
	hostStateMonitor.Start()
	defer hostStateMonitor.Stop()

	if newUrl, err = s3wrapper.FixEndpointURL(Options.BMConfig.S3EndpointURL); err != nil {
		log.WithError(err).Fatalf("failed to create valid bm config S3 endpoint URL from %s", Options.BMConfig.S3EndpointURL)
	} else {
		Options.BMConfig.S3EndpointURL = newUrl
	}

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig,
		generator, eventsHandler, objectHandler, metricsManager, *authHandler)

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))

	expirer := imgexpirer.NewManager(objectHandler, eventsHandler, Options.BMConfig.ImageExpirationTime, lead)
	imageExpirationMonitor := thread.New(
		log.WithField("pkg", "image-expiration-monitor"), "Image Expiration Monitor", Options.ImageExpirationInterval, expirer.ExpirationTask)
	imageExpirationMonitor.Start()
	defer imageExpirationMonitor.Stop()

	//Set inner handler chain. Inner handlers requires access to the Route
	innerHandler := func() func(http.Handler) http.Handler {
		return func(h http.Handler) http.Handler {
			wrapped := metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry)(h)
			wrapped = paramctx.ContextHandler()(wrapped)
			return wrapped
		}
	}

	h, err := restapi.Handler(restapi.Config{
		AuthAgentAuth:       authHandler.AuthAgentAuth,
		AuthUserAuth:        authHandler.AuthUserAuth,
		APIKeyAuthenticator: authHandler.CreateAuthenticator(),
		Authorizer:          authzHandler.CreateAuthorizer(),
		InstallerAPI:        bm,
		EventsAPI:           events,
		Logger:              log.Printf,
		VersionsAPI:         versionHandler,
		ManagedDomainsAPI:   domainHandler,
		InnerMiddleware:     innerHandler(),
	})
	if err != nil {
		log.Fatal("Failed to init rest handler,", err)
	}

	if Options.Auth.AllowedDomains != "" {
		allowedDomains := strings.Split(strings.ReplaceAll(Options.Auth.AllowedDomains, " ", ""), ",")
		log.Infof("AllowedDomains were provided, enabling CORS with %s as domain list", allowedDomains)
		// enabling CORS with given domain list
		h = app.SetupCORSMiddleware(h, allowedDomains)
	}

	h = app.WithMetricsResponderMiddleware(h)
	apiEnabler := NewApiEnabler(h, log)
	h = app.WithHealthMiddleware(apiEnabler)
	h = requestid.Middleware(h)

	if Options.DeployTarget == deploymet_type_k8s {
		go func() {
			defer apiEnabler.Enable()
			//Run first ISO dummy for image pull, this is done so that the image will be pulled and the api will take less time.
			// blocking function that can take a long time.
			bminventory.GenerateDummyISOImage(log, generator, eventsHandler)
		}()
	} else {
		apiEnabler.Enable()
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}

func createS3Bucket(objectHandler s3wrapper.API) {
	if Options.CreateS3Bucket {
		if err := objectHandler.CreateBucket(); err != nil {
			log.Fatal(err)
		}
	}
}

func NewApiEnabler(h http.Handler, log logrus.FieldLogger) *ApiEnabler {
	return &ApiEnabler{
		log:       log,
		isEnabled: false,
		inner:     h,
	}
}

type ApiEnabler struct {
	log       logrus.FieldLogger
	isEnabled bool
	inner     http.Handler
}

func (a *ApiEnabler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !a.isEnabled {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	} else if r.Method == http.MethodGet && r.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		return
	}
	a.inner.ServeHTTP(w, r)
}
func (a *ApiEnabler) Enable() {
	a.isEnabled = true
	a.log.Info("API is enabled")
}

func autoMigrationWithLeader(migrationLeader leader.ElectorInterface, db *gorm.DB, log logrus.FieldLogger) error {
	return migrationLeader.RunWithLeader(context.Background(), func() error {
		log.Infof("Start automigration")
		err := db.AutoMigrate(&models.Host{}, &common.Cluster{}, &events.Event{}).Error
		log.Infof("Finish automigration")
		return err
	})
}
