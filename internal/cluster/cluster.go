package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	reflect "reflect"
	"sort"
	"strings"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/jinzhu/gorm"
	"github.com/kennygrant/sanitize"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"k8s.io/apimachinery/pkg/types"
)

const DhcpLeaseTimeoutMinutes = 2

var S3FileNames = []string{
	constants.Kubeconfig,
	"bootstrap.ign",
	"master.ign",
	"worker.ign",
	"metadata.json",
	"kubeadmin-password",
	"kubeconfig-noingress",
	"install-config.yaml",
	"discovery.ign",
	"custom_manifests.yaml",
}

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster_api.go

type RegistrationAPI interface {
	// Register a new cluster
	RegisterCluster(ctx context.Context, c *common.Cluster) error
	// Register a new add-host cluster
	RegisterAddHostsCluster(ctx context.Context, c *common.Cluster) error
	// Register a new add-host-ocp cluster
	RegisterAddHostsOCPCluster(c *common.Cluster, db *gorm.DB) error
	//deregister cluster
	DeregisterCluster(ctx context.Context, c *common.Cluster) error
}

type InstallationAPI interface {
	// Install cluster
	Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	// Get the cluster master nodes ID's
	GetMasterNodesIds(ctx context.Context, c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error)
}

type API interface {
	RegistrationAPI
	InstallationAPI
	// Refresh state in case of hosts update
	RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error)
	ClusterMonitoring()
	IsOperatorAvailable(c *common.Cluster, operatorName string) bool
	UploadIngressCert(c *common.Cluster) (err error)
	VerifyClusterUpdatability(c *common.Cluster) (err error)
	AcceptRegistration(c *common.Cluster) (err error)
	CancelInstallation(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetCluster(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	PrepareForInstallation(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	HandlePreInstallError(ctx context.Context, c *common.Cluster, err error)
	HandlePreInstallSuccess(ctx context.Context, c *common.Cluster)
	SetVipsData(ctx context.Context, c *common.Cluster, apiVip, ingressVip, apiVipLease, ingressVipLease string, db *gorm.DB) error
	IsReadyForInstallation(c *common.Cluster) (bool, string)
	CreateTarredClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) (string, error)
	SetUploadControllerLogsAt(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	SetConnectivityMajorityGroupsForCluster(clusterID strfmt.UUID, db *gorm.DB) error
	DeleteClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error
	DeleteClusterFiles(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error
	UpdateLogsProgress(ctx context.Context, c *common.Cluster, progress string) error
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	UpdateAmsSubscriptionID(ctx context.Context, clusterID, amsSubscriptionID strfmt.UUID) *common.ApiErrorResponse
	GenerateAdditionalManifests(ctx context.Context, cluster *common.Cluster) error
	CompleteInstallation(ctx context.Context, db *gorm.DB, cluster *common.Cluster, successfullyFinished bool, reason string) (*common.Cluster, error)
	PermanentClustersDeletion(ctx context.Context, olderThan strfmt.DateTime, objectHandler s3wrapper.API) error
	DeregisterInactiveCluster(ctx context.Context, maxDeregisterPerInterval int, inactiveSince strfmt.DateTime) error
}

type LogTimeoutConfig struct {
	LogCollectionTimeout time.Duration `envconfig:"LOG_COLLECTION_TIMEOUT" default:"60m"`
	LogPendingTimeout    time.Duration `envconfig:"LOG_PENDING_TIMEOUT" default:"10m"`
}
type PrepareConfig struct {
	LogTimeoutConfig
	InstallationTimeout time.Duration `envconfig:"PREPARE_FOR_INSTALLATION_TIMEOUT" default:"10m"`
}

type Config struct {
	PrepareConfig           PrepareConfig
	MonitorBatchSize        int  `envconfig:"CLUSTER_MONITOR_BATCH_SIZE" default:"100"`
	EnableSingleNodeDnsmasq bool `envconfig:"ENABLE_SINGLE_NODE_DNSMASQ" default:"false"`
}

type Manager struct {
	Config
	log                   logrus.FieldLogger
	db                    *gorm.DB
	registrationAPI       RegistrationAPI
	installationAPI       InstallationAPI
	eventsHandler         events.Handler
	sm                    stateswitch.StateMachine
	metricAPI             metrics.API
	manifestsGeneratorAPI network.ManifestsGeneratorAPI
	hostAPI               host.API
	rp                    *refreshPreprocessor
	leaderElector         leader.Leader
	prevMonitorInvokedAt  time.Time
	ocmClient             *ocm.Client
	objectHandler         s3wrapper.API
	dnsApi                dns.DNSApi
	monitorQueryGenerator *common.MonitorQueryGenerator
}

func NewManager(cfg Config, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler,
	hostAPI host.API, metricApi metrics.API, manifestsGeneratorAPI network.ManifestsGeneratorAPI,
	leaderElector leader.Leader, operatorsApi operators.API, ocmClient *ocm.Client, objectHandler s3wrapper.API, dnsApi dns.DNSApi) *Manager {
	th := &transitionHandler{
		log:           log,
		db:            db,
		prepareConfig: cfg.PrepareConfig,
		eventsHandler: eventsHandler,
	}
	return &Manager{
		Config:                cfg,
		log:                   log,
		db:                    db,
		registrationAPI:       NewRegistrar(log, db),
		installationAPI:       NewInstaller(log, db, eventsHandler),
		eventsHandler:         eventsHandler,
		sm:                    NewClusterStateMachine(th),
		metricAPI:             metricApi,
		manifestsGeneratorAPI: manifestsGeneratorAPI,
		rp:                    newRefreshPreprocessor(log, hostAPI, operatorsApi),
		hostAPI:               hostAPI,
		leaderElector:         leaderElector,
		prevMonitorInvokedAt:  time.Now(),
		ocmClient:             ocmClient,
		objectHandler:         objectHandler,
		dnsApi:                dnsApi,
	}
}

func (m *Manager) RegisterCluster(ctx context.Context, c *common.Cluster) error {
	err := m.registrationAPI.RegisterCluster(ctx, c)
	if err != nil {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError,
			fmt.Sprintf("Failed to register cluster with name \"%s\". Error: %s", c.Name, err.Error()), time.Now())
	} else {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
			fmt.Sprintf("Registered cluster \"%s\"", c.Name), time.Now())
	}
	return err
}

func (m *Manager) RegisterAddHostsCluster(ctx context.Context, c *common.Cluster) error {
	err := m.registrationAPI.RegisterAddHostsCluster(ctx, c)
	if err != nil {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError,
			fmt.Sprintf("Failed to register add-hosts cluster with name \"%s\". Error: %s", c.Name, err.Error()), time.Now())
	} else {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
			fmt.Sprintf("Registered add-hosts cluster \"%s\"", c.Name), time.Now())
	}
	return err
}

