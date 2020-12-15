package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/hostutil"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
)

var BootstrapStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageWaitingForControlPlane,
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

var InstallationProgressTimeout = map[models.HostStage]time.Duration{
	models.HostStageStartingInstallation:        30 * time.Minute,
	models.HostStageWaitingForControlPlane:      60 * time.Minute,
	models.HostStageStartWaitingForControlPlane: 60 * time.Minute,
	models.HostStageInstalling:                  60 * time.Minute,
	models.HostStageJoined:                      60 * time.Minute,
	models.HostStageWritingImageToDisk:          30 * time.Minute,
	models.HostStageRebooting:                   70 * time.Minute,
	models.HostStageConfiguring:                 60 * time.Minute,
	models.HostStageWaitingForIgnition:          60 * time.Minute,
	"DEFAULT":                                   60 * time.Minute,
}

var disconnectionValidationStages = []models.HostStage{
	models.HostStageWritingImageToDisk,
	models.HostStageInstalling,
}

var WrongBootOrderIgnoreTimeoutStages = []models.HostStage{
	models.HostStageStartWaitingForControlPlane,
	models.HostStageWaitingForControlPlane,
	models.HostStageRebooting,
}

var InstallationTimeout = 20 * time.Minute

var MaxHostDisconnectionTime = 3 * time.Minute

type Config struct {
	EnableAutoReset  bool          `envconfig:"ENABLE_AUTO_RESET" default:"false"`
	ResetTimeout     time.Duration `envconfig:"RESET_CLUSTER_TIMEOUT" default:"3m"`
	MonitorBatchSize int           `envconfig:"HOST_MONITOR_BATCH_SIZE" default:"100"`
}

//go:generate mockgen -source=host.go -package=host -aux_files=github.com/openshift/assisted-service/internal/host=instructionmanager.go -destination=mock_host_api.go
type API interface {
	InstructionApi
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host, db *gorm.DB) error
	RegisterInstalledOCPHost(ctx context.Context, h *models.Host, db *gorm.DB) error
	HandleInstallationFailure(ctx context.Context, h *models.Host) error
	UpdateInstallProgress(ctx context.Context, h *models.Host, progress *models.HostProgress) error
	RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) error
	SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool, db *gorm.DB) error
	UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error
	UpdateApiVipConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error
	HostMonitoring()
	CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	IsRequireUserActionReset(h *models.Host) bool
	ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse
	ResetPendingUserAction(ctx context.Context, h *models.Host, db *gorm.DB) error
	// Disable host from getting any requests
	DisableHost(ctx context.Context, h *models.Host, db *gorm.DB) error
	// Enable host to get requests (disabled by default)
	EnableHost(ctx context.Context, h *models.Host, db *gorm.DB) error
	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) error
	GetStagesByRole(role models.HostRole, isbootstrap bool) []models.HostStage
	IsInstallable(h *models.Host) bool
	PrepareForInstallation(ctx context.Context, h *models.Host, db *gorm.DB) error
	// auto assign host role
	AutoAssignRole(ctx context.Context, h *models.Host, db *gorm.DB) error
	IsValidMasterCandidate(h *models.Host, db *gorm.DB, log logrus.FieldLogger) (bool, error)
	SetUploadLogsAt(ctx context.Context, h *models.Host, db *gorm.DB) error
	GetHostRequirements(role models.HostRole) models.HostRequirementsRole
	PermanentHostsDeletion(olderThen strfmt.DateTime) error

	UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error
	UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error
	UpdateInventory(ctx context.Context, h *models.Host, inventory string) error
	UpdateNTP(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource, db *gorm.DB) error
	UpdateMachineConfigPoolName(ctx context.Context, db *gorm.DB, h *models.Host, machineConfigPoolName string) error
}

type Manager struct {
	log            logrus.FieldLogger
	db             *gorm.DB
	instructionApi InstructionApi
	hwValidator    hardware.Validator
	eventsHandler  events.Handler
	sm             stateswitch.StateMachine
	rp             *refreshPreprocessor
	metricApi      metrics.API
	Config         Config
	leaderElector  leader.Leader
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, hwValidator hardware.Validator, instructionApi InstructionApi,
	hwValidatorCfg *hardware.ValidatorCfg, metricApi metrics.API, config *Config, leaderElector leader.ElectorInterface) *Manager {
	th := &transitionHandler{
		db:            db,
		log:           log,
		eventsHandler: eventsHandler,
	}
	return &Manager{
		log:            log,
		db:             db,
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		eventsHandler:  eventsHandler,
		sm:             NewHostStateMachine(th),
		rp:             newRefreshPreprocessor(log, hwValidatorCfg),
		metricApi:      metricApi,
		Config:         *config,
		leaderElector:  leaderElector,
	}
}

