package main

import (
	"context"
	"encoding/json"
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
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/openshift/assisted-service/internal/assistedserviceiso"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/bootfiles"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/connectivity"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/domains"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/migrations"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/spec"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/app"
	"github.com/openshift/assisted-service/pkg/auth"
	paramctx "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/db"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logconfig "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/openshift/assisted-service/restapi"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func init() {
	strfmt.MarshalFormat = strfmt.RFC3339Millis
}

const deployment_type_k8s = "k8s"
const deployment_type_onprem = "onprem"
const deployment_type_ocp = "ocp"

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
	OpenshiftVersions           string        `envconfig:"OPENSHIFT_VERSIONS"`
	ReleaseImageOverride        string        `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE"`
	ReleaseImageMirror          string        `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	CreateS3Bucket              bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval     time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ClusterConfig               cluster.Config
	DeployTarget                string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	OCMConfig                   ocm.Config
	HostConfig                  host.Config
	LogConfig                   logconfig.Config
	LeaderConfig                leader.Config
	DeletionWorkerInterval      time.Duration `envconfig:"DELETION_WORKER_INTERVAL" default:"1h"`
	ValidationsConfig           validations.Config
	AssistedServiceISOConfig    assistedserviceiso.Config
	EnableKubeAPI               bool `envconfig:"ENABLE_KUBE_API" default:"false"`
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

	failOnError := func(err error, msg string, args ...interface{}) {
		if err != nil {
			log.WithError(err).Fatalf(msg, args...)
		}
	}

	port := flag.String("port", "8090", "define port that the service will listen to")
	flag.Parse()

	log.Println("Starting bm service")

	var openshiftVersionsMap models.OpenshiftVersions

	failOnError(json.Unmarshal([]byte(Options.OpenshiftVersions), &openshiftVersionsMap),
		"Failed to parse supported openshift versions JSON %s", Options.OpenshiftVersions)

	log.Println(fmt.Sprintf("Started service with OCP versions %v", openshiftVersionsMap))

	// Connect to db
	db := setupDB(log)
	defer db.Close()

	ctrlMgr, err := createControllerManager()
	failOnError(err, "failed to create controller manager")

	prometheusRegistry := prometheus.DefaultRegisterer
	metricsManager := metrics.NewMetricsManager(prometheusRegistry)

	ocmClient := getOCMClient(log, metricsManager)

	Options.InstructionConfig.ReleaseImageMirror = Options.ReleaseImageMirror
	Options.JobConfig.ReleaseImageMirror = Options.ReleaseImageMirror

	var lead leader.ElectorInterface
	var k8sClient *kubernetes.Clientset
	var autoMigrationLeader leader.ElectorInterface
	authHandler := auth.NewAuthHandler(Options.Auth, ocmClient, log.WithField("pkg", "auth"), db)
	authzHandler := auth.NewAuthzHandler(Options.Auth, ocmClient, log.WithField("pkg", "authz"))
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{})
	versionHandler := versions.NewHandler(log.WithField("pkg", "versions"), releaseHandler,
		Options.Versions, openshiftVersionsMap, Options.ReleaseImageOverride, Options.ReleaseImageMirror)
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	eventsHandler := events.New(db, log.WithField("pkg", "events"))
	hwValidator := hardware.NewValidator(log.WithField("pkg", "validators"), Options.HWValidatorConfig)
	connectivityValidator := connectivity.NewValidator(log.WithField("pkg", "validators"))
	instructionApi := host.NewInstructionManager(log.WithField("pkg", "instructions"), db, hwValidator,
		releaseHandler, Options.InstructionConfig, connectivityValidator, eventsHandler, versionHandler)

	images := []string{
		Options.ReleaseImageMirror,
		Options.BMConfig.AgentDockerImg,
		Options.InstructionConfig.InstallerImage,
		Options.InstructionConfig.ControllerImage,
		Options.InstructionConfig.ConnectivityCheckImage,
		Options.InstructionConfig.InventoryImage,
		Options.InstructionConfig.FreeAddressesImage,
		Options.InstructionConfig.DhcpLeaseAllocatorImage,
		Options.InstructionConfig.APIVIPConnectivityCheckImage,
	}

	for _, ocpVersion := range openshiftVersionsMap {
		images = append(images, ocpVersion.ReleaseImage)
	}

	pullSecretValidator, err := validations.NewPullSecretValidator(Options.ValidationsConfig, images...)
	failOnError(err, "failed to create pull secret validator")

	log.Println("DeployTarget: " + Options.DeployTarget)

	var newUrl string
	newUrl, err = s3wrapper.FixEndpointURL(Options.JobConfig.S3EndpointURL)
	failOnError(err, "failed to create valid job config S3 endpoint URL from %s", Options.JobConfig.S3EndpointURL)
	Options.JobConfig.S3EndpointURL = newUrl

	var generator generator.ISOInstallConfigGenerator
	var objectHandler s3wrapper.API

	failOnError(bmh_v1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme),
		"Failed to add BareMetalHost to scheme")

	var ocpClient k8sclient.K8SClient = nil
	switch Options.DeployTarget {
	case deployment_type_k8s:
		var kclient client.Client

		objectHandler = s3wrapper.NewS3Client(&Options.S3Config, log, versionHandler)
		if objectHandler == nil {
			log.Fatal("failed to create S3 client")
		}
		createS3Bucket(objectHandler)

		kclient, err = client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme.Scheme})
		failOnError(err, "failed to create controller-runtime client")
		generator = job.New(log.WithField("pkg", "k8s-job-wrapper"), kclient, objectHandler, Options.JobConfig)

		cfg, cerr := clientcmd.BuildConfigFromFlags("", "")
		failOnError(cerr, "Failed to create kubernetes cluster config")
		k8sClient = kubernetes.NewForConfigOrDie(cfg)

		autoMigrationLeader = leader.NewElector(k8sClient, leader.Config{LeaseDuration: 5 * time.Second,
			RetryInterval: 2 * time.Second, Namespace: Options.LeaderConfig.Namespace, RenewDeadline: 4 * time.Second},
			"assisted-service-migration-helper",
			log.WithField("pkg", "migrationLeader"))

		lead = leader.NewElector(k8sClient, Options.LeaderConfig, "assisted-service-leader-election-helper",
			log.WithField("pkg", "monitor-runner"))

		failOnError(lead.StartLeaderElection(context.Background()), "Failed to start leader")

		ocpClient, err = k8sclient.NewK8SClient("", log)
		failOnError(err, "Failed to create client for OCP")

	case deployment_type_onprem, deployment_type_ocp:
		lead = &leader.DummyElector{}
		autoMigrationLeader = lead
		// in on-prem mode, setup file system s3 driver and use localjob implementation
		objectHandler = s3wrapper.NewFSClient("/data", log, versionHandler)
		createS3Bucket(objectHandler)
		generator = job.NewLocalJob(log.WithField("pkg", "local-job-wrapper"), Options.JobConfig, versionHandler)
		if Options.DeployTarget == deployment_type_ocp {
			ocpClient, err = k8sclient.NewK8SClient("", log)
			failOnError(err, "Failed to create client for OCP")
		}
	default:
		log.Fatalf("not supported deploy target %s", Options.DeployTarget)
	}

	failOnError(autoMigrationWithLeader(autoMigrationLeader, db, log), "Failed auto migration process")

	hostApi := host.NewManager(log.WithField("pkg", "host-state"), db, eventsHandler, hwValidator,
		instructionApi, &Options.HWValidatorConfig, metricsManager, &Options.HostConfig, lead)
	manifestsApi := manifests.NewManifestsAPI(db, log.WithField("pkg", "manifests"), objectHandler)
	ntpUtils := network.NewNtpUtils(manifestsApi)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager, ntpUtils, lead)
	bootFilesApi := bootfiles.NewBootFilesAPI(log.WithField("pkg", "bootfiles"), objectHandler)

	clusterStateMonitor := thread.New(
		log.WithField("pkg", "cluster-monitor"), "Cluster State Monitor", Options.ClusterStateMonitorInterval, clusterApi.ClusterMonitoring)
	clusterStateMonitor.Start()
	defer clusterStateMonitor.Stop()

	hostStateMonitor := thread.New(
		log.WithField("pkg", "host-monitor"), "Host State Monitor", Options.HostStateMonitorInterval, hostApi.HostMonitoring)
	hostStateMonitor.Start()
	defer hostStateMonitor.Stop()

	newUrl, err = s3wrapper.FixEndpointURL(Options.BMConfig.S3EndpointURL)
	failOnError(err, "failed to create valid bm config S3 endpoint URL from %s", Options.BMConfig.S3EndpointURL)
	Options.BMConfig.S3EndpointURL = newUrl

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig,
		generator, eventsHandler, objectHandler, metricsManager, *authHandler, ocpClient, lead, pullSecretValidator, versionHandler)

	deletionWorker := thread.New(
		log.WithField("inventory", "Deletion Worker"),
		"Deletion Worker",
		Options.DeletionWorkerInterval,
		bm.PermanentlyDeleteUnregisteredClustersAndHosts)
	deletionWorker.Start()
	defer deletionWorker.Stop()

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))
	expirer := imgexpirer.NewManager(objectHandler, eventsHandler, Options.BMConfig.ImageExpirationTime, lead)
	imageExpirationMonitor := thread.New(
		log.WithField("pkg", "image-expiration-monitor"), "Image Expiration Monitor", Options.ImageExpirationInterval, expirer.ExpirationTask)
	imageExpirationMonitor.Start()
	defer imageExpirationMonitor.Stop()
	assistedServiceISO := assistedserviceiso.NewAssistedServiceISOApi(objectHandler, *authHandler, logrus.WithField("pkg", "assistedserviceiso"), pullSecretValidator, Options.AssistedServiceISOConfig)

	//Set inner handler chain. Inner handlers requires access to the Route
	innerHandler := func() func(http.Handler) http.Handler {
		return func(h http.Handler) http.Handler {
			wrapped := metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry)(h)
			wrapped = paramctx.ContextHandler()(wrapped)
			return wrapped
		}
	}

	h, err := restapi.Handler(restapi.Config{
		AuthAgentAuth:         authHandler.AuthAgentAuth,
		AuthUserAuth:          authHandler.AuthUserAuth,
		APIKeyAuthenticator:   authHandler.CreateAuthenticator(),
		Authorizer:            authzHandler.CreateAuthorizer(),
		InstallerAPI:          bm,
		AssistedServiceIsoAPI: assistedServiceISO,
		EventsAPI:             events,
		Logger:                log.Printf,
		VersionsAPI:           versionHandler,
		ManagedDomainsAPI:     domainHandler,
		InnerMiddleware:       innerHandler(),
		ManifestsAPI:          manifestsApi,
		BootfilesAPI:          bootFilesApi,
	})
	failOnError(err, "Failed to init rest handler")

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
	h = spec.WithSpecMiddleware(h)

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		errs, _ := errgroup.WithContext(ctx)
		//cancel the context in case this method ends
		defer cancel()

		switch Options.DeployTarget {
		case deployment_type_k8s:
			baseISOUploadLeader := leader.NewElector(k8sClient, leader.Config{LeaseDuration: 5 * time.Second,
				RetryInterval: 2 * time.Second, Namespace: Options.LeaderConfig.Namespace, RenewDeadline: 4 * time.Second},
				"assisted-service-baseiso-helper",
				log.WithField("pkg", "baseISOUploadLeader"))

			for version := range openshiftVersionsMap {
				currVresion := version
				errs.Go(func() error {
					return errors.Wrapf(baseISOUploadLeader.RunWithLeader(context.Background(), func() error {
						return objectHandler.UploadBootFiles(context.Background(), currVresion)
					}), "Failed uploading boot files for OCP version %s", currVresion)
				})
			}
		case deployment_type_onprem, deployment_type_ocp:
			for version := range openshiftVersionsMap {
				currVresion := version
				errs.Go(func() error {
					return errors.Wrapf(objectHandler.UploadBootFiles(context.Background(), currVresion),
						"Failed uploading boot files for OCP version %s", currVresion)
				})
			}

			if Options.DeployTarget == deployment_type_ocp {
				errs.Go(func() error {
					return errors.Wrapf(bm.RegisterOCPCluster(context.Background()), "Failed to create OCP cluster")
				})
			}
		}

		if err = errs.Wait(); err != nil {
			failOnError(err, "Failed to make API ready")
		}

		apiEnabler.Enable()
	}()

	go func() {
		if Options.EnableKubeAPI {
			failOnError((&controllers.ImageReconciler{
				Client:    ctrlMgr.GetClient(),
				Log:       log,
				Scheme:    ctrlMgr.GetScheme(),
				Installer: bm,
			}).SetupWithManager(ctrlMgr), "unable to create controller Image")

			failOnError((&controllers.ClusterReconciler{
				Client:    ctrlMgr.GetClient(),
				Log:       log,
				Scheme:    ctrlMgr.GetScheme(),
				Installer: bm,
			}).SetupWithManager(ctrlMgr), "unable to create controller Cluster")

			failOnError((&controllers.HostReconciler{
				Client: ctrlMgr.GetClient(),
				Log:    log,
				Scheme: ctrlMgr.GetScheme(),
			}).SetupWithManager(ctrlMgr), "unable to create controller Host")

			failOnError(ctrlMgr.Start(ctrl.SetupSignalHandler()), "failed to run manager")
		}
	}()

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}

func setupDB(log logrus.FieldLogger) *gorm.DB {
	dbConnectionStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=disable",
		Options.DBConfig.Host, Options.DBConfig.Port, Options.DBConfig.User, Options.DBConfig.Name, Options.DBConfig.Pass)
	db, err := gorm.Open("postgres", dbConnectionStr)
	if err != nil {
		log.WithError(err).Fatal("Fail to connect to DB")
	}
	db.DB().SetMaxIdleConns(0)
	db.DB().SetMaxOpenConns(0)
	db.DB().SetConnMaxLifetime(0)
	return db
}

func getOCMClient(log logrus.FieldLogger, metrics metrics.API) *ocm.Client {
	var ocmClient *ocm.Client
	var err error
	if Options.Auth.EnableAuth {
		ocmClient, err = ocm.NewClient(Options.OCMConfig, log.WithField("pkg", "ocm"), metrics)
		if err != nil {
			log.WithError(err).Fatal("Failed to Create OCM Client")
		}
	}
	return ocmClient
}

func createS3Bucket(objectHandler s3wrapper.API) {
	if Options.CreateS3Bucket {
		if err := objectHandler.CreateBucket(); err != nil {
			log.Fatal(err)
		}
		if err := objectHandler.CreatePublicBucket(); err != nil {
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
		if err != nil {
			log.WithError(err).Fatal("Failed auto migration process")
			return err
		}
		log.Info("Finished automigration")

		log.Infof("Starting manual migrations")
		err = migrations.Migrate(db)
		if err != nil {
			log.WithError(err).Fatal("Manual migration process failed")
			return err
		}
		log.Info("Finished manual migrations")

		return nil
	})
}

func createControllerManager() (manager.Manager, error) {
	if Options.EnableKubeAPI {
		var schemes = runtime.NewScheme()
		utilruntime.Must(scheme.AddToScheme(schemes))
		utilruntime.Must(adiiov1alpha1.AddToScheme(schemes))

		syncPeriod := 10 * time.Second

		return ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			Scheme:           schemes,
			Port:             9443,
			LeaderElection:   true,
			LeaderElectionID: "77190dcb.my.domain",
			// TODO: remove it after we have backend notifications
			SyncPeriod: &syncPeriod,
		})
	}
	return nil, nil
}