func (m *Manager) RegisterAddHostsOCPCluster(c *common.Cluster, db *gorm.DB) error {
	return m.registrationAPI.RegisterAddHostsOCPCluster(c, db)
}

func (m *Manager) DeregisterCluster(ctx context.Context, c *common.Cluster) error {

	var metricsErr error
	for _, h := range c.Hosts {
		if err := m.hostAPI.ReportValidationFailedMetrics(ctx, h, c.OpenshiftVersion, c.EmailDomain); err != nil {
			m.log.WithError(err).Errorf("Failed to report metrics for failed validations on host %s in cluster %s", h.ID, c.ID)
			metricsErr = multierror.Append(metricsErr, err)
		}
	}
	if err := m.reportValidationFailedMetrics(ctx, c, c.OpenshiftVersion, c.EmailDomain); err != nil {
		m.log.WithError(err).Errorf("Failed to report metrics for failed validations on cluster %s", c.ID)
		metricsErr = multierror.Append(metricsErr, err)
	}
	if metricsErr != nil {
		return metricsErr
	}

	// Delete discovery image for deregistered cluster
	discoveryImage := fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, c.ID.String()))
	exists, err := m.objectHandler.DoesObjectExist(ctx, discoveryImage)
	if err != nil {
		m.log.WithError(err).Errorf("Failed to find cluster discovery image %s", discoveryImage)
		return err
	}
	if exists {
		_, err = m.objectHandler.DeleteObject(ctx, discoveryImage)
		if err != nil {
			m.log.WithError(err).Errorf("Failed to delete cluster discovery image %s", discoveryImage)
			return err
		}
	}

	err = m.registrationAPI.DeregisterCluster(ctx, c)
	if err != nil {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError,
			fmt.Sprintf("Failed to deregister cluster. Error: %s", err.Error()), time.Now())
	} else {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
			fmt.Sprintf("Deregistered cluster: %s", c.ID.String()), time.Now())
	}
	return err
}

func (m *Manager) reportValidationFailedMetrics(ctx context.Context, c *common.Cluster, ocpVersion, emailDomain string) error {
	log := logutil.FromContext(ctx, m.log)
	if c.ValidationsInfo == "" {
		log.Warnf("Cluster %s doesn't contain any validations info, cannot report metrics for that cluster", *c.ID)
		return nil
	}
	var validationRes ValidationsStatus
	if err := json.Unmarshal([]byte(c.ValidationsInfo), &validationRes); err != nil {
		log.WithError(err).Errorf("Failed to unmarshal validations info from cluster %s", *c.ID)
		return err
	}
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.Status == ValidationFailure {
				m.metricAPI.ClusterValidationFailed(ocpVersion, emailDomain, models.ClusterValidationID(v.ID))
			}
		}
	}
	return nil
}

func (m *Manager) reportValidationStatusChanged(ctx context.Context, c *common.Cluster,
	newValidationRes, currentValidationRes ValidationsStatus) {
	for vCategory, vRes := range newValidationRes {
		for _, v := range vRes {
			if currentStatus, ok := m.getValidationStatus(currentValidationRes, vCategory, v.ID); ok {
				if v.Status == ValidationFailure && currentStatus == ValidationSuccess {
					m.metricAPI.ClusterValidationChanged(c.OpenshiftVersion, c.EmailDomain, models.ClusterValidationID(v.ID))
					eventMsg := fmt.Sprintf("Cluster validation '%s' that used to succeed is now failing", v.ID)
					m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityWarning, eventMsg, time.Now())
				}
				if v.Status == ValidationSuccess && currentStatus == ValidationFailure {
					eventMsg := fmt.Sprintf("Cluster validation '%s' is now fixed", v.ID)
					m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo, eventMsg, time.Now())
				}
			}
		}
	}
}