func (m *Manager) RegisterHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	var host models.Host
	err := db.First(&host, "id = ? and cluster_id = ?", *h.ID, h.ClusterID).Error
	if err != nil && !gorm.IsRecordNotFoundError(err) {
		return err
	}

	pHost := &host
	if err != nil && gorm.IsRecordNotFoundError(err) {
		// Delete any previews record of the host if it was soft deleted from the cluster,
		// no error will be returned if the host was not existed.
		if err := db.Unscoped().Delete(&host, "id = ? and cluster_id = ?", *h.ID, h.ClusterID).Error; err != nil {
			return errors.Wrapf(
				err,
				"error while trying to delete previews record from db (if exists) of host %s in cluster %s",
				host.ID.String(), host.ClusterID.String())
		}
		pHost = h
	}

	return m.sm.Run(TransitionTypeRegisterHost, newStateHost(pHost), &TransitionArgsRegisterHost{
		ctx:                   ctx,
		discoveryAgentVersion: h.DiscoveryAgentVersion,
		db:                    db,
	})
}

func (m *Manager) RegisterInstalledOCPHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	return m.sm.Run(TransitionTypeRegisterInstalledHost, newStateHost(h), &TransitionArgsRegisterInstalledHost{
		ctx: ctx,
		db:  db,
	})
}

func (m *Manager) HandleInstallationFailure(ctx context.Context, h *models.Host) error {

	lastStatusUpdateTime := h.StatusUpdatedAt
	err := m.sm.Run(TransitionTypeHostInstallationFailed, newStateHost(h), &TransitionArgsHostInstallationFailed{
		ctx:    ctx,
		reason: "installation command failed",
	})
	if err == nil {
		m.reportInstallationMetrics(ctx, h, &models.HostProgressInfo{CurrentStage: "installation command failed",
			StageStartedAt: lastStatusUpdateTime}, models.HostStageFailed)
	}
	return err
}

// populateDisksEligibility updates an inventory json string by updating the eligibility
// struct of each disk in the inventory with service-side checks for disk eligibility, in
// addition to agent-side checks that have already been performed. The reason that some
// checks are performed by the agent (and not the service) is because the agent has data
// that is not available in the service.
func (m *Manager) populateDisksEligibility(inventoryString string) (string, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(inventoryString), &inventory); err != nil {
		return "", err
	}

	for _, disk := range inventory.Disks {
		if disk.InstallationEligibility == nil {
			// This is a sign that the agent is using an old version that does not fill in the eligibility struct,
			// for backwards compatibility, pretend that the agent has decided that this disk is eligible
			disk.InstallationEligibility = &models.DiskInstallationEligibility{Eligible: true,
				NotEligibleReasons: make([]string, 0)}
		} else if disk.InstallationEligibility.Eligible && len(disk.InstallationEligibility.NotEligibleReasons) != 0 {
			// The agent decided that it's eligible. Clean up any reasons that might somehow have ended up here
			m.log.Warnf("Agent reported that disk is eligible but the reasons array was not empty: %v",
				disk.InstallationEligibility.NotEligibleReasons)
			disk.InstallationEligibility.NotEligibleReasons = make([]string, 0)
		}

		eligible, newNotEligibleReasons := m.hwValidator.DiskIsEligible(disk)

		// Append to the existing reasons already filled in by the agent
		disk.InstallationEligibility.NotEligibleReasons = append(disk.InstallationEligibility.NotEligibleReasons, newNotEligibleReasons...)

		// Combine the final decision of both the agent and the service
		disk.InstallationEligibility.Eligible = disk.InstallationEligibility.Eligible && eligible
	}

	result, err := json.Marshal(inventory)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func (m *Manager) UpdateInventory(ctx context.Context, h *models.Host, inventory string) error {
	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := append(hostStatusesBeforeInstallation[:], models.HostStatusInstallingInProgress)

	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Host is in %s state, host can be updated only in one of %s states",
				hostStatus, allowedStatuses))
	}

	var err error
	if h.Inventory, err = m.populateDisksEligibility(inventory); err != nil {
		return err
	}

	return m.db.Model(h).Update("inventory", h.Inventory).Error
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

func (m *Manager) EnableHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	return m.sm.Run(TransitionTypeEnableHost, newStateHost(h), &TransitionArgsEnableHost{
		ctx: ctx,
		db:  db,
	})
}

