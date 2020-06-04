package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/thoas/go-funk"

	"github.com/filanov/bm-inventory/internal/events"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

const minHostsNeededForInstallation = 3

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster_api.go

type StateAPI interface {
	// Refresh state in case of hosts update7
	RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error)
}

type RegistrationAPI interface {
	// Register a new cluster
	RegisterCluster(ctx context.Context, c *models.Cluster) error
	//deregister cluster
	DeregisterCluster(ctx context.Context, c *models.Cluster) error
}

type InstallationAPI interface {
	// Install cluster
	Install(ctx context.Context, c *models.Cluster, db *gorm.DB) error
	// Get the cluster master nodes ID's
	GetMasterNodesIds(ctx context.Context, c *models.Cluster, db *gorm.DB) ([]*strfmt.UUID, error)
}

type API interface {
	StateAPI
	RegistrationAPI
	InstallationAPI
	ClusterMonitoring()
	DownloadFiles(c *models.Cluster) (err error)
	DownloadKubeconfig(c *models.Cluster) (err error)
	GetCredentials(c *models.Cluster) (err error)
	UploadIngressCert(c *models.Cluster) (err error)
	VerifyRegisterHost(c *models.Cluster) (err error)
}

type Manager struct {
	log             logrus.FieldLogger
	db              *gorm.DB
	insufficient    StateAPI
	ready           StateAPI
	installing      StateAPI
	installed       StateAPI
	error           StateAPI
	registrationAPI RegistrationAPI
	installationAPI InstallationAPI
	eventsHandler   events.Handler
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler) *Manager {
	return &Manager{
		log:             log,
		db:              db,
		insufficient:    NewInsufficientState(log, db),
		ready:           NewReadyState(log, db),
		installing:      NewInstallingState(log, db),
		installed:       NewInstalledState(log, db),
		error:           NewErrorState(log, db),
		registrationAPI: NewRegistrar(log, db),
		installationAPI: NewInstaller(log, db),
		eventsHandler:   eventsHandler,
	}
}

func (m *Manager) getCurrentState(status string) (StateAPI, error) {
	switch status {
	case "":
	case clusterStatusInsufficient:
		return m.insufficient, nil
	case clusterStatusReady:
		return m.ready, nil
	case clusterStatusInstalling:
		return m.installing, nil
	case clusterStatusInstalled:
		return m.installed, nil
	case clusterStatusError:
		return m.error, nil
	}
	return nil, fmt.Errorf("not supported cluster status: %s", status)
}

func (m *Manager) RegisterCluster(ctx context.Context, c *models.Cluster) error {
	err := m.registrationAPI.RegisterCluster(ctx, c)
	var msg string
	if err != nil {
		msg = fmt.Sprintf("Registration of cluster %s failed. Error: %s", c.ID, err.Error())
	} else {
		msg = fmt.Sprintf("Registered cluster %s", c.ID)
	}
	m.eventsHandler.AddEvent(ctx, c.ID.String(), msg, time.Now())
	return err
}

func (m *Manager) DeregisterCluster(ctx context.Context, c *models.Cluster) error {
	err := m.registrationAPI.DeregisterCluster(ctx, c)
	var msg string
	if err != nil {
		msg = fmt.Sprintf("Deregistration of cluster %s failed. Error: %s", c.ID, err.Error())
	} else {
		msg = fmt.Sprintf("Deregistered cluster %s", c.ID)
	}
	m.eventsHandler.AddEvent(ctx, c.ID.String(), msg, time.Now())
	return err
}

func (m *Manager) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(c.Status))
	if err != nil {
		return nil, err
	}
	return state.RefreshStatus(ctx, c, db)
}

func (m *Manager) Install(ctx context.Context, c *models.Cluster, db *gorm.DB) error {
	return m.installationAPI.Install(ctx, c, db)
}

func (m *Manager) GetMasterNodesIds(ctx context.Context, c *models.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return m.installationAPI.GetMasterNodesIds(ctx, c, db)
}

func (m *Manager) ClusterMonitoring() {
	var clusters []*models.Cluster

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

func (m *Manager) DownloadFiles(c *models.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInstalling,
		clusterStatusInstalled,
		clusterStatusError}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = fmt.Errorf("cluster %s is in %s state, files can be downloaded only when status is one of: %s",
			c.ID, clusterStatus, allowedStatuses)
	}
	return err
}

func (m *Manager) DownloadKubeconfig(c *models.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	if clusterStatus != clusterStatusInstalled {
		err = fmt.Errorf("cluster %s is in %s state, %s can be downloaded only in installed state", c.ID, clusterStatus, "kubeconfig")
	}

	return err
}
func (m *Manager) GetCredentials(c *models.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInstalling, clusterStatusInstalled}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = fmt.Errorf("Cluster %s is in %s state, credentials are available only in installing or installed state", c.ID, clusterStatus)
	}

	return err
}

func (m *Manager) UploadIngressCert(c *models.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	if clusterStatus != clusterStatusInstalled {
		err = fmt.Errorf("Cluster %s is in %s state, upload ingress ca can be done only in installed state", c.ID, clusterStatus)
	}

	return err
}

func (m *Manager) VerifyRegisterHost(c *models.Cluster) (err error) {
	clusterStatus := swag.StringValue(c.Status)
	allowedStatuses := []string{clusterStatusInsufficient, clusterStatusReady}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		err = errors.Errorf("Cluster %s is in %s state, host can register only in one of %s", c.ID, clusterStatus, allowedStatuses)
	}
	return err
}