func (m *Manager) getValidationStatus(vs ValidationsStatus, category string, vID ValidationID) (ValidationStatus, bool) {
	for _, v := range vs[category] {
		if v.ID == vID {
			return v.Status, true
		}
	}
	return ValidationStatus(""), false
}

func GetValidations(c *common.Cluster) (ValidationsStatus, error) {
	var currentValidationRes ValidationsStatus
	if c.ValidationsInfo != "" {
		if err := json.Unmarshal([]byte(c.ValidationsInfo), &currentValidationRes); err != nil {
			return ValidationsStatus{}, errors.Wrapf(err, "Failed to unmarshal validations info from cluster %s", *c.ID)
		}
	}
	return currentValidationRes, nil
}

func (m *Manager) didValidationChanged(ctx context.Context, newValidationRes, currentValidationRes ValidationsStatus) bool {
	if len(newValidationRes) == 0 {
		// in order to be considered as a change, newValidationRes should not contain less data than currentValidations
		return false
	}
	return !reflect.DeepEqual(newValidationRes, currentValidationRes)
}

func (m *Manager) updateValidationsInDB(ctx context.Context, db *gorm.DB, c *common.Cluster, newValidationRes ValidationsStatus) (*common.Cluster, error) {
	b, err := json.Marshal(newValidationRes)
	if err != nil {
		return nil, err
	}
	return UpdateCluster(logutil.FromContext(ctx, m.log), db, *c.ID, *c.Status, "validations_info", string(b))
}

func (m *Manager) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	//new transition code
	if db == nil {
		db = m.db
	}
	cluster, err := common.GetClusterFromDBWithoutDisabledHosts(db, *c.ID)
	if err != nil {
		return nil, err
	}
	return m.refreshStatusInternal(ctx, cluster, db)
}

func (m *Manager) refreshStatusInternal(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	//new transition code
	if db == nil {
		db = m.db
	}
	var (
		vc               *clusterPreprocessContext
		err              error
		conditions       map[string]bool
		newValidationRes map[string][]ValidationResult
	)
	vc = newClusterValidationContext(c, db)
	conditions, newValidationRes, err = m.rp.preprocess(ctx, vc)
	if err != nil {
		return c, err
	}
	currentValidationRes, err := GetValidations(c)
	if err != nil {
		return nil, err
	}
	if m.didValidationChanged(ctx, newValidationRes, currentValidationRes) {
		// Validation status changes are detected when new validations are different from the
		// current validations in the DB.
		// For changes to be detected and reported correctly, the comparison needs to be
		// performed before the new validations are updated to the DB.
		m.reportValidationStatusChanged(ctx, c, newValidationRes, currentValidationRes)
		if _, err = m.updateValidationsInDB(ctx, db, c, newValidationRes); err != nil {
			return nil, err
		}
	}
	args := &TransitionArgsRefreshCluster{
		ctx:               ctx,
		db:                db,
		eventHandler:      m.eventsHandler,
		metricApi:         m.metricAPI,
		hostApi:           m.hostAPI,
		conditions:        conditions,
		validationResults: newValidationRes,
		clusterAPI:        m,
		objectHandler:     m.objectHandler,
		ocmClient:         m.ocmClient,
		dnsApi:            m.dnsApi,
	}

	err = m.sm.Run(TransitionTypeRefreshStatus, newStateCluster(vc.cluster), args)
	if err != nil {
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	ret := args.updatedCluster
	if ret == nil {
		ret = c
	}
	return ret, err
}

func (m *Manager) SetUploadControllerLogsAt(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	err := db.Model(c).Update("controller_logs_collected_at", strfmt.DateTime(time.Now())).Error
	if err != nil {
		return errors.Wrapf(err, "failed to set controller_logs_collected_at to cluster %s", c.ID.String())
	}
	return nil
}

func (m *Manager) Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	return m.installationAPI.Install(ctx, c, db)
}

func (m *Manager) GetMasterNodesIds(ctx context.Context, c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return m.installationAPI.GetMasterNodesIds(ctx, c, db)
}

func (m *Manager) tryAssignMachineCidrDHCPMode(cluster *common.Cluster) error {
	networks := network.GetClusterNetworks(cluster.Hosts, m.log)
	if len(networks) == 1 {
		/*
		 * Auto assign machine network CIDR is relevant if there is only single host network.  Otherwise the user
		 * has to select the machine network CIDR
		 */
		return m.db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Update(&common.Cluster{
			Cluster: models.Cluster{
				MachineNetworkCidr: networks[0],
			},
			MachineNetworkCidrUpdatedAt: time.Now(),
		}).Error
	}
	return nil
}

func (m *Manager) tryAssignMachineCidrNonDHCPMode(cluster *common.Cluster) error {
	machineCidr, err := network.CalculateMachineNetworkCIDR(
		cluster.APIVip, cluster.IngressVip, cluster.Hosts, false)

	if err != nil {
		return err
	} else if machineCidr == cluster.MachineNetworkCidr {
		return nil
	}

	return m.db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Update(
		"machine_network_cidr", machineCidr,
		"machine_network_cidr_updated_at", time.Now(),
	).Error
}

