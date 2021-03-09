package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
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
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/migrations"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/handler"
	"github.com/openshift/assisted-service/internal/spec"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/app"
	"github.com/openshift/assisted-service/pkg/auth"
	paramctx "github.com/openshift/assisted-service/pkg/context"
	dbPkg "github.com/openshift/assisted-service/pkg/db"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logconfig "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/openshift/assisted-service/restapi"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
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
	DBConfig                    dbPkg.Config
	HWValidatorConfig           hardware.ValidatorCfg
	JobConfig                   job.Config
	InstructionConfig           hostcommands.InstructionConfig
	ClusterStateMonitorInterval time.Duration `envconfig:"CLUSTER_MONITOR_INTERVAL" default:"10s"`
	S3Config                    s3wrapper.Config
	HostStateMonitorInterval    time.Duration `envconfig:"HOST_MONITOR_INTERVAL" default:"8s"`
	Versions                    versions.Versions
	OpenshiftVersions           string        `envconfig:"OPENSHIFT_VERSIONS"`
	ReleaseImageMirror          string        `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	CreateS3Bucket              bool          `envconfig:"CREATE_S3_BUCKET" default:"false"`
	ImageExpirationInterval     time.Duration `envconfig:"IMAGE_EXPIRATION_INTERVAL" default:"30m"`
	ClusterConfig               cluster.Config
	DeployTarget                string `envconfig:"DEPLOY_TARGET" default:"k8s"`
	Storage                     string `envconfig:"STORAGE" default:""`
	OCMConfig                   ocm.Config
	HostConfig                  host.Config
	LogConfig                   logconfig.Config
	LeaderConfig                leader.Config
	DeletionWorkerInterval      time.Duration `envconfig:"DELETION_WORKER_INTERVAL" default:"1h"`
	ValidationsConfig           validations.Config
	AssistedServiceISOConfig    assistedserviceiso.Config
	EnableKubeAPI               bool `envconfig:"ENABLE_KUBE_API" default:"false"`
	ISOEditorConfig             isoeditor.Config
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

	if Options.OpenshiftVersions == "" {
		log.Fatal("OpenShift versions is empty")
	}

	failOnError(json.Unmarshal([]byte(Options.OpenshiftVersions), &openshiftVersionsMap),
		"Failed to parse supported openshift versions JSON %s", Options.OpenshiftVersions)

	log.Println(fmt.Sprintf("Started service with OCP versions %v", openshiftVersionsMap))

	failOnError(os.MkdirAll(Options.BMConfig.ISOCacheDir, 0700), "Failed to create ISO cache directory %s", Options.BMConfig.ISOCacheDir)

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
	authHandler, err := auth.NewAuthenticator(&Options.Auth, ocmClient, log.WithField("pkg", "auth"), db)
	failOnError(err, "failed to create authenticator")
	authzHandler := auth.NewAuthzHandler(&Options.Auth, ocmClient, log.WithField("pkg", "authz"))
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{})
	versionHandler := versions.NewHandler(log.WithField("pkg", "versions"), releaseHandler,
		Options.Versions, openshiftVersionsMap, Options.ReleaseImageMirror)
	domainHandler := domains.NewHandler(Options.BMConfig.BaseDNSDomains)
	eventsHandler := events.New(db, log.WithField("pkg", "events"))
	hwValidator := hardware.NewValidator(log.WithField("pkg", "validators"), Options.HWValidatorConfig)
	connectivityValidator := connectivity.NewValidator(log.WithField("pkg", "validators"))
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
		images = append(images, *ocpVersion.ReleaseImage)
	}

	pullSecretValidator, err := validations.NewPullSecretValidator(Options.ValidationsConfig, images...)
	failOnError(err, "failed to create pull secret validator")

	log.Println("DeployTarget: " + Options.DeployTarget)

	var newUrl string
	newUrl, err = s3wrapper.FixEndpointURL(Options.JobConfig.S3EndpointURL)
	failOnError(err, "failed to create valid job config S3 endpoint URL from %s", Options.JobConfig.S3EndpointURL)
	Options.JobConfig.S3EndpointURL = newUrl

	staticNetworkConfig := staticnetworkconfig.New(log.WithField("pkg", "static_network_config"))

	isoEditorFactory := isoeditor.NewFactory(Options.ISOEditorConfig, staticNetworkConfig)

	var objectHandler s3wrapper.API = createStorageClient(Options.DeployTarget, Options.Storage, &Options.S3Config,
		Options.JobConfig.WorkDir, log, versionHandler, isoEditorFactory)
	createS3Bucket(objectHandler, log)

	failOnError(bmh_v1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme),
		"Failed to add BareMetalHost to scheme")

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

	operatorsManager := operators.NewManager(log)
	hostApi := host.NewManager(log.WithField("pkg", "host-state"), db, eventsHandler, hwValidator,
		instructionApi, &Options.HWValidatorConfig, metricsManager, &Options.HostConfig, lead, operatorsManager)
	manifestsApi := manifests.NewManifestsAPI(db, log.WithField("pkg", "manifests"), objectHandler)
	manifestsGenerator := network.NewManifestsGenerator(manifestsApi)
	clusterApi := cluster.NewManager(Options.ClusterConfig, log.WithField("pkg", "cluster-state"), db,
		eventsHandler, hostApi, metricsManager, manifestsGenerator, lead, operatorsManager)
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

	generator := newISOInstallConfigGenerator(log, objectHandler, operatorsManager)
	var crdUtils bminventory.CRDUtils
	if ctrlMgr != nil {
		crdUtils = controllers.NewCRDUtils(ctrlMgr.GetClient())
	} else {
		crdUtils = controllers.NewDummyCRDUtils()
	}

	bm := bminventory.NewBareMetalInventory(db, log.WithField("pkg", "Inventory"), hostApi, clusterApi, Options.BMConfig,
		generator, eventsHandler, objectHandler, metricsManager, operatorsManager, authHandler, ocpClient, ocmClient,
		lead, pullSecretValidator, versionHandler, isoEditorFactory, crdUtils, staticNetworkConfig)

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
	assistedServiceISO := assistedserviceiso.NewAssistedServiceISOApi(objectHandler, authHandler, logrus.WithField("pkg", "assistedserviceiso"), pullSecretValidator, Options.AssistedServiceISOConfig)

	//Set inner handler chain. Inner handlers requires access to the Route
	innerHandler := func() func(http.Handler) http.Handler {
		return func(h http.Handler) http.Handler {
			wrapped := metrics.WithMatchedRoute(log.WithField("pkg", "matched-h"), prometheusRegistry)(h)
			wrapped = paramctx.ContextHandler()(wrapped)
			return wrapped
		}
	}

	operatorsHandler := handler.NewHandler(operatorsManager, log.WithField("pkg", "operators"), db)
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
		ctx, cancel := context.WithCancel(context.Background())
		errs, _ := errgroup.WithContext(ctx)
		//cancel the context in case this method ends
		defer cancel()

		// Checks whether latest version of minimal ISO templates already exists
		haveLatestMinimalTemplate := s3wrapper.HaveLatestMinimalTemplate(context.Background(), log, objectHandler)

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
						return objectHandler.UploadBootFiles(context.Background(), currVresion, Options.BMConfig.ServiceBaseURL, haveLatestMinimalTemplate)
					}), "Failed uploading boot files for OCP version %s", currVresion)
				})
			}
		case deployment_type_onprem, deployment_type_ocp:
			for version := range openshiftVersionsMap {
				currVresion := version
				errs.Go(func() error {
					return errors.Wrapf(objectHandler.UploadBootFiles(context.Background(), currVresion, Options.BMConfig.ServiceBaseURL, haveLatestMinimalTemplate),
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
			crdEventsHandler := controllers.NewCRDEventsHandler()
			failOnError((&controllers.InstallEnvReconciler{
				Client:           ctrlMgr.GetClient(),
				Log:              log,
				Installer:        bm,
				CRDEventsHandler: crdEventsHandler,
			}).SetupWithManager(ctrlMgr), "unable to create controller InstallEnv")

			failOnError((&controllers.ClusterDeploymentsReconciler{
				Client:           ctrlMgr.GetClient(),
				Log:              log,
				Scheme:           ctrlMgr.GetScheme(),
				Installer:        bm,
				ClusterApi:       clusterApi,
				HostApi:          hostApi,
				CRDEventsHandler: crdEventsHandler,
			}).SetupWithManager(ctrlMgr), "unable to create controller ClusterDeployment")

			failOnError((&controllers.AgentReconciler{
				Client:    ctrlMgr.GetClient(),
				Log:       log,
				Scheme:    ctrlMgr.GetScheme(),
				Installer: bm,
			}).SetupWithManager(ctrlMgr), "unable to create controller Agent")

			failOnError(ctrlMgr.Start(ctrl.SetupSignalHandler()), "failed to run manager")
		}
	}()

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}

func newISOInstallConfigGenerator(log *logrus.Entry, objectHandler s3wrapper.API, operatorsApi operators.API) generator.ISOInstallConfigGenerator {
	var configGenerator generator.ISOInstallConfigGenerator
	switch Options.DeployTarget {
	case deployment_type_k8s:
		kclient, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme.Scheme})
		if err != nil {
			log.WithError(err).Fatalf("failed to create controller-runtime client")
		}
		configGenerator = job.New(log.WithField("pkg", "k8s-job-wrapper"), kclient, objectHandler, Options.JobConfig, operatorsApi)
	case deployment_type_onprem, deployment_type_ocp:
		configGenerator = job.NewLocalJob(log.WithField("pkg", "local-job-wrapper"), objectHandler, Options.JobConfig, operatorsApi)
	default:
		log.Fatalf("not supported deploy target %s", Options.DeployTarget)
	}
	return configGenerator
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
	if Options.Auth.ResolvedAuthType() == auth.TypeRHSSO {
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
	log logrus.FieldLogger, versionsHandler versions.Handler, isoEditorFactory isoeditor.Factory) s3wrapper.API {
	var storageClient s3wrapper.API
	if storage != "" {
		switch storage {
		case "s3":
			storageClient = s3wrapper.NewS3Client(s3cfg, log, versionsHandler, isoEditorFactory)
			if storageClient == nil {
				log.Fatal("failed to create S3 client")
			}
		case "filesystem":
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, versionsHandler, isoEditorFactory)
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
			storageClient = s3wrapper.NewFSClient(fsWorkDir, log, versionsHandler, isoEditorFactory)
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

func autoMigrationWithLeader(migrationLeader leader.ElectorInterface, db *gorm.DB, log logrus.FieldLogger) error {
	return migrationLeader.RunWithLeader(context.Background(), func() error {
		log.Infof("Start automigration")
		err := common.AutoMigrate(db)
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
		utilruntime.Must(hivev1.AddToScheme(schemes))

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
