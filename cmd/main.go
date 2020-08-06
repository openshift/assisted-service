package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/app"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/db"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/openshift/assisted-service/restapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func init() {
	strfmt.MarshalFormat = strfmt.ISO8601LocalTime
}

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
}

func main() {
	log := logrus.New()
	log.SetReportCaller(true)

	err := envconfig.Process("myapp", &Options)
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

	ocmClient, err := ocm.NewClient(Options.OCMConfig)

	if err != nil {
		log.Warn("Failed to Create OCM Client,", err)
	}

	authHandler := auth.NewAuthHandler(Options.Auth, ocmClient, log.WithField("pkg", "auth"), db)
	versionHandler := versions.NewHandler(Options.Versions)
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	eventsHandler := events.New(db, log.WithField("pkg", "events"))
	hwValidator := hardware.NewValidator(log.WithField("pkg", "validators"), Options.HWValidatorConfig)
	connectivityValidator := connectivity.NewValidator(log.WithField("pkg", "validators"))
	instructionApi := host.NewInstructionManager(log.WithField("pkg", "instructions"), db, hwValidator, Options.InstructionConfig, connectivityValidator)
	prometheusRegistry := prometheus.DefaultRegisterer
	metricsManager := metrics.NewMetricsManager(prometheusRegistry)
	hostApi := host.NewManager(log.WithField("pkg", "host-state"), db, eventsHandler, hwValidator, instructionApi, &Options.HWValidatorConfig, metricsManager)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager)

	clusterStateMonitor := thread.New(
		log.WithField("pkg", "cluster-monitor"), "Cluster State Monitor", Options.ClusterStateMonitorInterval, clusterApi.ClusterMonitoring)
	clusterStateMonitor.Start()
	defer clusterStateMonitor.Stop()

	hostStateMonitor := thread.New(
		log.WithField("pkg", "host-monitor"), "Host State Monitor", Options.HostStateMonitorInterval, hostApi.HostMonitoring)
	hostStateMonitor.Start()
	defer hostStateMonitor.Stop()

	s3Client := s3wrapper.NewS3Client(&Options.S3Config, log)
	if s3Client == nil {
		log.Fatal("failed to create S3 client, ", err)
	}

	log.Println("DeployTarget: " + Options.DeployTarget)

	var generator generator.ISOInstallConfigGenerator

	switch Options.DeployTarget {
	case "k8s":
		var kclient client.Client
		createS3Bucket(s3Client)

		scheme := runtime.NewScheme()
		if err = clientgoscheme.AddToScheme(scheme); err != nil {
			log.Fatal("Failed to add K8S scheme", err)
		}

		kclient, err = client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
		if err != nil {
			log.Fatal("failed to create client:", err)
		}
		generator = job.New(log.WithField("pkg", "k8s-job-wrapper"), kclient, Options.JobConfig)
	case "onprem":
		// in on-prem mode, setup s3 and use localjob implementation
		createS3Bucket(s3Client)
		generator = job.NewLocalJob(log.WithField("pkg", "local-job-wrapper"), Options.JobConfig)
	default:
		log.Fatalf("not supported deploy target %s", Options.DeployTarget)
	}

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig, generator, eventsHandler, s3Client, metricsManager)

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))

	if Options.DeployTarget == "k8s" {
		expirer := imgexpirer.NewManager(log, s3Client.Client, Options.S3Config.S3Bucket, Options.BMConfig.ImageExpirationTime, eventsHandler)
		imageExpirationMonitor := thread.New(
			log.WithField("pkg", "image-expiration-monitor"), "Image Expiration Monitor", Options.ImageExpirationInterval, expirer.ExpirationTask)
		imageExpirationMonitor.Start()
		defer imageExpirationMonitor.Stop()
	} else {
		log.Info("Disabled image expiration monitor")
	}

	h, err := restapi.Handler(restapi.Config{
		AuthAgentAuth:       authHandler.AuthAgentAuth,
		AuthUserAuth:        authHandler.AuthUserAuth,
		APIKeyAuthenticator: authHandler.CreateAuthenticator(),
		Authorizer:          authHandler.CreateAuthorizer(),
		InstallerAPI:        bm,
		EventsAPI:           events,
		Logger:              log.Printf,
		VersionsAPI:         versionHandler,
		ManagedDomainsAPI:   domainHandler,
		InnerMiddleware:     metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry),
	})
	if Options.Auth.AllowedDomains != "" {
		allowedDomains := strings.Split(strings.ReplaceAll(Options.Auth.AllowedDomains, " ", ""), ",")
		log.Infof("AllowedDomains were provided, enabling CORS with %s as domain list", allowedDomains)
		// enabling CORS with given domain list
		h = app.SetupCORSMiddleware(h, allowedDomains)
	}

	h = app.WithMetricsResponderMiddleware(h)
	h = app.WithHealthMiddleware(h)
	h = requestid.Middleware(h)
	if err != nil {
		log.Fatal("Failed to init rest handler,", err)
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}

func createS3Bucket(s3Client *s3wrapper.S3Client) {
	if Options.CreateS3Bucket {
		if err := s3Client.CreateBucket(); err != nil {
			log.Fatal(err)
		}
	}
}