func (m *Manager) autoAssignMachineNetworkCidr(c *common.Cluster) error {
	if !funk.ContainsString([]string{models.ClusterStatusPendingForInput, models.ClusterStatusInsufficient}, swag.StringValue(c.Status)) {
		return nil
	}
	/*
	 * This handles two cases: Auto assign for DHCP mode when there is a single host network in cluster and in non DHCP mode
	 * when at least one of the VIPs is defined.
	 * In DHCP mode the aim is to get from DB only clusters that are candidates for machine network CIDR auto assign
	 * The cluster query is for clusters that have their DHCP mode set (vip_dhcp_allocation), the machine network CIDR empty, and in status insufficient, or pending for input.
	 */
	var err error
	if swag.BoolValue(c.VipDhcpAllocation) {
		err = m.tryAssignMachineCidrDHCPMode(c)
	} else if !swag.BoolValue(c.UserManagedNetworking) {
		err = m.tryAssignMachineCidrNonDHCPMode(c)
	}
	if err != nil {
		m.log.WithError(err).Warnf("Set machine cidr for cluster %s", c.ID.String())
	}
	return err
}

func (m *Manager) shouldTriggerLeaseTimeoutEvent(c *common.Cluster, curMonitorInvokedAt time.Time) bool {
	notAllowedStates := []string{models.ClusterStatusInstalled, models.ClusterStatusError, models.ClusterStatusCancelled}
	if funk.Contains(notAllowedStates, *c.Status) {
		return false
	}
	timeToCompare := c.MachineNetworkCidrUpdatedAt.Add(DhcpLeaseTimeoutMinutes * time.Minute)
	return swag.BoolValue(c.VipDhcpAllocation) && (c.APIVip == "" || c.IngressVip == "") && c.MachineNetworkCidr != "" &&
		(m.prevMonitorInvokedAt.Before(timeToCompare) || m.prevMonitorInvokedAt.Equal(timeToCompare)) &&
		curMonitorInvokedAt.After(timeToCompare)
}

func (m *Manager) triggerLeaseTimeoutEvent(ctx context.Context, c *common.Cluster) {
	m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityWarning, "API and Ingress VIPs lease allocation has been timed out", time.Now())
}

func (m *Manager) SkipMonitoring(c *common.Cluster) bool {
	// logs required monitoring on error state until IsLogCollectionTimedOut move the logs state to timeout,
	// or remote controllers reports that their log colection has been completed. Then, monitoring should be
	// stopped to avoid excessive computation
	skipMonitoringStates := []string{string(models.LogsStateCompleted), string(models.LogsStateTimeout)}
	result := ((swag.StringValue(c.Status) == models.ClusterStatusError || swag.StringValue(c.Status) == models.ClusterStatusCancelled) &&
		funk.Contains(skipMonitoringStates, c.LogsInfo))
	return result
}

func (m *Manager) initMonitorQueryGenerator() {
	if m.monitorQueryGenerator == nil {
		noNeedToMonitorInStates := []string{
			models.ClusterStatusInstalled,
		}

		dbWithCondition := m.db.Preload("Hosts", "status <> ?", models.HostStatusDisabled).Preload(common.MonitoredOperatorsTable).
			Where("status NOT IN (?)", noNeedToMonitorInStates)
		m.monitorQueryGenerator = common.NewMonitorQueryGenerator(m.db, dbWithCondition, m.MonitorBatchSize)
	}
}

func (m *Manager) ClusterMonitoring() {
	if !m.leaderElector.IsLeader() {
		m.log.Debugf("Not a leader, exiting ClusterMonitoring")
		return
	}
	m.log.Debugf("Running ClusterMonitoring")
	defer commonutils.MeasureOperation("ClusterMonitoring", m.log, m.metricAPI)()
	var (
		offset              int
		limit               = m.MonitorBatchSize
		monitored           int64
		clusters            []*common.Cluster
		clusterAfterRefresh *common.Cluster
		requestID           = requestid.NewID()
		ctx                 = requestid.ToContext(context.Background(), requestID)
		log                 = requestid.RequestIDLogger(m.log, requestID)
		err                 error
	)

	curMonitorInvokedAt := time.Now()
	defer func() {
		m.prevMonitorInvokedAt = curMonitorInvokedAt
	}()

	//no need to refresh cluster status if the cluster is in the following statuses
	//when cluster is in error. it should be still monitored until all the logs are collected.
	//Then, SkipMonitoring() stops the logic from running forever
	m.initMonitorQueryGenerator()

	query := m.monitorQueryGenerator.NewQuery()
	for {
		clusters, err = query.Next()
		if err != nil {
			log.WithError(err).Errorf("failed to get clusters")
			return
		}
		if len(clusters) == 0 {
			break
		}
		for _, cluster := range clusters {
			if !m.leaderElector.IsLeader() {
				m.log.Debugf("Not a leader, exiting ClusterMonitoring")
				return
			}
			if !m.SkipMonitoring(cluster) {
				monitored += 1
				_ = m.autoAssignMachineNetworkCidr(cluster)
				if err = m.setConnectivityMajorityGroupsForClusterInternal(cluster, m.db); err != nil {
					log.WithError(err).Error("failed to set majority group for clusters")
				}
				clusterAfterRefresh, err = m.refreshStatusInternal(ctx, cluster, m.db)
				if err != nil {
					log.WithError(err).Errorf("failed to refresh cluster %s state", cluster.ID)
					continue
				}

				if swag.StringValue(clusterAfterRefresh.Status) != swag.StringValue(cluster.Status) {
					log.Infof("cluster %s updated status from %s to %s via monitor", cluster.ID,
						swag.StringValue(cluster.Status), swag.StringValue(clusterAfterRefresh.Status))
				}

				if m.shouldTriggerLeaseTimeoutEvent(cluster, curMonitorInvokedAt) {
					m.triggerLeaseTimeoutEvent(ctx, cluster)
				}
			}
		}
		offset += limit
	}
	m.metricAPI.MonitoredClusterCount(monitored)
}

