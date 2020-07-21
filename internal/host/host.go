package host

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const (
	HostStatusDiscovering                 = "discovering"
	HostStatusKnown                       = "known"
	HostStatusDisconnected                = "disconnected"
	HostStatusInsufficient                = "insufficient"
	HostStatusDisabled                    = "disabled"
	HostStatusInstalling                  = "installing"
	HostStatusInstallingInProgress        = "installing-in-progress"
	HostStatusInstallingPendingUserAction = "installing-pending-user-action"
	HostStatusInstalled                   = "installed"
	HostStatusError                       = "error"
	HostStatusResetting                   = "resetting"
	HostStatusPendingForInput             = "pending-for-input"
)

var BootstrapStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageStartWaitingForControlPlane,
	models.HostStageRebooting, models.HostStageConfiguring, models.HostStageDone,
}
var MasterStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageRebooting,
	models.HostStageConfiguring, models.HostStageJoined, models.HostStageDone,
}
var WorkerStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageRebooting,
	models.HostStageWaitingForIgnition, models.HostStageConfiguring, models.HostStageDone,
}

var manualRebootStages = [...]models.HostStage{
	models.HostStageRebooting,
	models.HostStageWaitingForIgnition,
	models.HostStageConfiguring,
	models.HostStageJoined,
	models.HostStageDone,
}

//go:generate mockgen -source=host.go -package=host -aux_files=github.com/filanov/bm-inventory/internal/host=instructionmanager.go -destination=mock_host_api.go
type API interface {
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host) error
	HandleInstallationFailure(ctx context.Context, h *models.Host) error
	InstructionApi
	UpdateInstallProgress(ctx context.Context, h *models.Host, progress *models.HostProgress) error
	RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) error
	SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool, db *gorm.DB) error
	UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error
	HostMonitoring()
	UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error
	UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error
	CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	IsRequireUserActionReset(h *models.Host) bool
	ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetPendingUserAction(ctx context.Context, h *models.Host, db *gorm.DB) error
	GetHostname(h *models.Host) string
	// Disable host from getting any requests
	DisableHost(ctx context.Context, h *models.Host) error
	// Enable host to get requests (disabled by default)
	EnableHost(ctx context.Context, h *models.Host) error
	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) error
	// Set a new HW information
	UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) error
	// Set a new inventory information
	UpdateInventory(ctx context.Context, h *models.Host, inventory string) error
	GetStagesByRole(role models.HostRole, isbootstrap bool) []models.HostStage
	IsInstallable(h *models.Host) bool
	PrepareForInstallation(ctx context.Context, h *models.Host, db *gorm.DB) error
}

type Manager struct {
	log            logrus.FieldLogger
	db             *gorm.DB
	instructionApi InstructionApi
	hwValidator    hardware.Validator
	eventsHandler  events.Handler
	sm             stateswitch.StateMachine
	rp             *refreshPreprocessor
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, hwValidator hardware.Validator, instructionApi InstructionApi, hwValidatorCfg *hardware.ValidatorCfg) *Manager {
	th := &transitionHandler{
		db:  db,
		log: log,
	}
	return &Manager{
		log:            log,
		db:             db,
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		eventsHandler:  eventsHandler,
		sm:             NewHostStateMachine(th),
		rp:             newRefreshPreprocessor(log, hwValidatorCfg),
	}
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

func (m *Manager) UpdateInventory(ctx context.Context, h *models.Host, inventory string) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := []string{models.HostStatusDiscovering, models.HostStatusKnown, models.HostStatusDisconnected,
		models.HostStatusInsufficient}
	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Host is in %s state, host can be updated only in one of %s states",
				hostStatus, allowedStatuses))
	}
	h.Inventory = inventory
	return m.db.Model(h).Update("inventory", inventory).Error
}

