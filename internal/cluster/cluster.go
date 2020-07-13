package cluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const minHostsNeededForInstallation = 3

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster_api.go

type StateAPI interface {
	// Refresh state in case of hosts update7
	RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*UpdateReply, error)
}

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
	StateAPI
	RegistrationAPI
	InstallationAPI
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
	PrepareForInstallation(ctx context.Context, c *common.Cluster) error
	HandlePreInstallError(ctx context.Context, c *common.Cluster, err error)
}

type Config struct {
	PrepareConfig PrepareConfig
}

type Manager struct {
	Config
	log             logrus.FieldLogger
	db              *gorm.DB
	insufficient    StateAPI
	ready           StateAPI
	installing      StateAPI
	installed       StateAPI
	error           StateAPI
	prepare         StateAPI
	registrationAPI RegistrationAPI
	installationAPI InstallationAPI
	eventsHandler   events.Handler
	sm              stateswitch.StateMachine
}

func NewManager(cfg Config, log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler) *Manager {
	th := &transitionHandler{
		log: log,
		db:  db,
	}
	return &Manager{
		log:             log,
		db:              db,
		insufficient:    NewInsufficientState(log, db),
		ready:           NewReadyState(log, db),
		installing:      NewInstallingState(log, db),
		installed:       NewInstalledState(log, db),
		error:           NewErrorState(log, db),
		prepare:         NewPrepareForInstallation(cfg.PrepareConfig, log, db),
		registrationAPI: NewRegistrar(log, db),
		installationAPI: NewInstaller(log, db),
		eventsHandler:   eventsHandler,
		sm:              NewClusterStateMachine(th),
	}
}

func (m *Manager) getCurrentState(status string) (StateAPI, error) {
	switch status {
	case "":
	case models.ClusterStatusInsufficient:
		return m.insufficient, nil
	case models.ClusterStatusReady:
		return m.ready, nil
	case models.ClusterStatusInstalling:
		return m.installing, nil
	case models.ClusterStatusInstalled:
		return m.installed, nil
	case models.ClusterStatusError:
		return m.error, nil
	case models.ClusterStatusPreparingForInstallation:
		return m.prepare, nil
	}
	return nil, fmt.Errorf("not supported cluster status: %s", status)
}

func (m *Manager) RegisterCluster(ctx context.Context, c *common.Cluster) error {
	err := m.registrationAPI.RegisterCluster(ctx, c)
	var msg string
	if err != nil {
		msg = fmt.Sprintf("Failed to register cluster. Error: %s", err.Error())
	} else {
		msg = "Registered cluster"
	}
	m.eventsHandler.AddEvent(ctx, c.ID.String(), msg, time.Now())
	return err
}

func (m *Manager) DeregisterCluster(ctx context.Context, c *common.Cluster) error {
	err := m.registrationAPI.DeregisterCluster(ctx, c)
	var msg string
	if err != nil {
		msg = fmt.Sprintf("Failed to deregister cluster. Error: %s", err.Error())
	} else {
		msg = "Deregistered cluster"
	}
	m.eventsHandler.AddEvent(ctx, c.ID.String(), msg, time.Now())
	return err
}

func (m *Manager) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(c.Status))
	if err != nil {
		return nil, err
	}
	return state.RefreshStatus(ctx, c, db)
}

func (m *Manager) Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	return m.installationAPI.Install(ctx, c, db)
}

func (m *Manager) GetMasterNodesIds(ctx context.Context, c *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return m.installationAPI.GetMasterNodesIds(ctx, c, db)
}

func (m *Manager) ClusterMonitoring() {
	var clusters []*common.Cluster

	if err := m.db.Find(&clusters).Error; err != nil {
		m.log.WithError(err).Errorf("failed to get clusters")
		return
	}
	for _, cluster := range clusters {
		state, err := m.getCurrentState(swag.StringValue(cluster.Status))

		if err != nil {
			m.log.WithError(err).Errorf("failed to get cluster %s currentState", cluster.ID)
			continue
		}
		stateReply, err := state.RefreshStatus(context.Background(), cluster, m.db)
		if err != nil {
			m.log.WithError(err).Errorf("failed to refresh cluster %s state", cluster.ID)
			continue
		}
		if stateReply.IsChanged {
			m.log.Infof("cluster %s updated to state %s via monitor", cluster.ID, stateReply.State)
		}
	}
}

func (m *Manager) DownloadFiles(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInstalling,
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
	allowedStatuses := []string{clusterStatusInstalling, clusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, credentials are available only in installing or installed state", c.ID, clusterStatus)
	}

	return err
}

func (m *Manager) UploadIngressCert(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	if clusterStatus != clusterStatusInstalled {
		err = errors.Errorf("Cluster %s is in %s state, upload ingress ca can be done only in installed state", c.ID, clusterStatus)
	}

	return err
}

func (m *Manager) AcceptRegistration(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInsufficient, clusterStatusReady}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, host can register only in one of %s", c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) VerifyClusterUpdatability(c *common.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInsufficient, clusterStatusReady}
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
	err := m.sm.Run(TransitionTypeCancelInstallation, newStateCluster(c), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) ResetCluster(ctx context.Context, c *common.Cluster, reason string, db *gorm.DB) *common.ApiErrorResponse {
	err := m.sm.Run(TransitionTypeResetCluster, newStateCluster(c), &TransitionArgsResetCluster{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) PrepareForInstallation(ctx context.Context, c *common.Cluster) error {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusReady}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Cluster is in %s state, cluster can be prepared for installation only in one of %s states",
				clusterStatus, allowedStatuses))
	}

	_, err := updateState(clusterStatusPrepareForInstallation, statusInfoPreparingForInstallation, c, m.db,
		logutil.FromContext(ctx, m.log))
	return err
}

func (m *Manager) HandlePreInstallError(ctx context.Context, c *common.Cluster, installErr error) {
	log := logutil.FromContext(ctx, m.log)
	if _, err := updateState(clusterStatusError, installErr.Error(), c, m.db, log); err != nil {
		log.WithError(err).Errorf("failed to set cluster to %s", clusterStatusError)
	}
	log.Infof("Successfully handled pre-installation error, cluster %s changed state to %s",
		c.ID.String(), clusterStatusError)
}