func CanDownloadFiles(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{
		models.ClusterStatusInstalling,
		models.ClusterStatusFinalizing,
		models.ClusterStatusInstalled,
		models.ClusterStatusError,
		models.ClusterStatusAddingHosts,
		models.ClusterStatusCancelled,
	}
	if !funk.Contains(allowedStatuses, clusterStatus) {
		err = errors.Errorf("cluster %s is in %s state, files can be downloaded only when status is one of: %s",
			c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func CanDownloadKubeconfig(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{
		models.ClusterStatusFinalizing,
		models.ClusterStatusInstalled,
	}
	if !funk.Contains(allowedStatuses, clusterStatus) {
		err = errors.Errorf("cluster %s is in %s state, %s can be downloaded only when status is one of: %s",
			c.ID, clusterStatus, constants.Kubeconfig, allowedStatuses)
	}

	return err
}

func (m *Manager) IsOperatorAvailable(c *common.Cluster, operatorName string) bool {
	// TODO: MGMT-4458
	// Backward-compatible solution for clusters that don't have monitored operators data
	if len(c.MonitoredOperators) == 0 {
		clusterStatus := swag.StringValue(c.Status)
		allowedStatuses := []string{models.ClusterStatusInstalling, models.ClusterStatusFinalizing, models.ClusterStatusInstalled}
		return funk.ContainsString(allowedStatuses, clusterStatus)
	}

	for _, o := range c.MonitoredOperators {
		if o.Name == operatorName {
			return o.Status == models.OperatorStatusAvailable
		}
	}
	return false
}

func (m *Manager) UploadIngressCert(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{models.ClusterStatusFinalizing, models.ClusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, upload ingress ca can be done only in %s or %s state", c.ID, clusterStatus, models.ClusterStatusFinalizing, models.ClusterStatusInstalled)
	}
	return err
}

func (m *Manager) AcceptRegistration(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{models.ClusterStatusInsufficient, models.ClusterStatusReady, models.ClusterStatusPendingForInput, models.ClusterStatusAddingHosts}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		if clusterStatus == models.ClusterStatusInstalled {
			err = errors.Errorf("Cannot add host to a cluster that is already installed, please use the day2 cluster option")
		} else {
			err = errors.Errorf("Host can register only in one of the following states: %s", allowedStatuses)
		}
	}
	return err
}

func (m *Manager) VerifyClusterUpdatability(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{models.ClusterStatusInsufficient, models.ClusterStatusReady, models.ClusterStatusPendingForInput, models.ClusterStatusAddingHosts}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, cluster can be updated only in one of %s", c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) CancelInstallation(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := "Cancelled cluster installation"
	lastState := newStateCluster(c)
	installationStates := []string{
		models.ClusterStatusPreparingForInstallation, models.ClusterStatusInstalling, models.ClusterStatusFinalizing}
	defer func() {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, eventSeverity, eventInfo, time.Now())
		//metrics for cancel as final state are calculated only when the transition to cancel was made
		//from one of the installing states
		if funk.Contains(installationStates, lastState.srcState) {
			m.metricAPI.ClusterInstallationFinished(ctx, models.ClusterStatusCancelled, lastState.srcState, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
		}
	}()

	err := m.sm.Run(TransitionTypeCancelInstallation, lastState, &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to cancel installation: %s", err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) UpdateLogsProgress(ctx context.Context, c *common.Cluster, progress string) error {
	err := updateLogsProgress(logutil.FromContext(ctx, m.log), m.db, c, swag.StringValue(c.Status), progress)
	return err
}

func (m *Manager) UpdateAmsSubscriptionID(ctx context.Context, clusterID, amsSubscriptionID strfmt.UUID) *common.ApiErrorResponse {
	log := logutil.FromContext(ctx, m.log)
	if err := m.db.Model(&common.Cluster{}).Where("id = ?", clusterID.String()).Update("ams_subscription_id", amsSubscriptionID).Error; err != nil {
		log.WithError(err).Errorf("Failed to patch DB with AMS subscription ID for cluster %v", clusterID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (m *Manager) ResetCluster(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := "Reset cluster installation"
	defer func() {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, eventSeverity, eventInfo, time.Now())
	}()

	err := m.sm.Run(TransitionTypeResetCluster, newStateCluster(c), &TransitionArgsResetCluster{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to reset installation. Error: %s", err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) PrepareForInstallation(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	err := m.sm.Run(TransitionTypePrepareForInstallation, newStateCluster(c),
		&TransitionArgsPrepareForInstallation{
			ctx:                ctx,
			db:                 db,
			manifestsGenerator: m.manifestsGeneratorAPI,
			metricApi:          m.metricAPI,
		},
	)
	return err
}

func (m *Manager) HandlePreInstallError(ctx context.Context, c *common.Cluster, installErr error) {
	log := logutil.FromContext(ctx, m.log)
	log.WithError(installErr).Warnf("Failed to prepare installation of cluster %s", c.ID.String())
	err := m.db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).Update(&common.Cluster{
		InstallationPreparationCompletionStatus: common.InstallationPreparationFailed,
	}).Error
	if err != nil {
		log.WithError(err).Errorf("Failed to handle pre installation error for cluster %s", c.ID.String())
	} else {
		log.Infof("Successfully handled pre-installation error, cluster %s", c.ID.String())
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityWarning, "Failed to prepare the installation due to an unexpected error. Please retry later", time.Now())
	}
}

func (m *Manager) HandlePreInstallSuccess(ctx context.Context, c *common.Cluster) {
	log := logutil.FromContext(ctx, m.log)
	err := m.db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).Update(&common.Cluster{
		InstallationPreparationCompletionStatus: common.InstallationPreparationSucceeded,
	}).Error
	if err != nil {
		log.WithError(err).Errorf("Failed to handle pre installation success for cluster %s", c.ID.String())
	} else {
		log.Infof("Successfully handled pre-installation success, cluster %s", c.ID.String())
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo, "Cluster was prepared successfully for installation", time.Now())
	}
}

func vipMismatchError(apiVip, ingressVip string, cluster *common.Cluster) error {
	return errors.Errorf("Got VIPs different than those that are stored in the DB for cluster %s. APIVip = %s @db = %s, IngressVIP = %s @db = %s",
		cluster.ID.String(), apiVip, cluster.APIVip, ingressVip, cluster.IngressVip)
}

func (m *Manager) SetVipsData(ctx context.Context, c *common.Cluster, apiVip, ingressVip, apiVipLease, ingressVipLease string, db *gorm.DB) error {
	var err error
	if db == nil {
		db = m.db
	}
	log := logutil.FromContext(ctx, m.log)
	formattedApiLease := network.FormatLease(apiVipLease)
	formattedIngressVip := network.FormatLease(ingressVipLease)
	if apiVip == c.APIVip &&
		ingressVip == c.IngressVip &&
		formattedApiLease == c.ApiVipLease &&
		formattedIngressVip == c.IngressVipLease {
		return nil
	}
	switch swag.StringValue(c.Status) {
	case models.ClusterStatusPendingForInput, models.ClusterStatusInsufficient, models.ClusterStatusReady:
		if err = db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).
			Updates(map[string]interface{}{
				"api_vip":           apiVip,
				"ingress_vip":       ingressVip,
				"api_vip_lease":     formattedApiLease,
				"ingress_vip_lease": formattedIngressVip,
			}).Error; err != nil {
			log.WithError(err).Warnf("Update vips of cluster %s", c.ID.String())
			return err
		}
		if apiVip != c.APIVip || c.IngressVip != ingressVip {
			if c.APIVip != "" || c.IngressVip != "" {
				log.WithError(vipMismatchError(apiVip, ingressVip, c)).Warn("VIPs changed")
			}
			m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
				fmt.Sprintf("Cluster was updated with api-vip %s, ingress-vip %s", apiVip, ingressVip), time.Now())
		}

	case models.ClusterStatusInstalling, models.ClusterStatusPreparingForInstallation, models.ClusterStatusFinalizing:
		if c.APIVip != apiVip || c.IngressVip != ingressVip {
			err = vipMismatchError(apiVip, ingressVip, c)
			log.WithError(err).Error("VIPs changed during installation")

			// TODO move cluster to error
			return err
		}
	}
	return nil
}

