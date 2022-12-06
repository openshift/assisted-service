package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/kelseyhightower/envconfig"
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-image-service/pkg/servers"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/domains"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/infraenv"
	installcfg "github.com/openshift/assisted-service/internal/installcfg/builder"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/migrations"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/handler"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/spec"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/app"
	"github.com/openshift/assisted-service/pkg/auth"
	paramctx "github.com/openshift/assisted-service/pkg/context"
	dbPkg "github.com/openshift/assisted-service/pkg/db"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logconfig "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/openshift/assisted-service/restapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.elastic.co/apm/module/apmhttp"
	"go.elastic.co/apm/module/apmlogrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func init() {
	strfmt.MarshalFormat = strfmt.RFC3339Millis
}

const (
	deployment_type_k8s    = "k8s"
	deployment_type_onprem = "onprem"
	deployment_type_ocp    = "ocp"
	storage_filesystem     = "filesystem"
	storage_s3             = "s3"
)

var Options struct {
	Auth                           auth.Config
	BMConfig                       bminventory.Config
	DBConfig                       dbPkg.Config
	HWValidatorConfig              hardware.ValidatorCfg
	GeneratorConfig                generator.Config
	InstructionConfig              hostcommands.InstructionConfig
	OperatorsConfig                operators.Options
	GCConfig                       garbagecollector.Config
	StaticNetworkConfig            staticnetworkconfig.Config
	ClusterStateMonitorInterval    time.Duration `envconfig:"CLUSTER_MONITOR_INTERVAL" default:"10s"`
	S3Config                       s3wrapper.Config
	HostStateMonitorInterval       time.Duration `envconfig:"HOST_MONITOR_INTERVAL" default:"8s"`
	Versions                       versions.Versions
	OsImages                       string        `envconfig:"OS_IMAGES" default:""`
	ReleaseImages                  string        `envconfig:"RELEASE_IMAGES" default:""`
	MustGatherImages               string        `envconfig:"MUST_GATHER_IMAGES" default:""`
	ReleaseImageMirror             string        `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	CreateS3Bucket                 bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval        time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ClusterConfig                  cluster.Config
	DeployTarget                   string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	Storage                        string `envconfig:"STORAGE" default:"s3"`
	OCMConfig                      ocm.Config
	HostConfig                     host.Config
	LogConfig                      logconfig.Config
	LeaderConfig                   leader.Config
	ValidationsConfig              validations.Config
	ManifestsGeneratorConfig       network.Config
	EnableKubeAPI                  bool `envconfig:"ENABLE_KUBE_API" default:"false"`
	InfraEnvConfig                 controllers.InfraEnvConfig
	CheckClusterVersion            bool          `envconfig:"CHECK_CLUSTER_VERSION" default:"false"`
	DeletionWorkerInterval         time.Duration `envconfig:"DELETION_WORKER_INTERVAL" default:"1h"`
	InfraEnvDeletionWorkerInterval time.Duration `envconfig:"INFRAENV_DELETION_WORKER_INTERVAL" default:"1h"`
	DeregisterWorkerInterval       time.Duration `envconfig:"DEREGISTER_WORKER_INTERVAL" default:"1h"`
	EnableDeletedUnregisteredGC    bool          `envconfig:"ENABLE_DELETE_UNREGISTER_GC" default:"true"`
	EnableDeregisterInactiveGC     bool          `envconfig:"ENABLE_DEREGISTER_INACTIVE_GC" default:"true"`
	ServeHTTPS                     bool          `envconfig:"SERVE_HTTPS" default:"false"`
	HTTPSKeyFile                   string        `envconfig:"HTTPS_KEY_FILE" default:""`
	HTTPSCertFile                  string        `envconfig:"HTTPS_CERT_FILE" default:""`
	MaxIdleConns                   int           `envconfig:"DB_MAX_IDLE_CONNECTIONS" default:"50"`
	MaxOpenConns                   int           `envconfig:"DB_MAX_OPEN_CONNECTIONS" default:"90"`
	ConnMaxLifetime                time.Duration `envconfig:"DB_CONNECTIONS_MAX_LIFETIME" default:"30m"`
	FileSystemUsageThreshold       int           `envconfig:"FILESYSTEM_USAGE_THRESHOLD" default:"80"`
	EnableElasticAPM               bool          `envconfig:"ENABLE_ELASTIC_APM" default:"false"`
	WorkDir                        string        `envconfig:"WORK_DIR" default:"/data/"`
	LivenessValidationTimeout      time.Duration `envconfig:"LIVENESS_VALIDATION_TIMEOUT" default:"5m"`
	ApproveCsrsRequeueDuration     time.Duration `envconfig:"APPROVE_CSRS_REQUEUE_DURATION" default:"1m"`
	HTTPListenPort                 string        `envconfig:"HTTP_LISTEN_PORT" default:""`
	AllowConvergedFlow             bool          `envconfig:"ALLOW_CONVERGED_FLOW" default:"false"` // set to true once https://bugzilla.redhat.com/show_bug.cgi?id=2089683 is resolved
	IronicIgnitionBuilderConfig    ignition.IronicIgniotionBuilderConfig

	// Directory containing pre-generated TLS certs/keys for the ephemeral installer
	ClusterTLSCertOverrideDir string `envconfig:"EPHEMERAL_INSTALLER_CLUSTER_TLS_CERTS_OVERRIDE_DIR" default:""`
}

func InitLogs() *logrus.Entry {
	log := logrus.New()

	fmt.Println(Options.EnableElasticAPM)
	if Options.EnableElasticAPM {
		log.AddHook(&apmlogrus.Hook{})
	}

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

func maxDuration(dur time.Duration, durations ...time.Duration) time.Duration {
	ret := dur
	for _, d := range durations {
		if d > ret {
			ret = d
		}
	}
	return ret
}

func main() {
	err := envconfig.Process(common.EnvConfigPrefix, &Options)
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

	if Options.BMConfig.ImageServiceBaseURL == "" {
		log.Fatal("IMAGE_SERVICE_BASE_URL is required")
	}

	var osImagesArray models.OsImages
	if Options.OsImages == "" {
		log.Fatal("OS_IMAGES list is empty")
		osImagesArray = models.OsImages{}
	} else {
		failOnError(json.Unmarshal([]byte(Options.OsImages), &osImagesArray),
			"Failed to parse OS_IMAGES json %s", Options.OsImages)
	}

	var releaseImagesArray models.ReleaseImages
	if Options.ReleaseImages == "" {
		// ReleaseImages is optional (not used by the operator)
		releaseImagesArray = models.ReleaseImages{}
	} else {
		failOnError(json.Unmarshal([]byte(Options.ReleaseImages), &releaseImagesArray),
			"Failed to parse RELEASE_IMAGES json %s", Options.ReleaseImages)
	}

	log.Println(fmt.Sprintf("Started service with OS images %v, Release images %v",
		Options.OsImages, Options.ReleaseImages))

	var mustGatherVersionsMap = make(versions.MustGatherVersions)
	if Options.MustGatherImages != "" {
		failOnError(json.Unmarshal([]byte(Options.MustGatherImages), &mustGatherVersionsMap),
			"Failed to parse feature must-gather images JSON %s", Options.MustGatherImages)
	}

	failOnError(os.MkdirAll(Options.BMConfig.ISOCacheDir, 0700), "Failed to create ISO cache directory %s", Options.BMConfig.ISOCacheDir)

	// Connect to db
	db := setupDB(log)
	defer common.CloseDB(db)

	ctrlMgr, err := createControllerManager()
	failOnError(err, "failed to create controller manager")

	usageManager := usage.NewManager(log)
	ocmClient := getOCMClient(log)

	authHandler, err := auth.NewAuthenticator(&Options.Auth, ocmClient, log.WithField("pkg", "auth"), db)
	failOnError(err, "failed to create authenticator")
	authzHandler := auth.NewAuthzHandler(&Options.Auth, ocmClient, log.WithField("pkg", "authz"), db)

	crdEventsHandler := createCRDEventsHandler()
	eventsHandler := createEventsHandler(crdEventsHandler, db, authzHandler, log)

	prometheusRegistry := prometheus.DefaultRegisterer
	metricsManager := metrics.NewMetricsManager(prometheusRegistry, eventsHandler)
	if ocmClient != nil {
		//inject the metric server to the ocm client for purpose of
		//performance monitoring the calls to ACM. This could not be done
		//in the constructor due to a cyclic dependency with the event handler
		ocmClient.SetMetrics(metricsManager)
	}

	Options.InstructionConfig.ReleaseImageMirror = Options.ReleaseImageMirror
	Options.InstructionConfig.CheckClusterVersion = Options.CheckClusterVersion
	Options.OperatorsConfig.CheckClusterVersion = Options.CheckClusterVersion
	Options.GeneratorConfig.ReleaseImageMirror = Options.ReleaseImageMirror
	//Initialize Provider API
	providerRegistry := registry.InitProviderRegistry(log.WithField("pkg", "provider"))
	// Make sure that prepare for installation timeout is more than the timeouts of all underlying tools + 2m extra
	Options.ClusterConfig.PrepareConfig.PrepareForInstallationTimeout = maxDuration(Options.ClusterConfig.PrepareConfig.PrepareForInstallationTimeout,
		maxDuration(Options.InstructionConfig.DiskCheckTimeout, Options.InstructionConfig.ImageAvailabilityTimeout)+2*time.Minute)
	var lead leader.ElectorInterface
	var k8sClient *kubernetes.Clientset
	var autoMigrationLeader leader.ElectorInterface

	mirrorRegistriesBuilder := mirrorregistries.New()
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay}, mirrorRegistriesBuilder)
	extracterHandler := oc.NewExtracter(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay})
	versionHandler, err := versions.NewHandler(log.WithField("pkg", "versions"), releaseHandler,
		Options.Versions, osImagesArray, releaseImagesArray, mustGatherVersionsMap, Options.ReleaseImageMirror)
	failOnError(err, "failed to create Versions handler")
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	staticNetworkConfig := staticnetworkconfig.New(log.WithField("pkg", "static_network_config"), Options.StaticNetworkConfig)
	ignitionBuilder, err := ignition.NewBuilder(log.WithField("pkg", "ignition"), staticNetworkConfig, mirrorRegistriesBuilder)
	failOnError(err, "failed to create ignition builder")
	installConfigBuilder := installcfg.NewInstallConfigBuilder(log.WithField("pkg", "installcfg"), mirrorRegistriesBuilder, providerRegistry)

	var objectHandler = createStorageClient(Options.DeployTarget, Options.Storage, &Options.S3Config,
		Options.WorkDir, log, metricsManager, Options.FileSystemUsageThreshold)
	createS3Bucket(objectHandler, log)

	manifestsApi := manifests.NewManifestsAPI(db, log.WithField("pkg", "manifests"), objectHandler, usageManager)
	operatorsManager := operators.NewManager(log, manifestsApi, Options.OperatorsConfig, objectHandler, extracterHandler)
	hwValidator := hardware.NewValidator(log.WithField("pkg", "validators"), Options.HWValidatorConfig, operatorsManager)
	connectivityValidator := connectivity.NewValidator(log.WithField("pkg", "validators"))
	Options.InstructionConfig.DisabledSteps = disableFreeAddressesIfNeeded(Options.EnableKubeAPI, Options.InstructionConfig.DisabledSteps)
	instructionApi := hostcommands.NewInstructionManager(log.WithField("pkg", "instructions"), db, hwValidator,
		releaseHandler, Options.InstructionConfig, connectivityValidator, eventsHandler, versionHandler)

	images := []string{
		Options.ReleaseImageMirror,
		Options.BMConfig.AgentDockerImg,
		Options.InstructionConfig.InstallerImage,
		Options.InstructionConfig.ControllerImage,
		Options.InstructionConfig.AgentImage,
	}

	for _, releaseImage := range releaseImagesArray {
		images = append(images, *releaseImage.URL)
	}

	pullSecretValidator, err := validations.NewPullSecretValidator(Options.ValidationsConfig, authHandler, images...)
	failOnError(err, "failed to create pull secret validator")

	log.Println("DeployTarget: " + Options.DeployTarget)

	var ocpClient k8sclient.K8SClient = nil
	switch Options.DeployTarget {
	case deployment_type_k8s:

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

		if Options.DeployTarget == deployment_type_ocp {
			ocpClient, err = k8sclient.NewK8SClient("", log)
			failOnError(err, "Failed to create client for OCP")
		}

	default:
		log.Fatalf("not supported deploy target %s", Options.DeployTarget)
	}

	failOnError(autoMigrationWithLeader(autoMigrationLeader, db, log), "Failed auto migration process")

	hostApi := host.NewManager(log.WithField("pkg", "host-state"), db, eventsHandler, hwValidator,
		instructionApi, &Options.HWValidatorConfig, metricsManager, &Options.HostConfig, lead, operatorsManager, providerRegistry)
	dnsApi := dns.NewDNSHandler(Options.BMConfig.BaseDNSDomains, log)
	manifestsGenerator := network.NewManifestsGenerator(manifestsApi, Options.ManifestsGeneratorConfig)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager, manifestsGenerator, lead, operatorsManager, ocmClient, objectHandler, dnsApi, authHandler)
	infraEnvApi := infraenv.NewManager(log.WithField("pkg", "host-state"), db, objectHandler)

	clusterStateMonitor := thread.New(
		log.WithField("pkg", "cluster-monitor"), "Cluster State Monitor", Options.ClusterStateMonitorInterval, clusterApi.ClusterMonitoring)
	clusterStateMonitor.Start()
	defer clusterStateMonitor.Stop()

	hostStateMonitor := thread.New(
		log.WithField("pkg", "host-monitor"), "Host State Monitor", Options.HostStateMonitorInterval, hostApi.HostMonitoring)
	hostStateMonitor.Start()
	defer hostStateMonitor.Stop()

	newUrl, err := s3wrapper.FixEndpointURL(Options.BMConfig.S3EndpointURL)
	failOnError(err, "failed to create valid bm config S3 endpoint URL from %s", Options.BMConfig.S3EndpointURL)
	Options.BMConfig.S3EndpointURL = newUrl

	generator := generator.New(log, objectHandler, Options.GeneratorConfig, Options.WorkDir, operatorsManager, providerRegistry, Options.ClusterTLSCertOverrideDir)
	var crdUtils bminventory.CRDUtils
	if ctrlMgr != nil {
		crdUtils = controllers.NewCRDUtils(ctrlMgr.GetClient(), hostApi)
	} else {
		crdUtils = controllers.NewDummyCRDUtils()
	}

	if Options.EnableDeregisterInactiveGC || Options.EnableDeletedUnregisteredGC {
		gc := garbagecollector.NewGarbageCollectors(Options.GCConfig, db, log.WithField("pkg", "garbage_collector"),
			hostApi, clusterApi, infraEnvApi, objectHandler, lead)

		// In operator-deployment, ClusterDeployment is responsible for managing the lifetime of the cluster resource.
		if !Options.EnableKubeAPI && Options.EnableDeregisterInactiveGC {
			deregisterWorker := thread.New(
				log.WithField("garbagecollector", "Deregister Worker"),
				"Deregister Worker",
				Options.DeregisterWorkerInterval,
				gc.DeregisterInactiveClusters)

			deregisterWorker.Start()
			defer deregisterWorker.Stop()
		}

		if Options.EnableDeletedUnregisteredGC {
			deletionWorker := thread.New(
				log.WithField("garbagecollector", "Deletion Worker"),
				"Deletion Worker",
				Options.DeletionWorkerInterval,
				gc.PermanentlyDeleteUnregisteredClustersAndHosts)

			deletionWorker.Start()
			defer deletionWorker.Stop()
		}

		//In operator-deployment, InfraEnv CR is responsible for managing the lifetime of the InfraEnv resource.
		if !Options.EnableKubeAPI {
			deletionInfraEnvWorker := thread.New(
				log.WithField("garbagecollector", "InfraEnv Deletion Worker"),
				"InfraEnv Deletion Worker",
				Options.InfraEnvDeletionWorkerInterval,
				gc.DeleteOrphanInfraEnvs)

			deletionInfraEnvWorker.Start()
			defer deletionInfraEnvWorker.Stop()
		}
	}

	// Determine if IPXE artifact URLs need to be http
	serverInfo := servers.New(Options.HTTPListenPort, swag.StringValue(port), Options.HTTPSKeyFile, Options.HTTPSCertFile)
	generateInsecureIPXEURLs := serverInfo.HTTP != nil

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, infraEnvApi, Options.BMConfig,
		generator, eventsHandler, objectHandler, metricsManager, usageManager, operatorsManager, authHandler, authzHandler, ocpClient, ocmClient,
		lead, pullSecretValidator, versionHandler, crdUtils, ignitionBuilder, hwValidator, dnsApi, installConfigBuilder, staticNetworkConfig,
		Options.GCConfig, providerRegistry, generateInsecureIPXEURLs)

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))

	//Set inner handler chain. Inner handlers requires access to the Route
	innerHandler := func() func(http.Handler) http.Handler {
		return func(h http.Handler) http.Handler {
			wrapped := metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry)(h)

			if Options.EnableElasticAPM {
				// For APM metrics, we only want to trace openapi (internal) requests.
				// We are generating our own transaction name since we are wrapping the lower
				// http handler. This will allow us to generate a transaction name that allows
				// us to group similar requests (using URL patterns) rather than individual ones.
				apmOptions := apmhttp.WithServerRequestName(generateAPMTransactionName)
				wrapped = apmhttp.Wrap(wrapped, apmOptions)
			}

			wrapped = paramctx.ContextHandler()(wrapped)
			return wrapped
		}
	}

	operatorsHandler := handler.NewHandler(operatorsManager, log.WithField("pkg", "operators"), db, eventsHandler, clusterApi)
	h, err := restapi.Handler(restapi.Config{
		AuthAgentAuth:       authHandler.AuthAgentAuth,
		AuthUserAuth:        authHandler.AuthUserAuth,
		AuthURLAuth:         authHandler.AuthURLAuth,
		AuthImageAuth:       authHandler.AuthImageAuth,
		AuthImageURLAuth:    authHandler.AuthImageAuth,
		APIKeyAuthenticator: authHandler.CreateAuthenticator(),
		Authorizer:          authzHandler.CreateAuthorizer(),
		InstallerAPI:        bm,
		EventsAPI:           events,
		Logger:              log.Printf,
		VersionsAPI:         versionHandler,
		ManagedDomainsAPI:   domainHandler,
		InnerMiddleware:     innerHandler(),
		ManifestsAPI:        manifestsApi,
		OperatorsAPI:        operatorsHandler,
	})
	failOnError(err, "Failed to init rest handler")

	if Options.Auth.AllowedDomains != "" {
		allowedDomains := strings.Split(strings.ReplaceAll(Options.Auth.AllowedDomains, " ", ""), ",")
		log.Infof("AllowedDomains were provided, enabling CORS with %s as domain list", allowedDomains)
		// enabling CORS with given domain list
		h = app.SetupCORSMiddleware(h, allowedDomains)
	}

	h = app.WithMetricsResponderMiddleware(h)
	h = app.WithHealthMiddleware(h, []*thread.Thread{hostStateMonitor, clusterStateMonitor},
		log.WithField("pkg", "healthcheck"), Options.LivenessValidationTimeout)
	h = requestid.Middleware(h)
	h = spec.WithSpecMiddleware(h)

	go func() {
		if log.Level == logrus.DebugLevel {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}
	}()

	go func() {
		if Options.EnableKubeAPI {
			bmoUtils := controllers.NewBMOUtils(ctrlMgr.GetAPIReader(),
				log.WithField("pkg", "baremetal_operator_utils"),
				Options.EnableKubeAPI)
			useConvergedFlow := false
			if Options.AllowConvergedFlow {
				useConvergedFlow = bmoUtils.ConvergedFlowAvailable()
			}

			c := ctrlMgr.GetClient()
			r := ctrlMgr.GetAPIReader()
			failOnError((&controllers.InfraEnvReconciler{
				Client:              c,
				APIReader:           r,
				Config:              Options.InfraEnvConfig,
				Log:                 log,
				Installer:           bm,
				CRDEventsHandler:    crdEventsHandler,
				ServiceBaseURL:      Options.BMConfig.ServiceBaseURL,
				ImageServiceBaseURL: Options.BMConfig.ImageServiceBaseURL,
				AuthType:            Options.Auth.AuthType,
				VersionsHandler:     versionHandler,
				PullSecretHandler:   controllers.NewPullSecretHandler(c, r, bm),
				InsecureIPXEURLs:    generateInsecureIPXEURLs,
			}).SetupWithManager(ctrlMgr), "unable to create controller InfraEnv")

			cluster_client := ctrlMgr.GetClient()
			cluster_reader := ctrlMgr.GetAPIReader()
			failOnError((&controllers.ClusterDeploymentsReconciler{
				Client:            cluster_client,
				APIReader:         cluster_reader,
				Log:               log,
				Scheme:            ctrlMgr.GetScheme(),
				Installer:         bm,
				ClusterApi:        clusterApi,
				HostApi:           hostApi,
				CRDEventsHandler:  crdEventsHandler,
				Manifests:         manifestsApi,
				ServiceBaseURL:    Options.BMConfig.ServiceBaseURL,
				PullSecretHandler: controllers.NewPullSecretHandler(cluster_client, cluster_reader, bm),
				AuthType:          Options.Auth.AuthType,
			}).SetupWithManager(ctrlMgr), "unable to create controller ClusterDeployment")

			failOnError((&controllers.AgentReconciler{
				Client:                     ctrlMgr.GetClient(),
				APIReader:                  ctrlMgr.GetAPIReader(),
				Log:                        log,
				Scheme:                     ctrlMgr.GetScheme(),
				Installer:                  bm,
				CRDEventsHandler:           crdEventsHandler,
				ServiceBaseURL:             Options.BMConfig.ServiceBaseURL,
				AuthType:                   Options.Auth.AuthType,
				SpokeK8sClientFactory:      controllers.NewSpokeK8sClientFactory(log),
				ApproveCsrsRequeueDuration: Options.ApproveCsrsRequeueDuration,
			}).SetupWithManager(ctrlMgr), "unable to create controller Agent")

			failOnError((&controllers.BMACReconciler{
				Client:                ctrlMgr.GetClient(),
				APIReader:             ctrlMgr.GetAPIReader(),
				Log:                   log,
				Scheme:                ctrlMgr.GetScheme(),
				SpokeK8sClientFactory: controllers.NewSpokeK8sClientFactory(log),
				ConvergedFlowEnabled:  useConvergedFlow,
			}).SetupWithManager(ctrlMgr), "unable to create controller BMH")

			failOnError((&controllers.AgentClusterInstallReconciler{
				Client:           ctrlMgr.GetClient(),
				Log:              log,
				CRDEventsHandler: crdEventsHandler,
			}).SetupWithManager(ctrlMgr), "unable to create controller AgentClusterInstall")

			failOnError((&controllers.AgentClassificationReconciler{
				Client: ctrlMgr.GetClient(),
				Log:    log,
			}).SetupWithManager(ctrlMgr), "unable to create controller AgentClassification")

			failOnError((&controllers.AgentLabelReconciler{
				Client: ctrlMgr.GetClient(),
				Log:    log,
			}).SetupWithManager(ctrlMgr), "unable to create controller AgentLabel")

			if useConvergedFlow {
				ironicBaseURL, err := bmoUtils.GetIronicServiceURL()
				if err != nil {
					log.WithError(err).Fatal("failed to get IronicServiceURL")
				}
				failOnError((&controllers.PreprovisioningImageReconciler{
					Client:                 ctrlMgr.GetClient(),
					Log:                    log,
					Installer:              bm,
					CRDEventsHandler:       crdEventsHandler,
					VersionsHandler:        versionHandler,
					OcRelease:              releaseHandler,
					IronicIgniotionBuilder: ignition.NewIronicIgniotionBuilder(Options.IronicIgnitionBuilderConfig),
					IronicServiceURL:       ironicBaseURL,
				}).SetupWithManager(ctrlMgr), "failed to create PreprovisioningImage ceontroller")
			}
			log.Infof("Starting controllers")
			failOnError(ctrlMgr.Start(ctrl.SetupSignalHandler()), "failed to run manager")
		}
	}()

	// Interrupt servers on SIGINT/SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Run listen on http and https ports if iPXE artifacts need to be exposed via HTTP
	if serverInfo.HasBothHandlers {
		h = app.WithIPXEScriptMiddleware(h)
	}
	if serverInfo.HTTP != nil {
		serverInfo.HTTP.Handler = h
	}
	if serverInfo.HTTPS != nil {
		serverInfo.HTTPS.Handler = h
	}
	serverInfo.ListenAndServe()
	<-stop
	serverInfo.Shutdown()
}

func generateAPMTransactionName(request *http.Request) string {
	route := middleware.MatchedRouteFrom(request)

	if route == nil {
		// Use the actual URL path if no route
		// matched this request. This will make
		// sure we can, granuarly, introspect
		// non-grouped requests.
		return request.URL.Path
	}

	// This matches the `operationId` in the swagger file
	return route.Operation.ID
}

func setupDB(log logrus.FieldLogger) *gorm.DB {
	dbConnectionStr := fmt.Sprintf("host=%s port=%s user=%s database=%s password=%s sslmode=disable",
		Options.DBConfig.Host, Options.DBConfig.Port, Options.DBConfig.User, Options.DBConfig.Name, Options.DBConfig.Pass)
	var db *gorm.DB
	var err error
	// Tries to open a db connection every 2 seconds
	retryInterval := 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.Info("Connecting to DB")
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		db, err = gorm.Open(postgres.Open(dbConnectionStr), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			log.WithError(err).Info("Failed to connect to DB, retrying")
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			log.WithError(err).Info("Failed to get sqlDB, retrying")
			common.CloseDB(db)
			return
		}
		sqlDB.SetMaxIdleConns(Options.MaxIdleConns)
		sqlDB.SetMaxOpenConns(Options.MaxOpenConns)
		sqlDB.SetConnMaxLifetime(Options.ConnMaxLifetime)
		cancel()
	}, retryInterval)
	if ctx.Err().Error() == context.DeadlineExceeded.Error() {
		log.WithError(ctx.Err()).Fatal("Timed out connecting to DB")
	}
	log.Info("Connected to DB")
	return db
}

func getOCMClient(log logrus.FieldLogger) *ocm.Client {
	var ocmClient *ocm.Client
	var err error
	if Options.Auth.AuthType == auth.TypeRHSSO {
		ocmClient, err = ocm.NewClient(Options.OCMConfig, log.WithField("pkg", "ocm"))
		if err != nil {
			log.WithError(err).Fatal("Failed to Create OCM Client")
		}
	}
	return ocmClient
}

func createS3Bucket(objectHandler s3wrapper.API, log logrus.FieldLogger) {
	if Options.CreateS3Bucket {
		if err := objectHandler.CreateBucket(); err != nil {
			log.Fatal(err)
		}
	}
}

func createStorageClient(deployTarget string, storage string, s3cfg *s3wrapper.Config, fsWorkDir string,
	log logrus.FieldLogger, metricsAPI metrics.API, fsThreshold int) s3wrapper.API {
	var storageClient s3wrapper.API
	if storage != "" {
		switch storage {
		case storage_s3:
			storageClient = s3wrapper.NewS3Client(s3cfg, log)
			if storageClient == nil {
				log.Fatal("failed to create S3 client")
			}
		case storage_filesystem:
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, metricsAPI, fsThreshold)
			if storageClient == nil {
				log.Fatal("failed to create filesystem client")
			}
		default:
			log.Fatalf("unsupported storage client: %s", storage)
		}
	} else {
		// Retain original logic for backwards capability
		switch deployTarget {
		case deployment_type_k8s:
			storageClient = s3wrapper.NewS3Client(s3cfg, log)
			if storageClient == nil {
				log.Fatal("failed to create S3 client")
			}
		case deployment_type_onprem, deployment_type_ocp:
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, metricsAPI, fsThreshold)
			if storageClient == nil {
				log.Fatal("failed to create S3 filesystem client")
			}
		default:
			log.Fatalf("unsupported deploy target %s", deployTarget)
		}
	}
	return storageClient
}

func autoMigrationWithLeader(migrationLeader leader.ElectorInterface, db *gorm.DB, log logrus.FieldLogger) error {
	return migrationLeader.RunWithLeader(context.Background(), func() error {
		log.Infof("Starting manual pre migrations")
		err := migrations.MigratePre(db)
		if err != nil {
			log.WithError(err).Fatal("Manual pre migration process failed")
			return err
		}
		log.Info("Finished manual pre migrations")

		log.Infof("Start automigration")
		err = common.AutoMigrate(db)
		if err != nil {
			log.WithError(err).Fatal("Failed auto migration process")
			return err
		}
		log.Info("Finished automigration")

		log.Infof("Starting manual post migrations")
		err = migrations.MigratePost(db)
		if err != nil {
			log.WithError(err).Fatal("Manual post migration process failed")
			return err
		}
		log.Info("Finished manual post migrations")

		return nil
	})
}

func createEventsHandler(crdEventsHandler controllers.CRDEventsHandler, db *gorm.DB, authzHandler auth.Authorizer, log logrus.FieldLogger) eventsapi.Handler {
	eventsHandler := events.New(db, authzHandler, log.WithField("pkg", "events"))

	if crdEventsHandler != nil {
		return controllers.NewControllerEventsWrapper(crdEventsHandler, eventsHandler, db, log)
	}
	return eventsHandler
}

func createCRDEventsHandler() controllers.CRDEventsHandler {
	if Options.EnableKubeAPI {
		return controllers.NewCRDEventsHandler()
	}
	return nil
}

func createControllerManager() (manager.Manager, error) {
	if Options.EnableKubeAPI {
		schemes := controllers.GetKubeClientSchemes()
		infraenvLabel, err := labels.NewRequirement(controllers.InfraEnvLabel, selection.Exists, nil)
		if err != nil {
			return nil, err

		}
		return ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			Scheme:           schemes,
			Port:             9443,
			LeaderElection:   true,
			LeaderElectionID: "77190dcb.agent-install.openshift.io",
			NewCache: cache.BuilderWithOptions(cache.Options{
				SelectorsByObject: map[client.Object]cache.ObjectSelector{
					&corev1.Secret{}: {
						Label: labels.SelectorFromSet(
							labels.Set{
								controllers.WatchResourceLabel: controllers.WatchResourceValue,
							},
						),
					},
					&metal3_v1alpha1.PreprovisioningImage{}: {
						Label: labels.NewSelector().Add(*infraenvLabel),
					},
				},
			}),
		})
	}
	return nil, nil
}

func disableFreeAddressesIfNeeded(enableKubeAPI bool, disabledSteps []models.StepType) []models.StepType {
	if enableKubeAPI {
		// If this step was already disabled via environment, it wont matter once parsed.
		return append(disabledSteps, models.StepTypeFreeNetworkAddresses)
	}
	return disabledSteps
}
