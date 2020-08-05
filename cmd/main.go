package main

import (
	"flag"
	"fmt"
	"net/http"
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
	"github.com/openshift/assisted-service/pkg/job"
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
	UseK8s                      bool          `envconfig:"USE_K8S" default:"true"` // TODO remove when jobs running deprecated
	CreateS3Bucket              bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval     time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ImageExpirationTime         time.Duration `envconfig:"IMAGE_EXPIRATION_TIME" default:"60m"`
	ClusterConfig               cluster.Config
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

	s3Client := s3wrapper.NewS3Client(&Options.S3Config, log)
	if s3Client == nil {
		log.Fatal("failed to create S3 client, ", err)
	}

	var kclient client.Client
	if Options.UseK8s {

		if Options.CreateS3Bucket {
			if err = s3Client.CreateBucket(); err != nil {
				log.Fatal(err)
			}
		}

		scheme := runtime.NewScheme()
		if err = clientgoscheme.AddToScheme(scheme); err != nil {
			log.Fatal("Failed to add K8S scheme", err)
		}

		kclient, err = client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
		if err != nil && Options.UseK8s {
			log.Fatal("failed to create client:", err)
		}

	} else {
		log.Println("running drone test, skipping S3")
		kclient = nil
	}

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

	authHandler := auth.NewAuthHandler(Options.Auth, log.WithField("pkg", "auth"))
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

	jobApi := job.New(log.WithField("pkg", "k8s-job-wrapper"), kclient, Options.JobConfig)

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig, jobApi, eventsHandler, s3Client, metricsManager)

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))

	if Options.UseK8s {
		expirer := imgexpirer.NewManager(log, s3Client.Client, Options.S3Config.S3Bucket, Options.ImageExpirationTime, eventsHandler)
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
		InstallerAPI:        bm,
		EventsAPI:           events,
		Logger:              log.Printf,
		VersionsAPI:         versionHandler,
		ManagedDomainsAPI:   domainHandler,
		InnerMiddleware:     metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry),
	})
	h = app.WithMetricsResponderMiddleware(h)
	h = app.WithHealthMiddleware(h)
	// TODO: replace this with real auth
	h = auth.GetUserInfoMiddleware(h)
	h = requestid.Middleware(h)
	if err != nil {
		log.Fatal("Failed to init rest handler,", err)
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}