func (m *Manager) CreateTarredClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) (string, error) {
	log := logutil.FromContext(ctx, m.log)
	fileName := fmt.Sprintf("%s/logs/cluster_logs.tar", c.ID)
	files, err := objectHandler.ListObjectsByPrefix(ctx, fmt.Sprintf("%s/logs/", c.ID))
	if err != nil {
		return "", common.NewApiError(http.StatusNotFound, err)
	}
	files = funk.Filter(files, func(x string) bool {
		return x != fileName
	}).([]string)

	var tarredFilenames []string
	var tarredFilename string
	for _, file := range files {
		fileNameSplit := strings.Split(file, "/")
		tarredFilename = file
		if len(fileNameSplit) > 1 {
			if _, err = uuid.Parse(fileNameSplit[len(fileNameSplit)-2]); err == nil {
				hostId := fileNameSplit[len(fileNameSplit)-2]
				for _, hostObject := range c.Hosts {
					if hostObject.ID.String() != hostId {
						continue
					}
					role := string(hostObject.Role)
					if hostObject.Bootstrap {
						role = string(models.HostRoleBootstrap)
					}
					tarredFilename = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), role, sanitize.Name(hostutil.GetHostnameForMsg(hostObject)))
				}
			} else {
				tarredFilename = fmt.Sprintf("%s_%s", fileNameSplit[len(fileNameSplit)-2], fileNameSplit[len(fileNameSplit)-1])
			}
		}
		tarredFilenames = append(tarredFilenames, tarredFilename)
	}

	if len(files) < 1 {
		return "", common.NewApiError(http.StatusNotFound,
			errors.Errorf("No log files were found"))
	}

	log.Debugf("List of files to include into %s is %s", fileName, files)
	err = s3wrapper.TarAwsFiles(ctx, fileName, files, tarredFilenames, objectHandler, log)
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s", fileName)
		return "", common.NewApiError(http.StatusInternalServerError, err)
	}
	return fileName, nil
}