func (m *Manager) DisableHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	return m.sm.Run(TransitionTypeDisableHost, newStateHost(h), &TransitionArgsDisableHost{
		ctx: ctx,
		db:  db,
	})
}

func (m *Manager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {
	return m.instructionApi.GetNextSteps(ctx, host)
}

func (m *Manager) UpdateInstallProgress(ctx context.Context, h *models.Host, progress *models.HostProgress) error {
	previousProgress := h.Progress

	if previousProgress != nil &&
		previousProgress.CurrentStage == progress.CurrentStage &&
		previousProgress.ProgressInfo == progress.ProgressInfo {
		return nil
	}

	validStatuses := []string{
		models.HostStatusInstalling, models.HostStatusInstallingInProgress, models.HostStatusInstallingPendingUserAction,
	}
	if !funk.ContainsString(validStatuses, swag.StringValue(h.Status)) {
		return errors.Errorf("Can't set progress <%s> to host in status <%s>", progress.CurrentStage, swag.StringValue(h.Status))
	}

	if previousProgress.CurrentStage != "" && progress.CurrentStage != models.HostStageFailed {
		// Verify the new stage is higher or equal to the current host stage according to its role stages array
		stages := m.GetStagesByRole(h.Role, h.Bootstrap)
		currentIndex := indexOfStage(progress.CurrentStage, stages)

		if currentIndex == -1 {
			return errors.Errorf("Stages %s isn't available for host role %s bootstrap %s",
				progress.CurrentStage, h.Role, strconv.FormatBool(h.Bootstrap))
		}
		if currentIndex < indexOfStage(previousProgress.CurrentStage, stages) {
			return errors.Errorf("Can't assign lower stage \"%s\" after host has been in stage \"%s\"",
				progress.CurrentStage, previousProgress.CurrentStage)
		}
	}

	statusInfo := string(progress.CurrentStage)

	var err error
	switch progress.CurrentStage {
	case models.HostStageDone:
		_, err = updateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusInstalled, statusInfo,
			previousProgress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
	case models.HostStageFailed:
		// Keeps the last progress

		if progress.ProgressInfo != "" {
			statusInfo += fmt.Sprintf(" - %s", progress.ProgressInfo)
		}

		_, err = updateHostStatus(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusError, statusInfo)
	case models.HostStageRebooting:
		if swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost {
			_, err = updateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
				swag.StringValue(h.Status), models.HostStatusAddedToExistingCluster, statusInfo,
				h.Progress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
			break
		}
		fallthrough
	default:
		_, err = updateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusInstallingInProgress, statusInfo,
			previousProgress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
	}
	m.reportInstallationMetrics(ctx, h, previousProgress, progress.CurrentStage)
	return err
}

func (m *Manager) SetBootstrap(ctx context.Context, h *models.Host, isbootstrap bool, db *gorm.DB) error {
	if h.Bootstrap != isbootstrap {
		err := db.Model(h).Update("bootstrap", isbootstrap).Error
		if err != nil {
			return errors.Wrapf(err, "failed to set bootstrap to host %s", h.ID.String())
		}
		m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityInfo,
			fmt.Sprintf("Host %s: set as bootstrap", hostutil.GetHostnameForMsg(h)), time.Now())
	}
	return nil
}

func (m *Manager) SetUploadLogsAt(ctx context.Context, h *models.Host, db *gorm.DB) error {
	err := db.Model(h).Update("logs_collected_at", strfmt.DateTime(time.Now())).Error
	if err != nil {
		return errors.Wrapf(err, "failed to set logs_collected_at to host %s", h.ID.String())
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

func (m *Manager) UpdateApiVipConnectivityReport(ctx context.Context, h *models.Host, apiVipConnectivityReport string) error {
	if h.APIVipConnectivity != apiVipConnectivityReport {
		if err := m.db.Model(h).Update("api_vip_connectivity", apiVipConnectivityReport).Error; err != nil {
			return errors.Wrapf(err, "failed to set api_vip_connectivity to host %s", h.ID.String())
		}
	}
	return nil
}

func (m *Manager) UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error {
	cdb := m.db
	if db != nil {
		cdb = db
	}

	if h.Role == "" {
		return updateRole(m.log, h, role, cdb, nil)
	} else {
		return updateRole(m.log, h, role, cdb, swag.String(string(h.Role)))
	}
}

func (m *Manager) UpdateMachineConfigPoolName(ctx context.Context, db *gorm.DB, h *models.Host, machineConfigPoolName string) error {
	if !IsDay2Host(h) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host %s must be in day2 to update its machine config pool name to %s",
				h.ID.String(), machineConfigPoolName))
	}

	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallation[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host machine config poll can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	cdb := m.db
	if db != nil {
		cdb = db
	}

	return cdb.Model(h).Update("machine_config_pool_name", machineConfigPoolName).Error
}

