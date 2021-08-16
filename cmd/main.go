package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/assistedserviceiso"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/domains"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/migrations"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/handler"
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
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.elastic.co/apm/module/apmhttp"
	"go.elastic.co/apm/module/apmlogrus"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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
	Auth                        auth.Config
	BMConfig                    bminventory.Config
	DBConfig                    dbPkg.Config
	HWValidatorConfig           hardware.ValidatorCfg
	GeneratorConfig             generator.Config
	InstructionConfig           hostcommands.InstructionConfig
	OperatorsConfig             operators.Options
	GCConfig                    garbagecollector.Config
	ClusterStateMonitorInterval time.Duration `envconfig:"CLUSTER_MONITOR_INTERVAL" default:"10s"`
	S3Config                    s3wrapper.Config
	HostStateMonitorInterval    time.Duration `envconfig:"HOST_MONITOR_INTERVAL" default:"8s"`
	Versions                    versions.Versions
	OpenshiftVersions           string        `envconfig:"OPENSHIFT_VERSIONS"`
	MustGatherImages            string        `envconfig:"MUST_GATHER_IMAGES" default:""`
	ReleaseImageMirror          string        `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	CreateS3Bucket              bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval     time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ClusterConfig               cluster.Config
	DeployTarget                string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	Storage                     string `envconfig:"STORAGE" default:"s3"`
	OCMConfig                   ocm.Config
	HostConfig                  host.Config
	LogConfig                   logconfig.Config
	LeaderConfig                leader.Config
	ValidationsConfig           validations.Config
	AssistedServiceISOConfig    assistedserviceiso.Config
	ManifestsGeneratorConfig    network.Config
	EnableKubeAPI               bool `envconfig:"ENABLE_KUBE_API" default:"false"`
	EnableKubeAPIDay2Cluster    bool `envconfig:"ENABLE_KUBE_API_DAY2" default:"false"`
	InfraEnvConfig              controllers.InfraEnvConfig
	ISOEditorConfig             isoeditor.Config
	CheckClusterVersion         bool          `envconfig:"CHECK_CLUSTER_VERSION" default:"false"`
	DeletionWorkerInterval      time.Duration `envconfig:"DELETION_WORKER_INTERVAL" default:"1h"`
	DeregisterWorkerInterval    time.Duration `envconfig:"DEREGISTER_WORKER_INTERVAL" default:"1h"`
	EnableDeletedUnregisteredGC bool          `envconfig:"ENABLE_DELETE_UNREGISTER_GC" default:"true"`
	EnableDeregisterInactiveGC  bool          `envconfig:"ENABLE_DEREGISTER_INACTIVE_GC" default:"true"`
	ServeHTTPS                  bool          `envconfig:"SERVE_HTTPS" default:"false"`
	HTTPSKeyFile                string        `envconfig:"HTTPS_KEY_FILE" default:""`
	HTTPSCertFile               string        `envconfig:"HTTPS_CERT_FILE" default:""`
	MaxIdleConns                int           `envconfig:"DB_MAX_IDLE_CONNECTIONS" default:"50"`
	MaxOpenConns                int           `envconfig:"DB_MAX_OPEN_CONNECTIONS" default:"100"`
	ConnMaxLifetime             time.Duration `envconfig:"DB_CONNECTIONS_MAX_LIFETIME" default:"30m"`
	FileSystemUsageThreshold    int           `envconfig:"FILESYSTEM_USAGE_THRESHOLD" default:"80"`
	EnableElasticAPM            bool          `envconfig:"ENABLE_ELASTIC_APM" default:"false"`
	WorkDir                     string        `envconfig:"WORK_DIR" default:"/data/"`
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

	var openshiftVersionsMap models.OpenshiftVersions
	if Options.OpenshiftVersions == "" {
		log.Fatal("OpenShift versions is empty")
	}
	failOnError(json.Unmarshal([]byte(Options.OpenshiftVersions), &openshiftVersionsMap),
		"Failed to parse supported openshift versions JSON %s", Options.OpenshiftVersions)

	log.Println(fmt.Sprintf("Started service with OCP versions %v", Options.OpenshiftVersions))

	var mustGatherVersionsMap = make(versions.MustGatherVersions)
	if Options.MustGatherImages != "" {
		failOnError(json.Unmarshal([]byte(Options.MustGatherImages), &mustGatherVersionsMap),
			"Failed to parse feature must-gather images JSON %s", Options.MustGatherImages)
	}

	failOnError(os.MkdirAll(Options.BMConfig.ISOCacheDir, 0700), "Failed to create ISO cache directory %s", Options.BMConfig.ISOCacheDir)

	// Connect to db
	db := setupDB(log)
	defer db.Close()

	ctrlMgr, err := createControllerManager()
	failOnError(err, "failed to create controller manager")

	usageManager := usage.NewManager(log)

	crdEventsHandler := createCRDEventsHandler()
	eventsHandler := createEventsHandler(crdEventsHandler, db, log)

	prometheusRegistry := prometheus.DefaultRegisterer
	metricsManager := metrics.NewMetricsManager(prometheusRegistry, eventsHandler)

	ocmClient := getOCMClient(log, metricsManager)

	Options.InstructionConfig.ReleaseImageMirror = Options.ReleaseImageMirror
	Options.InstructionConfig.CheckClusterVersion = Options.CheckClusterVersion
	Options.OperatorsConfig.CheckClusterVersion = Options.CheckClusterVersion
	Options.GeneratorConfig.ReleaseImageMirror = Options.ReleaseImageMirror

	// Make sure that prepare for installation timeout is more than the timeouts of all underlying tools + 2m extra
	Options.ClusterConfig.PrepareConfig.PrepareForInstallationTimeout = maxDuration(Options.ClusterConfig.PrepareConfig.PrepareForInstallationTimeout,
		maxDuration(Options.InstructionConfig.DiskCheckTimeout, Options.InstructionConfig.ImageAvailabilityTimeout)+2*time.Minute)
	var lead leader.ElectorInterface
	var k8sClient *kubernetes.Clientset
	var autoMigrationLeader leader.ElectorInterface
	authHandler, err := auth.NewAuthenticator(&Options.Auth, ocmClient, log.WithField("pkg", "auth"), db)
	failOnError(err, "failed to create authenticator")
	authzHandler := auth.NewAuthzHandler(&Options.Auth, ocmClient, log.WithField("pkg", "authz"))
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay})
	extracterHandler := oc.NewExtracter(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay})
	versionHandler := versions.NewHandler(log.WithField("pkg", "versions"), releaseHandler,
		Options.Versions, openshiftVersionsMap, mustGatherVersionsMap, Options.ReleaseImageMirror)
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	staticNetworkConfig := staticnetworkconfig.New(log.WithField("pkg", "static_network_config"))
	mirrorRegistriesBuilder := mirrorregistries.New()
	ignitionBuilder := ignition.NewBuilder(log.WithField("pkg", "ignition"), staticNetworkConfig, mirrorRegistriesBuilder)
	installConfigBuilder := installcfg.NewInstallConfigBuilder(log.WithField("pkg", "installcfg"), mirrorRegistriesBuilder)
	isoEditorFactory := isoeditor.NewFactory(Options.ISOEditorConfig, staticNetworkConfig)

	var objectHandler = createStorageClient(Options.DeployTarget, Options.Storage, &Options.S3Config,
		Options.WorkDir, log, versionHandler, isoEditorFactory, metricsManager, Options.FileSystemUsageThreshold)
	createS3Bucket(objectHandler, log)

	manifestsApi := manifests.NewManifestsAPI(db, log.WithField("pkg", "manifests"), objectHandler)
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

	for _, ocpVersion := range openshiftVersionsMap {
		// ReleaseImage is not necessarily specified when using the operator
		// (fetched from ClusterImageSet instead)
		if ocpVersion.ReleaseImage != nil {
			images = append(images, *ocpVersion.ReleaseImage)
		}
	}

	pullSecretValidator, err := validations.NewPullSecretValidator(Options.ValidationsConfig, images...)
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
		instructionApi, &Options.HWValidatorConfig, metricsManager, &Options.HostConfig, lead, operatorsManager)
	dnsApi := dns.NewDNSHandler(Options.BMConfig.BaseDNSDomains, log)
	manifestsGenerator := network.NewManifestsGenerator(manifestsApi, Options.ManifestsGeneratorConfig)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager, manifestsGenerator, lead, operatorsManager, ocmClient, objectHandler, dnsApi)

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

	generator := generator.New(log, objectHandler, Options.GeneratorConfig, Options.WorkDir, operatorsManager)
	var crdUtils bminventory.CRDUtils
	if ctrlMgr != nil {
		crdUtils = controllers.NewCRDUtils(ctrlMgr.GetClient(), hostApi)
	} else {
		crdUtils = controllers.NewDummyCRDUtils()
	}

	if !Options.EnableKubeAPI && (Options.EnableDeregisterInactiveGC || Options.EnableDeletedUnregisteredGC) {
		gc := garbagecollector.NewGarbageCollectors(Options.GCConfig, db, log.WithField("pkg", "garbage_collector"), hostApi, clusterApi, objectHandler, lead)

		// In operator-deployment, ClusterDeployment is responsible for managing the lifetime of the cluster resource.
		if Options.EnableDeregisterInactiveGC {
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
	}

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig,
		generator, eventsHandler, objectHandler, metricsManager, usageManager, operatorsManager, authHandler, ocpClient, ocmClient,
		lead, pullSecretValidator, versionHandler, isoEditorFactory, crdUtils, ignitionBuilder, hwValidator, dnsApi, installConfigBuilder, staticNetworkConfig,
		Options.GCConfig)

	events := events.NewApi(eventsHandler, logrus.WithField("pkg", "eventsApi"))
	expirer := imgexpirer.NewManager(objectHandler, eventsHandler, Options.BMConfig.ImageExpirationTime, lead, Options.EnableKubeAPI)
	imageExpirationMonitor := thread.New(
		log.WithField("pkg", "image-expiration-monitor"), "Image Expiration Monitor", Options.ImageExpirationInterval, expirer.ExpirationTask)
	imageExpirationMonitor.Start()
	defer imageExpirationMonitor.Stop()
	assistedServiceISO := assistedserviceiso.NewAssistedServiceISOApi(objectHandler, authHandler, logrus.WithField("pkg", "assistedserviceiso"), pullSecretValidator, Options.AssistedServiceISOConfig)

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
		AuthAgentAuth:         authHandler.AuthAgentAuth,
		AuthUserAuth:          authHandler.AuthUserAuth,
		AuthURLAuth:           authHandler.AuthURLAuth,
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
		OperatorsAPI:          operatorsHandler,
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
		// Upload ISOs with a leader lock if we're running with multiple replicas
		if Options.DeployTarget == deployment_type_k8s {
			baseISOUploadLeader := leader.NewElector(k8sClient, leader.Config{LeaseDuration: 5 * time.Second,
				RetryInterval: 2 * time.Second, Namespace: Options.LeaderConfig.Namespace, RenewDeadline: 4 * time.Second},
				"assisted-service-baseiso-helper",
				log.WithField("pkg", "baseISOUploadLeader"))

			uploadFunc := func() error { return uploadISOs(objectHandler, openshiftVersionsMap, log) }
			failOnError(baseISOUploadLeader.RunWithLeader(context.Background(), uploadFunc), "Failed to upload boot files")
		} else {
			failOnError(uploadISOs(objectHandler, openshiftVersionsMap, log), "Failed to upload boot files")
		}

		apiEnabler.Enable()
	}()

	go func() {
		if log.Level == logrus.DebugLevel {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}
	}()

	go func() {
		if Options.EnableKubeAPI {
			failOnError((&controllers.InfraEnvReconciler{
				Client:           ctrlMgr.GetClient(),
				Config:           Options.InfraEnvConfig,
				Log:              log,
				Installer:        bm,
				CRDEventsHandler: crdEventsHandler,
			}).SetupWithManager(ctrlMgr), "unable to create controller InfraEnv")

			failOnError((&controllers.ClusterDeploymentsReconciler{
				Client:            ctrlMgr.GetClient(),
				APIReader:         ctrlMgr.GetAPIReader(),
				Log:               log,
				Scheme:            ctrlMgr.GetScheme(),
				Installer:         bm,
				ClusterApi:        clusterApi,
				HostApi:           hostApi,
				CRDEventsHandler:  crdEventsHandler,
				Manifests:         manifestsApi,
				ServiceBaseURL:    Options.BMConfig.ServiceBaseURL,
				AuthType:          Options.Auth.AuthType,
				EnableDay2Cluster: Options.EnableKubeAPIDay2Cluster,
			}).SetupWithManager(ctrlMgr), "unable to create controller ClusterDeployment")

			failOnError((&controllers.AgentReconciler{
				Client:           ctrlMgr.GetClient(),
				Log:              log,
				Scheme:           ctrlMgr.GetScheme(),
				Installer:        bm,
				CRDEventsHandler: crdEventsHandler,
				ServiceBaseURL:   Options.BMConfig.ServiceBaseURL,
				AuthType:         Options.Auth.AuthType,
			}).SetupWithManager(ctrlMgr), "unable to create controller Agent")

			failOnError((&controllers.BMACReconciler{
				Client:    ctrlMgr.GetClient(),
				APIReader: ctrlMgr.GetAPIReader(),
				Log:       log,
				Scheme:    ctrlMgr.GetScheme(),
			}).SetupWithManager(ctrlMgr), "unable to create controller BMH")

			failOnError((&controllers.AgentClusterInstallReconciler{
				Client:           ctrlMgr.GetClient(),
				Log:              log,
				CRDEventsHandler: crdEventsHandler,
			}).SetupWithManager(ctrlMgr), "unable to create controller AgentClusterInstall")

			log.Info("waiting for REST api readiness before starting controllers")
			apiEnabler.WaitForEnabled()

			log.Infof("REST api is now ready, starting controllers")
			failOnError(ctrlMgr.Start(ctrl.SetupSignalHandler()), "failed to run manager")
		}
	}()

	address := fmt.Sprintf(":%s", swag.StringValue(port))
	if Options.ServeHTTPS {
		log.Fatal(http.ListenAndServeTLS(address, Options.HTTPSCertFile, Options.HTTPSKeyFile, h))
	} else {
		log.Fatal(http.ListenAndServe(address, h))
	}
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

func uploadISOs(objectHandler s3wrapper.API, openshiftVersionsMap models.OpenshiftVersions, log logrus.FieldLogger) error {
	ctx, cancel := context.WithCancel(context.Background())
	errs, _ := errgroup.WithContext(ctx)
	//cancel the context in case this method ends
	defer cancel()

	//starts a functional context to pass to loggers and derived flows
	uploadctx := requestid.ToContext(context.Background(), "main-uploadISOs")

	// Checks whether latest version of minimal ISO templates already exists
	// Must be done while holding the leader lock but outside of the version loop
	haveLatestMinimalTemplate := s3wrapper.HaveLatestMinimalTemplate(uploadctx, log, objectHandler)
	for version := range openshiftVersionsMap {
		currVersion := version
		errs.Go(func() error {
			err := objectHandler.UploadISOs(uploadctx, currVersion, haveLatestMinimalTemplate)
			return errors.Wrapf(err, "Failed uploading boot files for OCP version %s", currVersion)
		})
	}

	return errs.Wait()
}

func setupDB(log logrus.FieldLogger) *gorm.DB {
	dbConnectionStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=disable",
		Options.DBConfig.Host, Options.DBConfig.Port, Options.DBConfig.User, Options.DBConfig.Name, Options.DBConfig.Pass)
	var db *gorm.DB
	var err error
	// Tries to open a db connection every 2 seconds for up to 10 seconds.
	timeout := 10 * time.Second
	retryInterval := 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Info("Connecting to DB")
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		db, err = gorm.Open("postgres", dbConnectionStr)
		if err != nil {
			log.WithError(err).Info("Failed to connect to DB, retrying")
			return
		}
		db.DB().SetMaxIdleConns(Options.MaxIdleConns)
		db.DB().SetMaxOpenConns(Options.MaxOpenConns)
		db.DB().SetConnMaxLifetime(Options.ConnMaxLifetime)
		cancel()
	}, retryInterval)
	if ctx.Err().Error() == context.DeadlineExceeded.Error() {
		log.WithError(ctx.Err()).Fatal("Timed out connecting to DB")
	}
	log.Info("Connected to DB")
	return db
}

func getOCMClient(log logrus.FieldLogger, metrics metrics.API) *ocm.Client {
	var ocmClient *ocm.Client
	var err error
	if Options.Auth.AuthType == auth.TypeRHSSO {
		ocmClient, err = ocm.NewClient(Options.OCMConfig, log.WithField("pkg", "ocm"), metrics)
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
		if err := objectHandler.CreatePublicBucket(); err != nil {
			log.Fatal(err)
		}
	}
}

func createStorageClient(deployTarget string, storage string, s3cfg *s3wrapper.Config, fsWorkDir string,
	log logrus.FieldLogger, versionsHandler versions.Handler, isoEditorFactory isoeditor.Factory, metricsAPI metrics.API, fsThreshold int) s3wrapper.API {
	var storageClient s3wrapper.API
	if storage != "" {
		switch storage {
		case storage_s3:
			storageClient = s3wrapper.NewS3Client(s3cfg, log, versionsHandler, isoEditorFactory)
			if storageClient == nil {
				log.Fatal("failed to create S3 client")
			}
		case storage_filesystem:
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, versionsHandler, isoEditorFactory, metricsAPI, fsThreshold)
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
			storageClient = s3wrapper.NewS3Client(s3cfg, log, versionsHandler, isoEditorFactory)
			if storageClient == nil {
				log.Fatal("failed to create S3 client")
			}
		case deployment_type_onprem, deployment_type_ocp:
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, versionsHandler, isoEditorFactory, metricsAPI, fsThreshold)
			if storageClient == nil {
				log.Fatal("failed to create S3 filesystem client")
			}
		default:
			log.Fatalf("unsupported deploy target %s", deployTarget)
		}
	}
	return storageClient
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

func (a *ApiEnabler) WaitForEnabled() {
	for !a.isEnabled {
		time.Sleep(time.Second)
	}
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

func createEventsHandler(crdEventsHandler controllers.CRDEventsHandler, db *gorm.DB, log logrus.FieldLogger) events.Handler {
	eventsHandler := events.New(db, log.WithField("pkg", "events"))

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

		return ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			Scheme:           schemes,
			Port:             9443,
			LeaderElection:   true,
			LeaderElectionID: "77190dcb.agent-install.openshift.io",
			NewCache: cache.BuilderWithOptions(cache.Options{
				SelectorsByObject: cache.SelectorsByObject{
					&corev1.Secret{}: {
						Label: labels.SelectorFromSet(
							labels.Set{
								controllers.WatchResourceLabel: controllers.WatchResourceValue,
							},
						),
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
