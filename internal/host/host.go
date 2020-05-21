package host

import (
	"context"
	"fmt"
	"strings"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=host.go -package=host -aux_files=github.com/filanov/bm-inventory/internal/host=instructionmanager.go -destination=mock_host_api.go

type StateAPI interface {
	// Set a new HW information
	UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error)
	// Set a new inventory information
	UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error)
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

type SpecificHardwareParams interface {
	GetHostValidDisks(h *models.Host) ([]*models.Disk, error)
}

const (
	HostStatusDiscovering          = "discovering"
	HostStatusKnown                = "known"
	HostStatusDisconnected         = "disconnected"
	HostStatusInsufficient         = "insufficient"
	HostStatusDisabled             = "disabled"
	HostStatusInstalling           = "installing"
	HostStatusInstallingInProgress = "installing-in-progress"
	HostStatusInstalled            = "installed"
	HostStatusError                = "error"
)

const (
	RoleMaster    = "master"
	RoleBootstrap = "bootstrap"
	RoleWorker    = "worker"
)

const (
	progressDone   = "Done"
	progressFailed = "Failed"
)

type API interface {
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host) error
	StateAPI
	InstructionApi
	SpecificHardwareParams
	UpdateInstallProgress(ctx context.Context, h *models.Host, progress string) error
	SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool) error
	UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error
}

type Manager struct {
	log            logrus.FieldLogger
	db             *gorm.DB
	discovering    StateAPI
	known          StateAPI
	insufficient   StateAPI
	disconnected   StateAPI
	disabled       StateAPI
	installing     StateAPI
	installed      StateAPI
	error          StateAPI
	instructionApi InstructionApi
	hwValidator    hardware.Validator
	sm             stateswitch.StateMachine
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, instructionApi InstructionApi) *Manager {
	th := &transitionHandler{
		db:  db,
		log: log,
	}
	return &Manager{
		log:            log,
		db:             db,
		discovering:    NewDiscoveringState(log, db, hwValidator),
		known:          NewKnownState(log, db, hwValidator),
		insufficient:   NewInsufficientState(log, db, hwValidator),
		disconnected:   NewDisconnectedState(log, db, hwValidator),
		disabled:       NewDisabledState(log, db),
		installing:     NewInstallingState(log, db),
		installed:      NewInstalledState(log, db),
		error:          NewErrorState(log, db),
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		sm:             NewHostStateMachine(th),
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

func (m *Manager) RegisterHost(ctx context.Context, h *models.Host) error {
	var host models.Host
	err := m.db.First(&host, "id = ? and cluster_id = ?", *h.ID, h.ClusterID).Error
	if err != nil && !gorm.IsRecordNotFoundError(err) {
		return err
	}

	pHost := &host
	if err != nil && gorm.IsRecordNotFoundError(err) {
		pHost = h
	}

	return m.sm.Run(TransitionTypeRegisterHost, newStateHost(pHost), &TransitionArgsRegisterHost{
		ctx: ctx,
	})
}

func (m *Manager) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateHwInfo(ctx, h, hwInfo)
}

func (m *Manager) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateInventory(ctx, h, inventory)
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

func (m *Manager) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	return m.hwValidator.GetHostValidDisks(host)
}

func (m *Manager) UpdateInstallProgress(ctx context.Context, h *models.Host, progress string) error {
	if swag.StringValue(h.Status) != HostStatusInstalling && swag.StringValue(h.Status) != HostStatusInstallingInProgress {
		return fmt.Errorf("can't set progress to host in status <%s>", swag.StringValue(h.Status))
	}

	// installation done
	if progress == progressDone {
		_, err := updateStateWithParams(logutil.FromContext(ctx, m.log),
			HostStatusInstalled, HostStatusInstalled, h, m.db)
		return err
	}

	// installation failed
	if strings.HasPrefix(progress, progressFailed) {
		_, err := updateStateWithParams(logutil.FromContext(ctx, m.log),
			HostStatusError, progress, h, m.db)
		return err
	}

	_, err := updateStateWithParams(logutil.FromContext(ctx, m.log),
		HostStatusInstallingInProgress, progress, h, m.db)
	return err
}

func (m *Manager) SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool) error {
	if h.Bootstrap != isbootstrap {
		err := m.db.Model(h).Update("bootstrap", isbootstrap).Error
		if err != nil {
			return errors.Wrapf(err, "failed to set bootstrap to host %s", h.ID.String())
		}
	}
	return nil
}

func (m *Manager) UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error {
	if h.Connectivity != connectivityReport {
		err := m.db.Model(h).Update("connectivity", connectivityReport).Error
		if err != nil {
			return errors.Wrapf(err, "failed to set connectivity to host %s", h.ID.String())
		}
	}
	return nil

}