func (m *Manager) IsReadyForInstallation(c *common.Cluster) (bool, string) {
	if swag.StringValue(c.Status) != models.ClusterStatusReady {
		return false, swag.StringValue(c.StatusInfo)
	}
	return true, ""
}

func (m *Manager) setConnectivityMajorityGroupsForClusterInternal(cluster *common.Cluster, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	// We want to calculate majority groups only when in pre-install states since it is needed for pre-install validations
	allowedStates := []string{
		models.ClusterStatusPendingForInput,
		models.ClusterStatusInsufficient,
		models.ClusterStatusReady,
	}
	if !funk.ContainsString(allowedStates, swag.StringValue(cluster.Status)) {
		return nil
	}

	hosts := cluster.Hosts
	/*
		We want the resulting hosts to be always in the same order.  Otherwise, there might be cases that we will get different
		connectivity string (see marshalledMajorityGroups below), for the same connectivity group result.
	*/
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].ID.String() < hosts[j].ID.String()
	})
	majorityGroups := make(map[string][]strfmt.UUID)
	for _, cidr := range network.GetClusterNetworks(hosts, m.log) {
		majorityGroup, err := network.CreateL2MajorityGroup(cidr, hosts)
		if err != nil {
			m.log.WithError(err).Warnf("Create majority group for %s", cidr)
			continue
		}
		majorityGroups[cidr] = majorityGroup
	}

	for _, family := range []network.AddressFamily{network.IPv4, network.IPv6} {
		majorityGroup, err := network.CreateL3MajorityGroup(hosts, family)
		if err != nil {
			m.log.WithError(err).Warnf("Create L3 majority group for cluster %s failed", cluster.ID.String())
		} else {
			majorityGroups[family.String()] = majorityGroup
		}
	}
	b, err := json.Marshal(&majorityGroups)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	marshalledMajorityGroups := string(b)
	if marshalledMajorityGroups != cluster.ConnectivityMajorityGroups {
		err = db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Update(&common.Cluster{
			Cluster: models.Cluster{
				ConnectivityMajorityGroups: marshalledMajorityGroups,
			},
		}).Error
		if err != nil {
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}

func (m *Manager) SetConnectivityMajorityGroupsForCluster(clusterID strfmt.UUID, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	// We want to calculate majority groups only when in pre-install states since it is needed for pre-install validations
	var cluster common.Cluster
	if err := db.Preload("Hosts", "status <> ?", models.HostStatusDisabled).Take(&cluster, "id = ?", clusterID.String()).Error; err != nil {
		var statusCode int32 = http.StatusInternalServerError
		if gorm.IsRecordNotFoundError(err) {
			statusCode = http.StatusNotFound
		}
		return common.NewApiError(statusCode, errors.Wrapf(err, "Getting cluster %s", clusterID.String()))
	}
	return m.setConnectivityMajorityGroupsForClusterInternal(&cluster, db)
}

func (m *Manager) deleteClusterFiles(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API, folder string) error {
	log := logutil.FromContext(ctx, m.log)
	path := filepath.Join(string(*c.ID), folder) + "/"
	files, err := objectHandler.ListObjectsByPrefix(ctx, path)
	if err != nil {
		msg := fmt.Sprintf("Failed to list files in %s", path)
		m.log.WithError(err).Errorf(msg)
		return common.NewApiError(
			http.StatusInternalServerError,
			errors.Errorf(msg))
	}

	var failedToDelete []string
	for _, file := range files {
		//skip log and manifests deletion when deleting cluster files
		if folder == "" && (strings.Contains(file, "logs") || strings.Contains(file, "manifests")) {
			continue
		}
		log.Debugf("Deleting cluster %s S3 file: %s", c.ID.String(), file)
		_, err = objectHandler.DeleteObject(ctx, file)
		if err != nil {
			m.log.WithError(err).Errorf("failed deleting s3 file: %s", file)
			failedToDelete = append(failedToDelete, file)
		}
	}

	if len(failedToDelete) > 0 {
		return common.NewApiError(
			http.StatusInternalServerError,
			errors.Errorf("failed to delete s3 files: %q", failedToDelete))
	}
	return nil
}

func (m *Manager) deleteClusterManifests(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error {
	return m.deleteClusterFiles(ctx, c, objectHandler, "manifests")
}

func (m *Manager) DeleteClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error {
	return m.deleteClusterFiles(ctx, c, objectHandler, "logs")
}

func (m *Manager) DeleteClusterFiles(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error {
	return m.deleteClusterFiles(ctx, c, objectHandler, "")
}

func (m Manager) DeregisterInactiveCluster(ctx context.Context, maxDeregisterPerInterval int, inactiveSince strfmt.DateTime) error {
	log := logutil.FromContext(ctx, m.log)

	var clusters []*common.Cluster

	if err := m.db.Limit(maxDeregisterPerInterval).Where("updated_at < ?", inactiveSince).Find(&clusters).Error; err != nil {
		return err
	}
	for _, c := range clusters {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
			"Cluster is deregistered due to inactivity", time.Now())
		log.Infof("Cluster %s is deregistered due to inactivity since %s", c.ID, c.UpdatedAt)
		if err := m.DeregisterCluster(ctx, c); err != nil {
			log.WithError(err).Errorf("failed to deregister inactive cluster %s ", c.ID)
			continue
		}
	}
	return nil
}

func (m Manager) PermanentClustersDeletion(ctx context.Context, olderThan strfmt.DateTime, objectHandler s3wrapper.API) error {
	var clusters []*common.Cluster
	db := m.db.Unscoped()
	if reply := db.Where("deleted_at < ?", olderThan).Find(&clusters); reply.Error != nil {
		return reply.Error
	}
	for i := range clusters {

		c := clusters[i]
		m.log.Infof("Permanently deleting cluster %s that was de-registered before %s", c.ID.String(), olderThan)

		deleteFromDB := true
		if err := m.DeleteClusterFiles(ctx, c, objectHandler); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting s3 files of cluster %s", c.ID.String())
		}
		if err := m.DeleteClusterLogs(ctx, c, objectHandler); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting s3 logs of cluster %s", c.ID.String())
		}
		if err := m.deleteClusterManifests(ctx, c, objectHandler); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting s3 manifests of cluster %s", c.ID.String())
		}
		if _, err := objectHandler.DeleteObject(ctx, c.ID.String()); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting cluster directory %s", c.ID.String())
		}
		if !deleteFromDB {
			continue
		}

		cluster := c // Avoid passing loop variable by ref
		if reply := db.Delete(&cluster); reply.Error != nil {
			m.log.WithError(reply.Error).Warnf("Failed deleting cluster from db %s", c.ID.String())
		} else if reply.RowsAffected > 0 {
			m.log.Debugf("Deleted %s cluster from db", reply.RowsAffected)
		}

		if err := common.DeleteRecordsByClusterID(db, *c.ID, models.Event{}); err != nil {
			m.log.WithError(err).Warnf("Failed deleting events from db for cluster %s", c.ID.String())
		}

		if err := common.DeleteRecordsByClusterID(db, *c.ID, models.MonitoredOperator{}); err != nil {
			m.log.WithError(err).Warnf("Failed deleting operators from db for cluster %s", c.ID.String())
		}
	}
	return nil
}