func (m *Manager) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	vc, err := newValidationContext(h, db)
	if err != nil {
		return err
	}
	conditions, validationsResults, err := m.rp.preprocess(vc)
	if err != nil {
		return err
	}
	err = m.sm.Run(TransitionTypeRefresh, newStateHost(h), &TransitionArgsRefreshHost{
		ctx:               ctx,
		db:                db,
		eventHandler:      m.eventsHandler,
		conditions:        conditions,
		validationResults: validationsResults,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
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

func (m *Manager) UpdateInstallProgress(ctx context.Context, h *models.Host, progress *models.HostProgress) error {
	validStatuses := []string{HostStatusInstalling, HostStatusInstallingInProgress, HostStatusInstallingPendingUserAction}
	if !funk.ContainsString(validStatuses, swag.StringValue(h.Status)) {
		return fmt.Errorf("can't set progress to host in status <%s>", swag.StringValue(h.Status))
	}

	statusInfo := string(progress.CurrentStage)

	switch progress.CurrentStage {
	case models.HostStageDone:
		_, err := updateHostProgress(logutil.FromContext(ctx, m.log), m.db, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), HostStatusInstalled, statusInfo,
			h.Progress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
		return err
	case models.HostStageFailed:
		// Keeps the last progress

		if progress.ProgressInfo != "" {
			statusInfo += fmt.Sprintf(" - %s", progress.ProgressInfo)
		}

		_, err := updateHostStatus(logutil.FromContext(ctx, m.log), m.db, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), HostStatusError, statusInfo)
		return err
	default:
		_, err := updateHostProgress(logutil.FromContext(ctx, m.log), m.db, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), HostStatusInstallingInProgress, statusInfo,
			h.Progress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
		return err
	}
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
	allowedStatuses := []string{HostStatusDiscovering, HostStatusKnown, HostStatusDisconnected, HostStatusInsufficient, HostStatusPendingForInput}
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
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("Installation canceled for host %s", m.GetHostname(h))
	defer func() {
		m.eventsHandler.AddEvent(ctx, h.ID.String(), eventSeverity, eventInfo, time.Now(), h.ClusterID.String())
	}()

	err := m.sm.Run(TransitionTypeCancelInstallation, newStateHost(h), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to cancel installation of host %s. Error: %s", m.GetHostname(h), err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) IsRequireUserActionReset(h *models.Host) bool {
	if swag.StringValue(h.Status) != models.HostStatusResetting {
		return false
	}
	if !funk.Contains(manualRebootStages, h.Progress.CurrentStage) {
		return false
	}
	return true
}

func (m *Manager) ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("Installation reset for host %s", m.GetHostname(h))
	defer func() {
		m.eventsHandler.AddEvent(ctx, h.ID.String(), eventSeverity, eventInfo, time.Now(), h.ClusterID.String())
	}()

	err := m.sm.Run(TransitionTypeResetHost, newStateHost(h), &TransitionArgsResetHost{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to reset installation of host %s. Error: %s", m.GetHostname(h), err.Error())
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) ResetPendingUserAction(ctx context.Context, h *models.Host, db *gorm.DB) error {
	err := m.sm.Run(TransitionTypeResettingPendingUserAction, newStateHost(h), &TransitionResettingPendingUserAction{
		ctx: ctx,
		db:  db,
	})
	if err != nil {
		return err
	}
	return nil
}

func (m *Manager) GetHostname(host *models.Host) string {
	hostName, err := common.GetCurrentHostName(host)
	if err != nil {
		return host.ID.String()
	}
	return hostName
}

func (m *Manager) GetStagesByRole(role models.HostRole, isbootstrap bool) []models.HostStage {
	if isbootstrap || role == models.HostRoleBootstrap {
		return BootstrapStages[:]
	}

	switch role {
	case models.HostRoleMaster:
		return MasterStages[:]
	case models.HostRoleWorker:
		return WorkerStages[:]
	default:
		return []models.HostStage{}
	}
}

func (m *Manager) IsInstallable(h *models.Host) bool {
	return swag.StringValue(h.Status) == models.HostStatusKnown
}

func (m *Manager) PrepareForInstallation(ctx context.Context, h *models.Host, db *gorm.DB) error {
	return m.sm.Run(TransitionTypePrepareForInstallation, newStateHost(h), &TransitionArgsPrepareForInstallation{
		ctx: ctx,
		db:  db,
	})
}