func (m *Manager) UpdateNTP(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource, db *gorm.DB) error {
	bytes, err := json.Marshal(ntpSources)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal NTP sources for host %s", h.ID.String())
	}

	return db.Model(h).Update("ntp_sources", string(bytes)).Error
}

func (m *Manager) UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallation[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host name can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
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
	eventInfo := fmt.Sprintf("Installation canceled for host %s", hostutil.GetHostnameForMsg(h))
	shouldAddEvent := true
	defer func() {
		if shouldAddEvent {
			m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, eventSeverity, eventInfo, time.Now())
		}
	}()

	err := m.sm.Run(TransitionTypeCancelInstallation, newStateHost(h), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to cancel installation of host %s: %s", hostutil.GetHostnameForMsg(h), err.Error())
		return common.NewApiError(http.StatusConflict, err)
	} else if swag.StringValue(h.Status) == models.HostStatusDisabled {
		shouldAddEvent = false
	}
	return nil
}

func (m *Manager) IsRequireUserActionReset(h *models.Host) bool {
	if swag.StringValue(h.Status) != models.HostStatusResetting {
		return false
	}
	if time.Since(time.Time(h.StatusUpdatedAt)) >= m.Config.ResetTimeout {
		m.log.Infof("Cluster: %s Host %s is hanged in resetting status. Agent seems to be stuck. "+
			"Exceeded reset timeout: %s", h.ClusterID.String(), h.ID.String(), m.Config.ResetTimeout.String())
		return true
	}
	if funk.Contains(manualRebootStages, h.Progress.CurrentStage) {
		m.log.Infof("Cluster %s Host %s is in stage %s and must be restarted by user to the live image "+
			"in order to reset the installation.", h.ClusterID.String(), h.ID.String(), h.Progress.CurrentStage)
		return true
	}
	return false
}

func (m *Manager) ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("Installation reset for host %s", hostutil.GetHostnameForMsg(h))
	shouldAddEvent := true
	defer func() {
		if shouldAddEvent {
			m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, eventSeverity, eventInfo, time.Now())
		}
	}()

	var transitionType stateswitch.TransitionType
	var transitionArgs stateswitch.TransitionArgs

	if m.Config.EnableAutoReset {
		transitionType = TransitionTypeResetHost
		transitionArgs = &TransitionArgsResetHost{
			ctx:    ctx,
			reason: reason,
			db:     db,
		}
	} else {
		transitionType = TransitionTypeResettingPendingUserAction
		transitionArgs = &TransitionResettingPendingUserAction{
			ctx: ctx,
			db:  db,
		}
	}

	if err := m.sm.Run(transitionType, newStateHost(h), transitionArgs); err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to reset installation of host %s. Error: %s", hostutil.GetHostnameForMsg(h), err.Error())
		return common.NewApiError(http.StatusConflict, err)
	} else if swag.StringValue(h.Status) == models.HostStatusDisabled {
		shouldAddEvent = false
	}
	return nil
}

func (m *Manager) ResetPendingUserAction(ctx context.Context, h *models.Host, db *gorm.DB) error {
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("User action is required in order to complete installation reset for host %s", hostutil.GetHostnameForMsg(h))
	shouldAddEvent := true
	defer func() {
		if shouldAddEvent {
			m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, eventSeverity, eventInfo, time.Now())
		}
	}()

	err := m.sm.Run(TransitionTypeResettingPendingUserAction, newStateHost(h), &TransitionResettingPendingUserAction{
		ctx: ctx,
		db:  db,
	})
	if err != nil {
		eventSeverity = models.EventSeverityError
		eventInfo = fmt.Sprintf("Failed to set status of host %s to reset-pending-user-action. Error: %s", hostutil.GetHostnameForMsg(h), err.Error())
		return err
	} else if swag.StringValue(h.Status) == models.HostStatusDisabled {
		shouldAddEvent = false
	}
	return nil
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

func (m *Manager) reportInstallationMetrics(ctx context.Context, h *models.Host, previousProgress *models.HostProgressInfo, CurrentStage models.HostStage) {
	log := logutil.FromContext(ctx, m.log)
	//get openshift version from cluster
	var cluster common.Cluster
	err := m.db.First(&cluster, "id = ?", h.ClusterID).Error
	if err != nil {
		log.WithError(err).Errorf("not reporting installation metrics - failed to find cluster %s", h.ClusterID)
		return
	}
	//get the boot disk
	var boot *models.Disk
	disks, err := m.hwValidator.GetHostValidDisks(h)
	if err == nil && len(disks) > 0 {
		boot = disks[0]
	}
	m.metricApi.ReportHostInstallationMetrics(log, cluster.OpenshiftVersion, h.ClusterID, cluster.EmailDomain, boot, h, previousProgress, CurrentStage)
}