func (m *Manager) GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error) {
	c, err := common.GetClusterFromDBWhere(m.db, common.UseEagerLoading, common.SkipDeletedRecords, "kube_key_name = ? and kube_key_namespace = ?", key.Name, key.Namespace)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) GenerateAdditionalManifests(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, m.log)
	if err := m.manifestsGeneratorAPI.AddChronyManifest(ctx, log, cluster); err != nil {
		return errors.Wrap(err, "failed to add chrony manifest")
	}

	if common.IsSingleNodeCluster(cluster) && m.EnableSingleNodeDnsmasq {
		if err := m.manifestsGeneratorAPI.AddDnsmasqForSingleNode(ctx, log, cluster); err != nil {
			return errors.Wrap(err, "failed to add dnsmasq manifest")
		}
	}

	if err := m.rp.operatorsAPI.GenerateManifests(ctx, cluster); err != nil {
		return errors.Wrap(err, "failed to add operator manifests")
	}
	if err := m.manifestsGeneratorAPI.AddTelemeterManifest(ctx, log, cluster); err != nil {
		return errors.Wrap(err, "failed to add telemeter manifest")
	}

	return nil
}

func (m *Manager) CompleteInstallation(ctx context.Context, db *gorm.DB,
	cluster *common.Cluster, successfullyFinished bool, reason string) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, m.log)
	destStatus := models.ClusterStatusError
	result := models.ClusterStatusInstalled
	severity := models.EventSeverityInfo
	eventMsg := fmt.Sprintf("Successfully finished installing cluster %s", cluster.Name)

	defer func() {
		m.metricAPI.ClusterInstallationFinished(ctx, result, models.ClusterStatusFinalizing, cluster.OpenshiftVersion,
			*cluster.ID, cluster.EmailDomain, cluster.InstallStartedAt)
	}()

	if successfullyFinished {
		destStatus = models.ClusterStatusInstalled

		// Update AMS subscription only if configured and installation succeeded
		if m.ocmClient != nil && m.ocmClient.Config.WithAMSSubscriptions {
			if err := m.ocmClient.AccountsMgmt.UpdateSubscriptionStatusActive(ctx, cluster.AmsSubscriptionID); err != nil {
				err = errors.Wrapf(err, "Failed to update AMS subscription for cluster %s with status 'Active'", *cluster.ID)
				log.Error(err)
				return nil, err
			}
		}
	}

	clusterAfterUpdate, err := updateClusterStatus(ctx, log, db, *cluster.ID, models.ClusterStatusFinalizing,
		destStatus, reason, m.eventsHandler,
	)
	if err != nil {
		err = errors.Wrapf(err, "Failed to update cluster %s completion in db", *cluster.ID)
		log.Error(err)
		return nil, err
	}

	if !successfullyFinished {
		result = models.ClusterStatusError
		severity = models.EventSeverityCritical
		eventMsg = fmt.Sprintf("Failed installing cluster %s. Reason: %s", cluster.Name, reason)
	}
	m.eventsHandler.AddEvent(ctx, *cluster.ID, nil, severity, eventMsg, time.Now())
	return clusterAfterUpdate, nil
}
