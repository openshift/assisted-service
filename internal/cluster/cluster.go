package cluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster_api.go

type RegistrationAPI interface {
	// Register a new cluster
	RegisterCluster(ctx context.Context, c *common.Cluster) error
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
	DownloadFiles(c *common.Cluster) (err error)
	DownloadKubeconfig(c *common.Cluster) (err error)
	GetCredentials(c *common.Cluster) (err error)
	UploadIngressCert(c *common.Cluster) (err error)
	VerifyClusterUpdatability(c *common.Cluster) (err error)
	AcceptRegistration(c *common.Cluster) (err error)
	SetGeneratorVersion(c *common.Cluster, version string, db *gorm.DB) error
	CancelInstallation(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetCluster(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse
	PrepareForInstallation(ctx context.Context, c *common.Cluster, db *gorm.DB) error
	HandlePreInstallError(ctx context.Context, c *common.Cluster, err error)
	CompleteInstallation(ctx context.Context, c *common.Cluster, successfullyFinished bool, reason string) *common.ApiErrorResponse
	SetVips(ctx context.Context, c *common.Cluster, apiVip, ingressVip string, db *gorm.DB) error
	IsReadyForInstallation(c *common.Cluster) (bool, string)
}

type PrepareConfig struct {
	InstallationTimeout time.Duration `envconfig:"PREPARE_FOR_INSTALLATION_TIMEOUT" default:"10m"`
}

type Config struct {
	PrepareConfig PrepareConfig
}

type Manager struct {
	Config
	log             logrus.FieldLogger
	db              *gorm.DB
	registrationAPI RegistrationAPI
	installationAPI InstallationAPI
	eventsHandler   events.Handler
	sm              stateswitch.StateMachine
	metricAPI       metrics.API
	hostAPI         host.API
	rp              *refreshPreprocessor
}

func NewManager(cfg Config, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, hostAPI host.API, metricApi metrics.API) *Manager {
	th := &transitionHandler{
		log:           log,
		db:            db,
		prepareConfig: cfg.PrepareConfig,
	}
	return &Manager{
		log:             log,
		db:              db,
		registrationAPI: NewRegistrar(log, db),
		installationAPI: NewInstaller(log, db),
		eventsHandler:   eventsHandler,
		sm:              NewClusterStateMachine(th),
		metricAPI:       metricApi,
		rp:              newRefreshPreprocessor(log, hostAPI),
		hostAPI:         hostAPI,
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

func (m *Manager) DeregisterCluster(ctx context.Context, c *common.Cluster) error {
	err := m.registrationAPI.DeregisterCluster(ctx, c)
	if err != nil {
		m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError,
			fmt.Sprintf("Failed to deregister cluster. Error: %s", err.Error()), time.Now())
	} else {
		m.eventsHandler.DeleteClusterEvents(*c.ID)
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
	conditions, validationsResults, err := m.rp.preprocess(vc)
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
	var clusterAfterRefresh common.Cluster
	if err := db.Preload("Hosts").Take(&clusterAfterRefresh, "id = ?", c.ID.String()).Error; err != nil {
		return nil, errors.Wrapf(err, "failed to get cluster %s", c.ID.String())
	}
	return &clusterAfterRefresh, nil

}

func (m *Manager) Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	return m.installationAPI.Install(ctx, c, db)
}

func (m *Manager) GetMasterNodesIds(ctx context.Context, c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return m.installationAPI.GetMasterNodesIds(ctx, c, db)
}

func (m *Manager) ClusterMonitoring() {
	var (
		clusters            []*common.Cluster
		clusterAfterRefresh *common.Cluster
		requestID           = requestid.NewID()
		ctx                 = requestid.ToContext(context.Background(), requestID)
		log                 = requestid.RequestIDLogger(m.log, requestID)
		err                 error
	)

	if err = m.db.Find(&clusters).Error; err != nil {
		log.WithError(err).Errorf("failed to get clusters")
		return
	}
	for _, cluster := range clusters {
		if clusterAfterRefresh, err = m.RefreshStatus(ctx, cluster, m.db); err != nil {
			log.WithError(err).Errorf("failed to refresh cluster %s state", cluster.ID)
			continue
		}

		if swag.StringValue(clusterAfterRefresh.Status) != swag.StringValue(cluster.Status) {
			log.Infof("cluster %s updated status from %s to %s via monitor", cluster.ID,
				swag.StringValue(cluster.Status), swag.StringValue(clusterAfterRefresh.Status))
		}
	}
}

func (m *Manager) DownloadFiles(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInstalling,
		models.ClusterStatusFinalizing,
		clusterStatusInstalled,
		clusterStatusError}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("cluster %s is in %s state, files can be downloaded only when status is one of: %s",
			c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) DownloadKubeconfig(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	if clusterStatus != clusterStatusInstalled {
		err = errors.Errorf("cluster %s is in %s state, %s can be downloaded only in installed state", c.ID, clusterStatus, "kubeconfig")
	}

	return err
}
func (m *Manager) GetCredentials(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInstalling, models.ClusterStatusFinalizing, clusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, credentials are available only in installing or installed state", c.ID, clusterStatus)
	}

	return err
}

func (m *Manager) UploadIngressCert(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{models.ClusterStatusFinalizing, clusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, upload ingress ca can be done only in %s or %s state", c.ID, clusterStatus, models.ClusterStatusFinalizing, clusterStatusInstalled)
	}
	return err
}

func (m *Manager) AcceptRegistration(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInsufficient, clusterStatusReady, models.ClusterStatusPendingForInput}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, host can register only in one of %s", c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) VerifyClusterUpdatability(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInsufficient, clusterStatusReady, models.ClusterStatusPendingForInput}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, cluster can be updated only in one of %s", c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) SetGeneratorVersion(c *common.Cluster, version string, db *gorm.DB) error {
	return db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).
		Update("ignition_generator_version", version).Error
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
	m.metricAPI.ClusterInstallationFinished(log, "canceled", c.OpenshiftVersion, c.InstallStartedAt)
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
	if !successfullyFinished {
		result = models.ClusterStatusError
	}
	m.metricAPI.ClusterInstallationFinished(log, result, c.OpenshiftVersion, c.InstallStartedAt)
	return nil
}

