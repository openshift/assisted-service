package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"k8s.io/apimachinery/pkg/types"
)

const DhcpLeaseTimeoutMinutes = 2

var S3FileNames = []string{
	"kubeconfig",
	"bootstrap.ign",
	"master.ign",
	"worker.ign",
	"metadata.json",
	"kubeadmin-password",
	"kubeconfig-noingress",
	"install-config.yaml",
	"discovery.ign",
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
	GetCredentials(c *common.Cluster) (err error)
	UploadIngressCert(c *common.Cluster) (err error)
	VerifyClusterUpdatability(c *common.Cluster) (err error)
	AcceptRegistration(c *common.Cluster) (err error)
	CancelInstallation(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetCluster(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	PrepareForInstallation(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	HandlePreInstallError(ctx context.Context, c *common.Cluster, err error)
	CompleteInstallation(ctx context.Context, c *common.Cluster, successfullyFinished bool, reason string) *common.ApiErrorResponse
	SetVipsData(ctx context.Context, c *common.Cluster, apiVip, ingressVip, apiVipLease, ingressVipLease string, db *gorm.DB) error
	IsReadyForInstallation(c *common.Cluster) (bool, string)
	CreateTarredClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) (string, error)
	SetUploadControllerLogsAt(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	SetConnectivityMajorityGroupsForCluster(clusterID strfmt.UUID, db *gorm.DB) error
	DeleteClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error
	DeleteClusterFiles(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error
	PermanentClustersDeletion(ctx context.Context, olderThen strfmt.DateTime, objectHandler s3wrapper.API) error
	UpdateInstallProgress(ctx context.Context, c *common.Cluster, progress string) *common.ApiErrorResponse
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	UpdateAmsSubscriptionID(ctx context.Context, clusterID, amsSubscriptionID strfmt.UUID) *common.ApiErrorResponse
	GenerateAdditionalManifests(ctx context.Context, cluster *common.Cluster) error
}

type PrepareConfig struct {
	InstallationTimeout time.Duration `envconfig:"PREPARE_FOR_INSTALLATION_TIMEOUT" default:"10m"`
}

type Config struct {
	PrepareConfig    PrepareConfig
	MonitorBatchSize int `envconfig:"CLUSTER_MONITOR_BATCH_SIZE" default:"100"`
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
}

func NewManager(cfg Config, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler,
	hostAPI host.API, metricApi metrics.API, manifestsGeneratorAPI network.ManifestsGeneratorAPI,
	leaderElector leader.Leader, operatorsApi operators.API) *Manager {
	th := &transitionHandler{
		log:           log,
		db:            db,
		prepareConfig: cfg.PrepareConfig,
	}
	return &Manager{
		Config:                cfg,
		log:                   log,
		db:                    db,
		registrationAPI:       NewRegistrar(log, db),
		installationAPI:       NewInstaller(log, db),
		eventsHandler:         eventsHandler,
		sm:                    NewClusterStateMachine(th),
		metricAPI:             metricApi,
		manifestsGeneratorAPI: manifestsGeneratorAPI,
		rp:                    newRefreshPreprocessor(log, hostAPI, operatorsApi),
		hostAPI:               hostAPI,
		leaderElector:         leaderElector,
		prevMonitorInvokedAt:  time.Now(),
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
	if metricsErr != nil {
		return metricsErr
	}

	err := m.registrationAPI.DeregisterCluster(ctx, c)
	if err != nil {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError,
			fmt.Sprintf("Failed to deregister cluster. Error: %s", err.Error()), time.Now())
	} else {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
			fmt.Sprintf("Deregistered cluster: %s", c.ID.String()), time.Now())
	}
	return err
}

func (m *Manager) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {

	//new transition code
	if db == nil {
		db = m.db
	}
	vc, err := newClusterValidationContext(*c.ID, db)
	if err != nil {
		return c, err
	}
	conditions, validationsResults, err := m.rp.preprocess(ctx, vc)
	if err != nil {
		return c, err
	}
	err = m.sm.Run(TransitionTypeRefreshStatus, newStateCluster(vc.cluster), &TransitionArgsRefreshCluster{
		ctx:               ctx,
		db:                db,
		eventHandler:      m.eventsHandler,
		metricApi:         m.metricAPI,
		hostApi:           m.hostAPI,
		conditions:        conditions,
		validationResults: validationsResults,
	})
	if err != nil {
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	//return updated cluster
	return common.GetClusterFromDB(db, *c.ID, common.UseEagerLoading)

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

func (m *Manager) tryAssignMachineCidr(cluster *common.Cluster) error {
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

func (m *Manager) autoAssignMachineNetworkCidrs() error {
	var clusters []*common.Cluster
	/*
	 * The aim is to get from DB only clusters that are candidates for machine network CIDR auto assign
	 * The cluster query is for clusters that have their DHCP mode set (vip_dhcp_allocation), the machine network CIDR empty, and in status insufficient, or pending for input.
	 * For these clusters the hosts query is all hosts that are not in status (disabled, disconnected, discovering),
	 * since we want to calculate the host networks only from hosts wkith relevant inventory
	 */
	err := common.LoadHostsFromDB(m.db, "status not in (?)", []string{models.HostStatusDisabled, models.HostStatusDisconnected, models.HostStatusDiscovering}).
		Find(&clusters, "vip_dhcp_allocation = ? and machine_network_cidr = '' and status in (?)", true, []string{models.ClusterStatusPendingForInput, models.ClusterStatusInsufficient}).Error
	if err != nil {
		m.log.WithError(err).Warn("Query for clusters for machine network cidr allocation")
		return err
	}
	for _, cluster := range clusters {
		err = m.tryAssignMachineCidr(cluster)
		if err != nil {
			m.log.WithError(err).Warnf("Set machine cidr for cluster %s", cluster.ID.String())
		}
	}
	return nil
}

func (m *Manager) shouldTriggerLeaseTimeoutEvent(c *common.Cluster, curMonitorInvokedAt time.Time) bool {
	timeToCompare := c.MachineNetworkCidrUpdatedAt.Add(DhcpLeaseTimeoutMinutes * time.Minute)
	return swag.BoolValue(c.VipDhcpAllocation) && (c.APIVip == "" || c.IngressVip == "") && c.MachineNetworkCidr != "" &&
		(m.prevMonitorInvokedAt.Before(timeToCompare) || m.prevMonitorInvokedAt.Equal(timeToCompare)) &&
		curMonitorInvokedAt.After(timeToCompare)
}

func (m *Manager) triggerLeaseTimeoutEvent(ctx context.Context, c *common.Cluster) {
	m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityWarning, "API and Ingress VIPs lease allocation has been timed out", time.Now())
}

func (m *Manager) ClusterMonitoring() {
	if !m.leaderElector.IsLeader() {
		m.log.Debugf("Not a leader, exiting ClusterMonitoring")
		return
	}
	m.log.Debugf("Running ClusterMonitoring")
	var (
		offset              int
		limit               = m.MonitorBatchSize
		clusters            []*common.Cluster
		clusterAfterRefresh *common.Cluster
		requestID           = requestid.NewID()
		ctx                 = requestid.ToContext(context.Background(), requestID)
		log                 = requestid.RequestIDLogger(m.log, requestID)
		err                 error
	)

	_ = m.autoAssignMachineNetworkCidrs()
	curMonitorInvokedAt := time.Now()
	defer func() {
		m.prevMonitorInvokedAt = curMonitorInvokedAt
	}()

	//no need to refresh cluster status if the cluster is in the following statuses
	noNeedToMonitorInStates := []string{
		models.ClusterStatusInstalled,
		models.ClusterStatusError,
	}

	for {
		clusters = make([]*common.Cluster, 0, limit)
		if err = m.db.Where("status NOT IN (?)", noNeedToMonitorInStates).
			Offset(offset).Limit(limit).Order("id").Find(&clusters).Error; err != nil {
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
			if err = m.SetConnectivityMajorityGroupsForCluster(*cluster.ID, m.db); err != nil {
				log.WithError(err).Errorf("failed to set majority group for cluster %s", cluster.ID.String())
			}
			if clusterAfterRefresh, err = m.RefreshStatus(ctx, cluster, m.db); err != nil {
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
		offset += limit
	}
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
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("cluster %s is in %s state, files can be downloaded only when status is one of: %s",
			c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func CanDownloadKubeconfig(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	if clusterStatus != models.ClusterStatusInstalled {
		err = errors.Errorf("cluster %s is in %s state, %s can be downloaded only in installed state", c.ID, clusterStatus, "kubeconfig")
	}

	return err
}
func (m *Manager) GetCredentials(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{models.ClusterStatusInstalling, models.ClusterStatusFinalizing, models.ClusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, credentials are available only in installing or installed state", c.ID, clusterStatus)
	}

	return err
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
		err = errors.Errorf("Cluster %s is in %s state, host can register only in one of %s", c.ID, clusterStatus, allowedStatuses)
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
	log := logutil.FromContext(ctx, m.log)

	eventSeverity := models.EventSeverityInfo
	eventInfo := "Canceled cluster installation"
	defer func() {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, eventSeverity, eventInfo, time.Now())
	}()

	err := m.sm.Run(TransitionTypeCancelInstallation, newStateCluster(c), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to cancel installation: %s", err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}
	//report installation finished metric
	m.metricAPI.ClusterInstallationFinished(log, "canceled", c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
	return nil
}

func (m *Manager) UpdateInstallProgress(ctx context.Context, c *common.Cluster, progress string) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("Update cluster installation progress: %s", progress)
	defer func() {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, eventSeverity, eventInfo, time.Now())
	}()

	err := m.sm.Run(TransitionTypeUpdateInstallationProgress, newStateCluster(c), &TransitionArgsUpdateInstallationProgress{
		ctx:      ctx,
		progress: progress,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to update cluster installation progress. Error: %s", err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}

	return nil
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

func (m *Manager) CompleteInstallation(ctx context.Context, c *common.Cluster, successfullyFinished bool, reason string) *common.ApiErrorResponse {
	log := logutil.FromContext(ctx, m.log)

	err := m.sm.Run(TransitionTypeCompleteInstallation, newStateCluster(c), &TransitionArgsCompleteInstallation{
		ctx:       ctx,
		isSuccess: successfullyFinished,
		reason:    reason,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	result := models.ClusterStatusInstalled
	severity := models.EventSeverityInfo
	eventMsg := fmt.Sprintf("Successfully finished installing cluster %s", c.Name)
	if !successfullyFinished {
		result = models.ClusterStatusError
		severity = models.EventSeverityCritical
		eventMsg = fmt.Sprintf("Failed installing cluster %s. Reason: %s", c.Name, reason)
	}
	m.metricAPI.ClusterInstallationFinished(log, result, c.OpenshiftVersion, *c.ID, c.EmailDomain, c.InstallStartedAt)
	m.eventsHandler.AddEvent(ctx, *c.ID, nil, severity, eventMsg, time.Now())
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
	err := m.sm.Run(TransitionTypeHandlePreInstallationError, newStateCluster(c), &TransitionArgsHandlePreInstallationError{
		ctx:        ctx,
		installErr: installErr,
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to handle pre installation error for cluster %s", c.ID.String())
	} else {
		log.Infof("Successfully handled pre-installation error, cluster %s", c.ID.String())
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
	switch swag.StringValue(c.Status) {
	case models.ClusterStatusPendingForInput, models.ClusterStatusInsufficient, models.ClusterStatusReady:
		if err = db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).
			Updates(map[string]interface{}{
				"api_vip":           apiVip,
				"ingress_vip":       ingressVip,
				"api_vip_lease":     network.FormatLease(apiVipLease),
				"ingress_vip_lease": network.FormatLease(ingressVipLease),
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
	err = common.TarAwsFiles(ctx, fileName, files, tarredFilenames, objectHandler, log)
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

func (m *Manager) SetConnectivityMajorityGroupsForCluster(clusterID strfmt.UUID, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	// We want to calculate majority groups only when in pre-install states since it is needed for pre-install validations
	allowedStates := []string{
		models.ClusterStatusPendingForInput,
		models.ClusterStatusInsufficient,
		models.ClusterStatusReady,
	}
	var cluster common.Cluster
	if err := db.Select("id, status").Take(&cluster, "id = ?", clusterID.String()).Error; err != nil {
		return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Getting cluster %s", clusterID.String()))
	}

	if !funk.ContainsString(allowedStates, swag.StringValue(cluster.Status)) {
		return nil
	}

	var hosts []*models.Host
	if err := db.Order("id").Select("id, connectivity, inventory").Find(&hosts, "cluster_id = ? and status <> ?", clusterID.String(), models.HostStatusDisabled).Error; err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Getting hosts for cluster %s", clusterID.String()))
	}

	majorityGroups := make(map[string][]strfmt.UUID)
	for _, cidr := range network.GetClusterNetworks(hosts, m.log) {
		majorityGroup, err := network.CreateMajorityGroup(cidr, hosts)
		if err != nil {
			m.log.WithError(err).Warnf("Create majority group for %s", cidr)
			continue
		}
		majorityGroups[cidr] = majorityGroup
	}
	b, err := json.Marshal(&majorityGroups)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	err = db.Model(&common.Cluster{}).Where("id = ?", clusterID.String()).Update(&common.Cluster{
		Cluster: models.Cluster{
			ConnectivityMajorityGroups: string(b),
		},
	}).Error
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (m *Manager) DeleteClusterLogs(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error {
	log := logutil.FromContext(ctx, m.log)
	files, err := objectHandler.ListObjectsByPrefix(ctx, fmt.Sprintf("%s/logs/", c.ID))
	if err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	var failedToDelete []string
	for _, file := range files {
		log.Debugf("Deleting cluster %s S3 log file: %s", c.ID.String(), file)
		_, err = objectHandler.DeleteObject(ctx, file)
		if err != nil {
			m.log.WithError(err).Errorf("failed deleting s3 log %s", file)
			failedToDelete = append(failedToDelete, file)
		}
	}

	if len(failedToDelete) > 0 {
		return common.NewApiError(
			http.StatusInternalServerError,
			errors.Errorf("failed to delete s3 logs: %q", failedToDelete))
	}
	return nil
}

func (m *Manager) DeleteClusterFiles(ctx context.Context, c *common.Cluster, objectHandler s3wrapper.API) error {
	var failedToDelete []string
	path := fmt.Sprintf("%s/", c.ID)
	filesList, err := objectHandler.ListObjectsByPrefix(ctx, path)
	if err != nil {
		msg := fmt.Sprintf("Failed to list files in %s", path)
		m.log.WithError(err).Errorf(msg)
		return common.NewApiError(
			http.StatusInternalServerError,
			errors.Errorf(msg))
	}
	for _, fileName := range filesList {
		//skip log deletion
		if strings.Contains(fileName, "logs") {
			continue
		}
		_, err := objectHandler.DeleteObject(ctx, fileName)
		if err != nil {
			m.log.WithError(err).Errorf("failed deleting s3 file %s", fileName)
			failedToDelete = append(failedToDelete, fileName)
		}
	}

	if len(failedToDelete) > 0 {
		return common.NewApiError(
			http.StatusInternalServerError,
			errors.Errorf("failed to delete s3 files: %q", failedToDelete))
	}
	return nil
}

func (m Manager) PermanentClustersDeletion(ctx context.Context, olderThen strfmt.DateTime, objectHandler s3wrapper.API) error {
	var clusters []*common.Cluster
	db := m.db.Unscoped()
	if reply := db.Where("deleted_at < ?", olderThen).Find(&clusters); reply.Error != nil {
		return reply.Error
	}
	for i := range clusters {
		c := clusters[i]
		m.log.Debugf("Deleting all S3 files for cluster: %s", c.ID.String())

		deleteFromDB := true
		if err := m.DeleteClusterFiles(ctx, c, objectHandler); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting s3 files of cluster %s", c.ID.String())
		}
		if err := m.DeleteClusterLogs(ctx, c, objectHandler); err != nil {
			deleteFromDB = false
			m.log.WithError(err).Warnf("Failed deleting s3 logs of cluster %s", c.ID.String())
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

		m.eventsHandler.DeleteClusterEvents(*c.ID)
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
	if err := m.manifestsGeneratorAPI.AddChronyManifest(ctx, logutil.FromContext(ctx, m.log), cluster); err != nil {
		return errors.Wrap(err, "PostPrepareForInstallation failed to add chrony manifest")
	}
	sendNTPMetric(logutil.FromContext(ctx, m.log), m.metricAPI, cluster)

	if common.IsSingleNodeCluster(cluster) {
		if err := m.manifestsGeneratorAPI.AddDnsmasqForSingleNode(ctx, logutil.FromContext(ctx, m.log), cluster); err != nil {
			return errors.Wrap(err, "PostPrepareForInstallation failed to add dnsmasq manifest")
		}
	}
	return nil
}

func sendNTPMetric(log logrus.FieldLogger, metricApi metrics.API, cluster *common.Cluster) {
	ntpFailures := 0

	for _, host := range cluster.Hosts {
		if swag.StringValue(host.Status) == models.HostStatusDisabled || host.NtpSources == "" {
			continue
		}

		var ntpSources []*models.NtpSource
		if err := json.Unmarshal([]byte(host.NtpSources), &ntpSources); err != nil {
			log.Error(errors.Wrapf(err, "Failed to unmarshal %s", host.NtpSources))
			continue
		}

		isHostSynced := false

		for _, source := range ntpSources {
			if source.SourceState == models.SourceStateSynced {
				isHostSynced = true
				break
			}
		}

		if !isHostSynced {
			ntpFailures += 1
		}
	}

	metricApi.ClusterHostsNTPFailures(*cluster.ID, cluster.EmailDomain, ntpFailures)
}
