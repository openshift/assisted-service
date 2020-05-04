package host

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=host.go -package=host -aux_files=github.com/filanov/bm-inventory/internal/host=instructionmanager.go -destination=mock_host_api.go
type StateAPI interface {
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Set a new HW information
	UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error)
	// Set host state
	UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error)
	// check keep alive
	RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error)
	// Enable host to get requests (disabled by default)
	EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Disable host from getting any requests
	DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
}

const (
	HostStatusDiscovering  = "discovering"
	HostStatusKnown        = "known"
	HostStatusDisconnected = "disconnected"
	HostStatusInsufficient = "insufficient"
	HostStatusDisabled     = "disabled"
	HostStatusInstalling   = "installing"
	HostStatusInstalled    = "installed"
	HostStatusError        = "error"
)

type API interface {
	StateAPI
	InstructionApi
}

type Manager struct {
	discovering    StateAPI
	known          StateAPI
	insufficient   StateAPI
	disconnected   StateAPI
	disabled       StateAPI
	installing     StateAPI
	installed      StateAPI
	error          StateAPI
	instructionApi InstructionApi
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *Manager {
	return &Manager{
		discovering:    NewDiscoveringState(log, db, hwValidator),
		known:          NewKnownState(log, db, hwValidator),
		insufficient:   NewInsufficientState(log, db, hwValidator),
		disconnected:   NewDisconnectedState(log, db, hwValidator),
		disabled:       NewDisabledState(log, db),
		installing:     NewInstallingState(log, db),
		installed:      NewInstalledState(log, db),
		error:          NewErrorState(log, db),
		instructionApi: NewInstructionManager(log, db),
	}
}

func (m *Manager) getCurrentState(status string) (StateAPI, error) {
	switch status {
	case "":
	case HostStatusDiscovering:
		return m.discovering, nil
	case HostStatusKnown:
		return m.known, nil
	case HostStatusInsufficient:
		return m.insufficient, nil
	case HostStatusDisconnected:
		return m.disconnected, nil
	case HostStatusDisabled:
		return m.disabled, nil
	case HostStatusInstalling:
		return m.installing, nil
	case HostStatusInstalled:
		return m.installed, nil
	case HostStatusError:
		return m.error, nil
	}
	return nil, fmt.Errorf("not supported host status: %s", status)
}

func (m *Manager) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.RegisterHost(ctx, h)
}

func (m *Manager) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateHwInfo(ctx, h, hwInfo)
}

func (m *Manager) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateRole(ctx, h, role, db)
}

func (m *Manager) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.RefreshStatus(ctx, h)
}

func (m *Manager) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.Install(ctx, h, db)
}

func (m *Manager) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.EnableHost(ctx, h)
}

func (m *Manager) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.DisableHost(ctx, h)
}

func (m *Manager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {
	return m.instructionApi.GetNextSteps(ctx, host)
}