func (m *Manager) AutoAssignRole(ctx context.Context, h *models.Host, db *gorm.DB) error {
	// select role if needed
	if h.Role == models.HostRoleAutoAssign {
		return m.autoRoleSelection(ctx, h, db)
	}
	return nil
}

func (m *Manager) autoRoleSelection(ctx context.Context, h *models.Host, db *gorm.DB) error {
	log := logutil.FromContext(ctx, m.log)
	if h.Inventory == "" {
		return errors.Errorf("host %s from cluster %s don't have hardware info",
			h.ID.String(), h.ClusterID.String())
	}
	role, err := m.selectRole(ctx, h, db)
	if err != nil {
		return err
	}
	// use sourced role to prevent races with user role setting
	if err := updateRole(m.log, h, role, db, swag.String(string(models.HostRoleAutoAssign))); err != nil {
		log.WithError(err).Errorf("failed to update role %s for host %s cluster %s",
			role, h.ID.String(), h.ClusterID.String())
	}
	log.Infof("Auto selected role %s for host %s cluster %s", role, h.ID.String(), h.ClusterID.String())
	// pointer was changed in selectRole or after the update - need to take the host again
	return db.Model(&models.Host{}).
		Take(h, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error
}

func (m *Manager) selectRole(ctx context.Context, h *models.Host, db *gorm.DB) (models.HostRole, error) {
	var (
		autoSelectedRole = models.HostRoleWorker
		log              = logutil.FromContext(ctx, m.log)
	)

	if IsDay2Host(h) {
		return autoSelectedRole, nil
	}

	// count already existing masters
	mastersCount := 0
	if err := db.Model(&models.Host{}).Where("cluster_id = ? and status != ? and role = ?",
		h.ClusterID, models.HostStatusDisabled, models.HostRoleMaster).Count(&mastersCount).Error; err != nil {
		log.WithError(err).Errorf("failed to count masters in cluster %s", h.ClusterID.String())
		return autoSelectedRole, err
	}

	if mastersCount < common.MinMasterHostsNeededForInstallation {
		h.Role = models.HostRoleMaster
		vc, err := newValidationContext(h, db)
		if err != nil {
			log.WithError(err).Errorf("failed to create new validation context for host %s", h.ID.String())
			return autoSelectedRole, err
		}
		conditions, _, err := m.rp.preprocess(vc)
		if err != nil {
			log.WithError(err).Errorf("failed to run validations on host %s", h.ID.String())
			return autoSelectedRole, err
		}
		if m.canBeMaster(conditions) {
			return models.HostRoleMaster, nil
		}
	}

	return autoSelectedRole, nil
}

func (m *Manager) IsValidMasterCandidate(h *models.Host, db *gorm.DB, log logrus.FieldLogger) (bool, error) {
	if swag.StringValue(h.Status) != models.HostStatusKnown || h.Role == models.HostRoleWorker {
		return false, nil
	}

	h.Role = models.HostRoleMaster
	vc, err := newValidationContext(h, db)
	if err != nil {
		log.WithError(err).Errorf("failed to create new validation context for host %s", h.ID.String())
		return false, err
	}

	conditions, _, err := m.rp.preprocess(vc)
	if err != nil {
		log.WithError(err).Errorf("failed to run validations on host %s", h.ID.String())
		return false, err
	}

	if m.canBeMaster(conditions) {
		return true, nil
	}

	return false, nil
}

func (m *Manager) canBeMaster(conditions map[validationID]bool) bool {
	if conditions[HasCPUCoresForRole] && conditions[HasMemoryForRole] {
		return true
	}
	return false
}

func (m *Manager) GetHostRequirements(role models.HostRole) models.HostRequirementsRole {
	return m.hwValidator.GetHostRequirements(role)
}

func (m Manager) PermanentHostsDeletion(olderThen strfmt.DateTime) error {
	var hosts []*models.Host
	db := m.db.Unscoped()
	if reply := db.Where("deleted_at < ?", olderThen).Delete(&hosts); reply.Error != nil {
		return reply.Error
	} else if reply.RowsAffected > 0 {
		m.log.Debugf("Deleted %s hosts from db", reply.RowsAffected)
	}
	return nil
}