func (m *Manager) PrepareForInstallation(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	err := m.sm.Run(TransitionTypePrepareForInstallation, newStateCluster(c),
		&TransitionArgsPrepareForInstallation{
			ctx: ctx,
			db:  db,
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

func (m *Manager) SetVips(ctx context.Context, c *common.Cluster, apiVip, ingressVip string, db *gorm.DB) error {
	var err error
	if db == nil {
		db = m.db
	}
	log := logutil.FromContext(ctx, m.log)
	switch swag.StringValue(c.Status) {
	case models.ClusterStatusInsufficient, models.ClusterStatusReady:
		if err = db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).
			Updates(map[string]interface{}{"api_vip": apiVip,
				"ingress_vip": ingressVip}).Error; err != nil {
			log.WithError(err).Warnf("Update vips of cluster %s", c.ID.String())
			return err
		}
		if apiVip != c.APIVip || c.IngressVip != ingressVip {
			if c.APIVip != "" || c.IngressVip != "" {
				log.WithError(vipMismatchError(apiVip, ingressVip, c)).Warn("VIPs changed")
			}
			m.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo,
				fmt.Sprintf("Cluster %s was updated with api-vip %s, ingress-vip %s", c.ID.String(), apiVip, ingressVip), time.Now())
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

func (m *Manager) IsReadyForInstallation(c *common.Cluster) (bool, string) {
	if swag.StringValue(c.Status) != models.ClusterStatusReady {
		return false, swag.StringValue(c.StatusInfo)
	}
	return true, ""
}
