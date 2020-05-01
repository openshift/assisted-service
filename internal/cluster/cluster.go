package cluster

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster_api.go

type StateAPI interface {
	// Refresh state in case of hosts update7
	RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error)
	//deregister cluster
	DeregisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error)
	// Install cluster
	Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error)
}

type RegistrationAPI interface {
	// Register a new cluster
	RegisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error)
}

type API interface {
	StateAPI
	RegistrationAPI
}

type Manager struct {
	insufficient    StateAPI
	ready           StateAPI
	installing      StateAPI
	installed       StateAPI
	error           StateAPI
	registrationAPI RegistrationAPI
}

func NewManager(log logrus.FieldLogger, db *gorm.DB) *Manager {
	return &Manager{
		insufficient:    NewInsufficientState(log, db),
		ready:           NewReadyState(log, db),
		installing:      NewInstallingState(log, db),
		installed:       NewInstalledState(log, db),
		error:           NewErrorState(log, db),
		registrationAPI: NewRegistrar(log, db),
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

func (m *Manager) RegisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return m.registrationAPI.RegisterCluster(ctx, c)
}

func (m *Manager) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(c.Status))
	if err != nil {
		return nil, err
	}
	return state.RefreshStatus(ctx, c, db)
}

func (m *Manager) Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(c.Status))
	if err != nil {
		return nil, err
	}
	return state.Install(ctx, c)
}

func (m *Manager) DeregisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(c.Status))
	if err != nil {
		return nil, err
	}
	return state.DeregisterCluster(ctx, c)
}
