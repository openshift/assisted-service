package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/connectivity"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/internal/validators"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

//go:generate mockgen -source=host.go -package=host -aux_files=github.com/filanov/bm-inventory/internal/host=instructionmanager.go -destination=mock_host_api.go

type StateAPI interface {
	// Set a new inventory information
	UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error)
	// check keep alive
	RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error)
}

type SpecificHardwareParams interface {
	GetHostValidDisks(h *models.Host) ([]*models.Disk, error)
	ValidateCurrentInventory(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error)
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
	HostStatusResetting            = "resetting"
)

const (
	progressDone   = "Done"
	progressFailed = "Failed"
)

type API interface {
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host) error
	HandleInstallationFailure(ctx context.Context, h *models.Host) error
	StateAPI
	InstructionApi
	SpecificHardwareParams
	UpdateInstallProgress(ctx context.Context, h *models.Host, progress string) error
	SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool, db *gorm.DB) error
	UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error
	HostMonitoring()
	UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error
	UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error
	CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	GetHostname(h *models.Host) string
	// Disable host from getting any requests
	DisableHost(ctx context.Context, h *models.Host) error
	// Enable host to get requests (disabled by default)
	EnableHost(ctx context.Context, h *models.Host) error
	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) error
	// Set a new HW information
	UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) error
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
	resetting      StateAPI
	instructionApi InstructionApi
	hwValidator    hardware.Validator
	eventsHandler  events.Handler
	sm             stateswitch.StateMachine
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, hwValidator hardware.Validator, instructionApi InstructionApi, connectivityValidator connectivity.Validator) *Manager {
	th := &transitionHandler{
		db:  db,
		log: log,
	}
	return &Manager{
		log:            log,
		db:             db,
		discovering:    NewDiscoveringState(log, db, hwValidator, connectivityValidator),
		known:          NewKnownState(log, db, hwValidator, connectivityValidator),
		insufficient:   NewInsufficientState(log, db, hwValidator, connectivityValidator),
		disconnected:   NewDisconnectedState(log, db, hwValidator),
		disabled:       NewDisabledState(log, db),
		installing:     NewInstallingState(log, db),
		installed:      NewInstalledState(log, db),
		error:          NewErrorState(log, db),
		resetting:      NewResettingState(log, db),
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		eventsHandler:  eventsHandler,
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
	case HostStatusResetting:
		return m.resetting, nil
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
		ctx:                   ctx,
		discoveryAgentVersion: h.DiscoveryAgentVersion,
	})
}

func (m *Manager) HandleInstallationFailure(ctx context.Context, h *models.Host) error {

	return m.sm.Run(TransitionTypeHostInstallationFailed, newStateHost(h), &TransitionArgsHostInstallationFailed{
		ctx:    ctx,
		reason: "installation command failed",
	})
}

func (m *Manager) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := []string{models.HostStatusDisconnected, models.HostStatusDiscovering,
		models.HostStatusInsufficient, models.HostStatusKnown}
	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Host %s is in %s state, hardware info can be set only in one of %s states",
				h.ID.String(), hostStatus, allowedStatuses))
	}
	h.HardwareInfo = hwInfo
	return m.db.Model(h).Update("hardware_info", hwInfo).Error
}

func (m *Manager) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateInventory(ctx, h, inventory)
}

func (m *Manager) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	state, err := m.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	ret, err := state.RefreshStatus(ctx, h, db)
	if err == nil && ret.IsChanged {
		msg := fmt.Sprintf("Updated status of host %s to %s", m.GetHostname(h), ret.State)
		m.eventsHandler.AddEvent(ctx, h.ID.String(), msg, time.Now(), h.ClusterID.String())
	}
	return ret, err
}

func (m *Manager) Install(ctx context.Context, h *models.Host, db *gorm.DB) error {
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return m.sm.Run(TransitionTypeInstallHost, newStateHost(h), &TransitionArgsInstallHost{
		ctx: ctx,
		db:  cdb,
	})
}

func (m *Manager) EnableHost(ctx context.Context, h *models.Host) error {
	return m.sm.Run(TransitionTypeEnableHost, newStateHost(h), &TransitionArgsEnableHost{
		ctx: ctx,
	})
}

func (m *Manager) DisableHost(ctx context.Context, h *models.Host) error {
	return m.sm.Run(TransitionTypeDisableHost, newStateHost(h), &TransitionArgsDisableHost{
		ctx: ctx,
	})
}

func (m *Manager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {
	return m.instructionApi.GetNextSteps(ctx, host)
}

func (m *Manager) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	return m.hwValidator.GetHostValidDisks(host)
}

func (m *Manager) ValidateCurrentInventory(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error) {
	return m.hwValidator.IsSufficient(host, cluster)
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

func (m *Manager) SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool, db *gorm.DB) error {
	if h.Bootstrap != isbootstrap {
		err := db.Model(h).Update("bootstrap", isbootstrap).Error
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

func (m *Manager) UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := []string{HostStatusDiscovering, HostStatusKnown, HostStatusDisconnected, HostStatusInsufficient}
	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host role can be set only in one of %s states",
				hostStatus, allowedStatuses))
	}

	h.Role = role
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return cdb.Model(h).Update("role", role).Error
}

func (m *Manager) UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := []string{HostStatusDiscovering, HostStatusKnown, HostStatusDisconnected, HostStatusInsufficient}
	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host name can be set only in one of %s states",
				hostStatus, allowedStatuses))
	}

	h.RequestedHostname = hostname
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return cdb.Model(h).Update("requested_hostname", hostname).Error
}

func (m *Manager) CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	err := m.sm.Run(TransitionTypeCancelInstallation, newStateHost(h), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	err := m.sm.Run(TransitionTypeResetHost, newStateHost(h), &TransitionArgsResetHost{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) GetHostname(host *models.Host) string {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return host.ID.String()
	}
	return inventory.Hostname
}
