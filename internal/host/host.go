package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	reflect "reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

var BootstrapStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageWaitingForControlPlane,
	models.HostStageWaitingForBootkube, models.HostStageWaitingForController,
	models.HostStageRebooting, models.HostStageConfiguring, models.HostStageJoined,
	models.HostStageDone,
}
var MasterStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageRebooting,
	models.HostStageConfiguring, models.HostStageJoined, models.HostStageDone,
}
var WorkerStages = [...]models.HostStage{
	models.HostStageStartingInstallation, models.HostStageInstalling,
	models.HostStageWritingImageToDisk, models.HostStageRebooting,
	models.HostStageWaitingForIgnition, models.HostStageConfiguring,
	models.HostStageJoined, models.HostStageDone,
}

var manualRebootStages = []models.HostStage{
	models.HostStageRebooting,
	models.HostStageWaitingForIgnition,
	models.HostStageConfiguring,
	models.HostStageJoined,
	models.HostStageDone,
}

var InstallationProgressTimeout = map[models.HostStage]time.Duration{
	models.HostStageStartingInstallation:   30 * time.Minute,
	models.HostStageWaitingForControlPlane: 60 * time.Minute,
	models.HostStageWaitingForController:   60 * time.Minute,
	models.HostStageWaitingForBootkube:     60 * time.Minute,
	models.HostStageInstalling:             60 * time.Minute,
	models.HostStageJoined:                 60 * time.Minute,
	models.HostStageWritingImageToDisk:     30 * time.Minute,
	models.HostStageRebooting:              70 * time.Minute,
	models.HostStageConfiguring:            60 * time.Minute,
	models.HostStageWaitingForIgnition:     24 * time.Hour,
	"DEFAULT":                              60 * time.Minute,
}

var disconnectionValidationStages = []models.HostStage{
	models.HostStageWritingImageToDisk,
	models.HostStageInstalling,
}

var WrongBootOrderIgnoreTimeoutStages = []models.HostStage{
	models.HostStageWaitingForControlPlane,
	models.HostStageWaitingForController,
	models.HostStageWaitingForBootkube,
	models.HostStageRebooting,
}

var InstallationTimeout = 20 * time.Minute

var MaxHostDisconnectionTime = 3 * time.Minute

type LogTimeoutConfig struct {
	LogCollectionTimeout time.Duration `envconfig:"HOST_LOG_COLLECTION_TIMEOUT" default:"10m"`
	LogPendingTimeout    time.Duration `envconfig:"HOST_LOG_PENDING_TIMEOUT" default:"2m"`
}

type Config struct {
	LogTimeoutConfig
	EnableAutoReset         bool                    `envconfig:"ENABLE_AUTO_RESET" default:"false"`
	ResetTimeout            time.Duration           `envconfig:"RESET_CLUSTER_TIMEOUT" default:"3m"`
	MonitorBatchSize        int                     `envconfig:"HOST_MONITOR_BATCH_SIZE" default:"100"`
	DisabledHostvalidations DisabledHostValidations `envconfig:"DISABLED_HOST_VALIDATIONS" default:""` // Which host validations to disable (should not run in preprocess)
}

//go:generate mockgen -package=host -aux_files=github.com/openshift/assisted-service/internal/host/hostcommands=instruction_manager.go -destination=mock_host_api.go . API
type API interface {
	hostcommands.InstructionApi
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
	// auto assign host role
	AutoAssignRole(ctx context.Context, h *models.Host, db *gorm.DB) error
	IsValidMasterCandidate(h *models.Host, c *common.Cluster, db *gorm.DB, log logrus.FieldLogger) (bool, error)
	SetUploadLogsAt(ctx context.Context, h *models.Host, db *gorm.DB) error
	UpdateLogsProgress(ctx context.Context, h *models.Host, progress string) error
	PermanentHostsDeletion(olderThan strfmt.DateTime) error
	ReportValidationFailedMetrics(ctx context.Context, h *models.Host, ocpVersion, emailDomain string) error

	UpdateRole(ctx context.Context, h *models.Host, role models.HostRole, db *gorm.DB) error
	UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error
	UpdateInventory(ctx context.Context, h *models.Host, inventory string) error
	RefreshInventory(ctx context.Context, cluster *common.Cluster, h *models.Host, db *gorm.DB) error
	UpdateNTP(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource, db *gorm.DB) error
	UpdateMachineConfigPoolName(ctx context.Context, db *gorm.DB, h *models.Host, machineConfigPoolName string) error
	UpdateInstallationDisk(ctx context.Context, db *gorm.DB, h *models.Host, installationDiskId string) error
	UpdateKubeKeyNS(ctx context.Context, hostID, namespace string) error
	GetHostValidDisks(role *models.Host) ([]*models.Disk, error)
	UpdateImageStatus(ctx context.Context, h *models.Host, imageStatus *models.ContainerImageAvailability, db *gorm.DB) error
	SetDiskSpeed(ctx context.Context, h *models.Host, path string, speedMs int64, exitCode int64, db *gorm.DB) error
	ResetHostValidation(ctx context.Context, hostID, clusterID strfmt.UUID, validationID string, db *gorm.DB) error
}

type Manager struct {
	log                   logrus.FieldLogger
	db                    *gorm.DB
	instructionApi        hostcommands.InstructionApi
	hwValidator           hardware.Validator
	eventsHandler         events.Handler
	sm                    stateswitch.StateMachine
	rp                    *refreshPreprocessor
	metricApi             metrics.API
	Config                Config
	leaderElector         leader.Leader
	monitorQueryGenerator *common.MonitorQueryGenerator
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler, hwValidator hardware.Validator, instructionApi hostcommands.InstructionApi,
	hwValidatorCfg *hardware.ValidatorCfg, metricApi metrics.API, config *Config, leaderElector leader.ElectorInterface, operatorsApi operators.API) *Manager {
	th := &transitionHandler{
		db:            db,
		log:           log,
		config:        config,
		eventsHandler: eventsHandler,
	}
	return &Manager{
		log:            log,
		db:             db,
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		eventsHandler:  eventsHandler,
		sm:             NewHostStateMachine(th),
		rp:             newRefreshPreprocessor(log, hwValidatorCfg, hwValidator, operatorsApi, config.DisabledHostvalidations),
		metricApi:      metricApi,
		Config:         *config,
		leaderElector:  leaderElector,
	}
}

func (m *Manager) RegisterHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	dbHost, err := common.GetHostFromDB(db, h.ClusterID.String(), h.ID.String())
	var host *models.Host
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// Delete any previews record of the host if it was soft deleted from the cluster,
		// no error will be returned if the host was not existed.
		if err := db.Unscoped().Delete(&common.Host{}, "id = ? and cluster_id = ?", *h.ID, h.ClusterID).Error; err != nil {
			return errors.Wrapf(
				err,
				"error while trying to delete previews record from db (if exists) of host %s in cluster %s",
				h.ID.String(), h.ClusterID.String())
		}

		host = h
	} else {
		host = &dbHost.Host
	}

	return m.sm.Run(TransitionTypeRegisterHost, newStateHost(host), &TransitionArgsRegisterHost{
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
func (m *Manager) populateDisksEligibility(ctx context.Context, inventory *models.Inventory, cluster *common.Cluster, host *models.Host) error {
	for _, disk := range inventory.Disks {
		if !hardware.DiskEligibilityInitialized(disk) {
			// for backwards compatibility, pretend that the agent has decided that this disk is eligible
			disk.InstallationEligibility.Eligible = true
			disk.InstallationEligibility.NotEligibleReasons = make([]string, 0)
		}

		// Append to the existing reasons already filled in by the agent
		reasons, err := m.hwValidator.DiskIsEligible(ctx, disk, cluster, host)
		if err != nil {
			return err
		}
		disk.InstallationEligibility.NotEligibleReasons = reasons

		disk.InstallationEligibility.Eligible = len(disk.InstallationEligibility.NotEligibleReasons) == 0
	}
	return nil
}

// populateDisksId ensures that every disk has an id.
// The id used to identify the disk and mark a disk as selected
// This value should be equal to the host.installationDiskId
func (m *Manager) populateDisksId(inventory *models.Inventory) {
	for _, disk := range inventory.Disks {
		if disk.ID == "" {
			disk.ID = hostutil.GetDeviceIdentifier(disk)
		}
	}
}

func (m *Manager) HandlePrepareInstallationFailure(ctx context.Context, h *models.Host, reason string) error {

	lastStatusUpdateTime := h.StatusUpdatedAt
	err := m.sm.Run(TransitionTypeHostInstallationFailed, newStateHost(h), &TransitionArgsHostInstallationFailed{
		ctx:    ctx,
		reason: reason,
	})
	if err == nil {
		m.reportInstallationMetrics(ctx, h, &models.HostProgressInfo{CurrentStage: "installation command failed",
			StageStartedAt: lastStatusUpdateTime}, models.HostStageFailed)
	}
	return err
}

func (m *Manager) RefreshInventory(ctx context.Context, cluster *common.Cluster, h *models.Host, db *gorm.DB) error {
	return m.updateInventory(ctx, cluster, h, h.Inventory, db)
}

func (m *Manager) UpdateInventory(ctx context.Context, h *models.Host, inventoryStr string) error {
	log := logutil.FromContext(ctx, m.log)
	cluster, err := common.GetClusterFromDB(m.db, h.ClusterID, common.UseEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("not updating inventory - failed to find cluster %s", h.ClusterID)
		return common.NewApiError(http.StatusNotFound, err)
	}
	return m.updateInventory(ctx, cluster, h, inventoryStr, m.db)
}

// Check if the value of the
func (m *Manager) ntpSyncedChanged(c *common.Cluster, host *models.Host, inventoryStr string) bool {
	prev, err := common.IsNtpSynced(c)
	if err != nil {
		m.log.WithError(err).Error("Checking ntp synced")

		// Sine the was an error we would like to indicate that the synced has changed for safe side
		return true
	}
	for _, h := range c.Hosts {
		if h.ID.String() == host.ID.String() {
			h.Inventory = inventoryStr
			break
		}
	}
	current, err := common.IsNtpSynced(c)
	if err != nil {
		m.log.WithError(err).Error("Checking ntp synced")

		// Sine the was an error we would like to indicate that the synced has changed for safe side
		return true
	}
	return prev != current
}

// Create a JSON encoded inventory that can be compared to other such string for invariance.  Mainly it removes timestamps
// that are constantly changing
func canonizeInventory(inventoryStr string) string {
	var inventory models.Inventory
	err := json.Unmarshal([]byte(inventoryStr), &inventory)
	if err != nil {
		return inventoryStr
	}
	inventory.Timestamp = 0
	if inventory.Memory != nil {
		inventory.Memory.UsableBytes = 0
	}
	for _, d := range inventory.Disks {
		d.Smart = ""
	}
	b, err := json.Marshal(&inventory)
	if err != nil {
		return inventoryStr
	}
	return string(b)
}

func (m *Manager) updateInventory(ctx context.Context, cluster *common.Cluster, h *models.Host, inventoryStr string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, m.log)

	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := append(hostStatusesBeforeInstallation[:], models.HostStatusInstallingInProgress)

	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Host is in %s state, host can be updated only in one of %s states",
				hostStatus, allowedStatuses))
	}

	inventory, err := hostutil.UnmarshalInventory(inventoryStr)
	if err != nil {
		return err
	}

	err = m.populateDisksEligibility(ctx, inventory, cluster, h)
	if err != nil {
		log.WithError(err).Errorf("not updating inventory - failed to check disks eligibility for host %s", h.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	m.populateDisksId(inventory)

	marshalledInventory, err := hostutil.MarshalInventory(inventory)
	if err != nil {
		return err
	}

	validDisks := m.hwValidator.ListEligibleDisks(inventory)
	installationDisk := hostutil.DetermineInstallationDisk(validDisks, hostutil.GetHostInstallationPath(h))

	var (
		installationDiskPath string
		installationDiskID   string
	)
	if installationDisk == nil {
		installationDiskPath = ""
		installationDiskID = ""
	} else {
		installationDiskPath = hostutil.GetDeviceFullName(installationDisk)
		installationDiskID = hostutil.GetDeviceIdentifier(installationDisk)
	}

	// If there is substantial change in the inventory that might cause the state machine to move to a new status
	// or one of the validations to change, then the updated_at field has to be modified.  Otherwise, we just
	// perform update with touching the updated_at field
	if canonizeInventory(marshalledInventory) != canonizeInventory(h.Inventory) ||
		installationDiskPath != h.InstallationDiskPath ||
		installationDiskID != h.InstallationDiskID ||
		m.ntpSyncedChanged(cluster, h, marshalledInventory) {
		return db.Model(h).Update(map[string]interface{}{
			"inventory":              marshalledInventory,
			"installation_disk_path": installationDiskPath,
			"installation_disk_id":   installationDiskID,
		}).Error
	} else {
		return db.Model(h).UpdateColumns(map[string]interface{}{
			"inventory":              marshalledInventory,
			"installation_disk_path": installationDiskPath,
			"installation_disk_id":   installationDiskID,
		}).Error
	}
}

func (m *Manager) refreshStatusInternal(ctx context.Context, h *models.Host, c *common.Cluster, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	var (
		vc               *validationContext
		err              error
		conditions       map[string]bool
		newValidationRes ValidationsStatus
	)
	vc, err = newValidationContext(h, c, db, m.hwValidator)
	if err != nil {
		return err
	}
	conditions, newValidationRes, err = m.rp.preprocess(vc)
	if err != nil {
		return err
	}
	currentValidationRes, err := GetValidations(h)
	if err != nil {
		return err
	}
	if m.didValidationChanged(ctx, newValidationRes, currentValidationRes) {
		// Validation status changes are detected when new validations are different from the
		// current validations in the DB.
		// For changes to be detected and reported correctly, the comparison needs to be
		// performed before the new validations are updated to the DB.
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)
		_, err = m.updateValidationsInDB(ctx, db, h, newValidationRes)
		if err != nil {
			return err
		}
	}

	err = m.sm.Run(TransitionTypeRefresh, newStateHost(h), &TransitionArgsRefreshHost{
		ctx:               ctx,
		db:                db,
		eventHandler:      m.eventsHandler,
		conditions:        conditions,
		validationResults: newValidationRes,
	})
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	return m.refreshStatusInternal(ctx, h, nil, db)
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
		previousProgress.CurrentStage == progress.CurrentStage {
		if previousProgress.ProgressInfo == progress.ProgressInfo {
			return nil
		}
		updates := map[string]interface{}{
			"progress_progress_info":    progress.ProgressInfo,
			"progress_stage_updated_at": strfmt.DateTime(time.Now()),
		}
		return m.db.Model(h).UpdateColumns(updates).Error
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
		if currentIndex < indexOfStage(previousProgress.CurrentStage, stages) &&
			!m.allowStageOutOfOrder(h, progress.CurrentStage) {
			return errors.Errorf("Can't assign lower stage \"%s\" after host has been in stage \"%s\"",
				progress.CurrentStage, previousProgress.CurrentStage)
		}
	}

	statusInfo := string(progress.CurrentStage)

	var err error
	switch progress.CurrentStage {
	case models.HostStageDone:
		_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusInstalled, statusInfo,
			previousProgress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
	case models.HostStageFailed:
		// Keeps the last progress

		if progress.ProgressInfo != "" {
			statusInfo += fmt.Sprintf(" - %s", progress.ProgressInfo)
		}

		_, err = hostutil.UpdateHostStatus(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusError, statusInfo)
	case models.HostStageRebooting:
		if swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost {
			_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
				swag.StringValue(h.Status), models.HostStatusAddedToExistingCluster, statusInfo,
				h.Progress.CurrentStage, progress.CurrentStage, progress.ProgressInfo)
			break
		}
		fallthrough
	default:
		_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
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
func (m *Manager) UpdateLogsProgress(ctx context.Context, h *models.Host, progress string) error {
	_, err := hostutil.UpdateLogsProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.ClusterID, *h.ID,
		swag.StringValue(h.Status), progress)
	return err
}

func (m *Manager) SetUploadLogsAt(ctx context.Context, h *models.Host, db *gorm.DB) error {
	err := db.Model(h).Update("logs_collected_at", strfmt.DateTime(time.Now())).Error
	if err != nil {
		return errors.Wrapf(err, "failed to set logs_collected_at to host %s", h.ID.String())
	}
	return nil
}

// Create JSON encoded connectivity report that can be checked against similar string if the
// connectivity between the current host and other hosts has changed
func canonizeConnectivity(connectivityReportStr string) string {
	var connectivityReport models.ConnectivityReport
	err := json.Unmarshal([]byte(connectivityReportStr), &connectivityReport)
	if err != nil {
		return connectivityReportStr
	}
	for _, h := range connectivityReport.RemoteHosts {
		for _, l3 := range h.L3Connectivity {
			l3.AverageRTTMs = 0
			l3.PacketLossPercentage = 0
		}
		l3 := h.L3Connectivity
		sort.Slice(l3, func(i, j int) bool {
			if l3[i].RemoteIPAddress != l3[j].RemoteIPAddress {
				return l3[i].RemoteIPAddress < l3[j].RemoteIPAddress
			}
			return l3[i].OutgoingNic < l3[j].OutgoingNic
		})
		l2 := h.L2Connectivity
		sort.Slice(l2, func(i, j int) bool {
			if l2[i].RemoteIPAddress != l2[j].RemoteIPAddress {
				return l2[i].RemoteIPAddress < l2[j].RemoteIPAddress
			}
			return l2[i].OutgoingNic < l2[j].OutgoingNic
		})
	}
	sort.Slice(connectivityReport.RemoteHosts, func(i, j int) bool {
		return connectivityReport.RemoteHosts[i].HostID.String() < connectivityReport.RemoteHosts[j].HostID.String()
	})
	b, err := json.Marshal(&connectivityReport)
	if err != nil {
		return connectivityReportStr
	}
	return string(b)
}

func (m *Manager) UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error {
	if h.Connectivity != connectivityReport {
		var err error
		// Only if the connectivity between the hosts changed change the updated_at field
		if canonizeConnectivity(h.Connectivity) != canonizeConnectivity(connectivityReport) {
			err = m.db.Model(h).Update("connectivity", connectivityReport).Error
		} else {
			err = m.db.Model(h).UpdateColumn("connectivity", connectivityReport).Error
		}
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
	if !hostutil.IsDay2Host(h) {
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

func (m *Manager) UpdateImageStatus(ctx context.Context, h *models.Host, newImageStatus *models.ContainerImageAvailability, db *gorm.DB) error {
	hostImageStatuses, err := common.UnmarshalImageStatuses(h.ImagesStatus)
	if err != nil {
		return errors.Wrapf(err, "Unmarshal image status for host %s", h.ID.String())
	}

	oldImageStatus, alreadyExists := common.GetImageStatus(hostImageStatuses, newImageStatus.Name)
	if alreadyExists {
		m.log.Infof("Updating image status for %s with status %s to host %s", newImageStatus.Name, newImageStatus.Result, h.ID.String())
		oldImageStatus.Result = newImageStatus.Result
		common.SetImageStatus(hostImageStatuses, oldImageStatus)
	} else {
		common.SetImageStatus(hostImageStatuses, newImageStatus)
		m.log.Infof("Adding new image status for %s with status %s to host %s", newImageStatus.Name, newImageStatus.Result, h.ID.String())
		m.metricApi.ImagePullStatus(*h.ID, newImageStatus.Name, string(newImageStatus.Result), newImageStatus.DownloadRate)

		eventInfo := fmt.Sprintf("Host %s: New image status %s. result: %s.",
			hostutil.GetHostnameForMsg(h), newImageStatus.Name, newImageStatus.Result)

		if newImageStatus.SizeBytes > 0 {
			eventInfo += fmt.Sprintf(" time: %f seconds; size: %f bytes; download rate: %f MBps",
				newImageStatus.Time, newImageStatus.SizeBytes, newImageStatus.DownloadRate)
		}

		m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityInfo, eventInfo, time.Now())
	}
	marshalledStatuses, err := common.MarshalImageStatuses(hostImageStatuses)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal image statuses for host %s", h.ID.String())
	}

	return db.Model(h).Update("images_status", marshalledStatuses).Error
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

func (m *Manager) UpdateInstallationDisk(ctx context.Context, db *gorm.DB, h *models.Host, installationDiskPath string) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallation[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host name can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	validDisks, err := m.hwValidator.GetHostValidDisks(h)
	if err != nil {
		return err
	}

	matchedInstallationDisk := hostutil.GetDiskByInstallationPath(validDisks, installationDiskPath)

	if matchedInstallationDisk == nil {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Requested installation disk is not part of the host's valid disks"))
	}

	h.InstallationDiskPath = hostutil.GetDeviceFullName(matchedInstallationDisk)
	h.InstallationDiskID = hostutil.GetDeviceIdentifier(matchedInstallationDisk)
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return cdb.Model(h).Update(map[string]interface{}{
		"installation_disk_path": h.InstallationDiskPath,
		"installation_disk_id":   h.InstallationDiskID,
	}).Error
}

func (m *Manager) UpdateKubeKeyNS(ctx context.Context, hostID, namespace string) error {
	return m.db.Model(&common.Host{}).Where("id = ?", hostID).Update("kube_key_namespace", namespace).Error
}

func (m *Manager) CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	eventSeverity := models.EventSeverityInfo
	eventInfo := fmt.Sprintf("Installation cancelled for host %s", hostutil.GetHostnameForMsg(h))
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

func (m *Manager) reportInstallationMetrics(ctx context.Context, h *models.Host, previousProgress *models.HostProgressInfo, CurrentStage models.HostStage) {
	log := logutil.FromContext(ctx, m.log)
	//get openshift version from cluster
	var cluster common.Cluster
	err := m.db.First(&cluster, "id = ?", h.ClusterID).Error
	if err != nil {
		log.WithError(err).Errorf("not reporting installation metrics - failed to find cluster %s", h.ClusterID)
		return
	}

	boot, err := hostutil.GetHostInstallationDisk(h)

	if err != nil {
		log.WithError(err).Errorf("host %s in cluster %s: error fetching installation disk", h.ID.String(), h.ClusterID.String())
	} else if boot == nil {
		log.Errorf("host %s in cluster %s has empty installation path", h.ID.String(), h.ClusterID.String())
	}

	m.metricApi.ReportHostInstallationMetrics(ctx, cluster.OpenshiftVersion, h.ClusterID, cluster.EmailDomain, boot, h, previousProgress, CurrentStage)
}

func (m *Manager) ReportValidationFailedMetrics(ctx context.Context, h *models.Host, ocpVersion, emailDomain string) error {
	log := logutil.FromContext(ctx, m.log)
	if h.ValidationsInfo == "" {
		log.Warnf("Host %s in cluster %s doesn't contain any validations info, cannot report metrics for that host", h.ID, h.ClusterID)
		return nil
	}
	var validationRes ValidationsStatus
	if err := json.Unmarshal([]byte(h.ValidationsInfo), &validationRes); err != nil {
		log.WithError(err).Errorf("Failed to unmarshal validations info from host %s in cluster %s", h.ID, h.ClusterID)
		return err
	}
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.Status == ValidationFailure {
				m.metricApi.HostValidationFailed(ocpVersion, emailDomain, models.HostValidationID(v.ID))
			}
		}
	}
	return nil
}

func (m *Manager) reportValidationStatusChanged(ctx context.Context, vc *validationContext, h *models.Host,
	newValidationRes, currentValidationRes ValidationsStatus) {
	for vCategory, vRes := range newValidationRes {
		for _, v := range vRes {
			if currentStatus, ok := m.getValidationStatus(currentValidationRes, vCategory, v.ID); ok {
				if v.Status == ValidationFailure && currentStatus == ValidationSuccess {
					m.metricApi.HostValidationChanged(vc.cluster.OpenshiftVersion, vc.cluster.EmailDomain, models.HostValidationID(v.ID))
					eventMsg := fmt.Sprintf("Host %s: validation '%s' that used to succeed is now failing", hostutil.GetHostnameForMsg(h), v.ID)
					m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityWarning, eventMsg, time.Now())
				}
				if v.Status == ValidationSuccess && currentStatus == ValidationFailure {
					eventMsg := fmt.Sprintf("Host %s: validation '%s' is now fixed", hostutil.GetHostnameForMsg(h), v.ID)
					m.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityInfo, eventMsg, time.Now())
				}
			}
		}
	}
}

func (m *Manager) getValidationStatus(vs ValidationsStatus, category string, vID validationID) (ValidationStatus, bool) {
	for _, v := range vs[category] {
		if v.ID == vID {
			return v.Status, true
		}
	}
	return ValidationStatus(""), false
}

func (m *Manager) didValidationChanged(ctx context.Context, newValidationRes, currentValidationRes ValidationsStatus) bool {
	if len(newValidationRes) == 0 {
		// in order to be considered as a change, newValidationRes should not contain less data than currentValidations
		return false
	}
	return !reflect.DeepEqual(newValidationRes, currentValidationRes)
}

func (m *Manager) updateValidationsInDB(ctx context.Context, db *gorm.DB, h *models.Host, newValidationRes ValidationsStatus) (*common.Host, error) {
	b, err := json.Marshal(newValidationRes)
	if err != nil {
		return nil, err
	}
	return hostutil.UpdateHost(logutil.FromContext(ctx, m.log), db, h.ClusterID, *h.ID, *h.Status, "validations_info", string(b))
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
		err              error
		vc               *validationContext
	)

	if hostutil.IsDay2Host(h) {
		return autoSelectedRole, nil
	}

	// count already existing masters
	mastersCount := 0
	if err = db.Model(&models.Host{}).Where("cluster_id = ? and status != ? and role = ?",
		h.ClusterID, models.HostStatusDisabled, models.HostRoleMaster).Count(&mastersCount).Error; err != nil {
		log.WithError(err).Errorf("failed to count masters in cluster %s", h.ClusterID.String())
		return autoSelectedRole, err
	}

	if mastersCount < common.MinMasterHostsNeededForInstallation {
		h.Role = models.HostRoleMaster
		vc, err = newValidationContext(h, nil, db, m.hwValidator)
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

func (m *Manager) IsValidMasterCandidate(h *models.Host, c *common.Cluster, db *gorm.DB, log logrus.FieldLogger) (bool, error) {
	if h.Role == models.HostRoleWorker {
		return false, nil
	}

	h.Role = models.HostRoleMaster

	vc, err := newValidationContext(h, c, db, m.hwValidator)
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

func (m *Manager) canBeMaster(conditions map[string]bool) bool {
	if conditions[HasCPUCoresForRole.String()] && conditions[HasMemoryForRole.String()] {
		return true
	}
	return false
}

func (m *Manager) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	return m.hwValidator.GetHostValidDisks(host)
}

func (m *Manager) SetDiskSpeed(ctx context.Context, h *models.Host, path string, speedMs int64, exitCode int64, db *gorm.DB) error {
	log := logutil.FromContext(ctx, m.log)
	if db == nil {
		db = m.db
	}
	disksInfo, err := common.SetDiskSpeed(path, speedMs, exitCode, h.DisksInfo)
	if err != nil {
		log.WithError(err).Errorf("Could not set disk response value in %s", h.DisksInfo)
		return err
	}
	if disksInfo != h.DisksInfo {
		resultDb := db.Model(h).UpdateColumn("disks_info", disksInfo)
		if resultDb.Error != nil {
			log.WithError(err).Errorf("Update disk info for host %s", h.ID.String())
			return resultDb.Error
		}
		if resultDb.RowsAffected == 0 {
			err = errors.Errorf("No row updated for disk info.  Host %s", h.ID.String())
			log.WithError(err).Error("Disks info")
			return err
		}
	}
	return nil
}

func (m *Manager) resetDiskSpeedValidation(host *models.Host, log logrus.FieldLogger, db *gorm.DB) error {
	bootDevice, err := hardware.GetBootDevice(m.hwValidator, host)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.New("Get boot device"))
	}
	var updatedHost models.Host
	updatedHost.DisksInfo, err = common.ResetDiskSpeed(bootDevice, host.DisksInfo)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.New("Reset disk speed"))
	}
	return db.Model(&models.Host{}).Where("cluster_id = ? and id = ?", host.ClusterID.String(), host.ID.String()).Update(&updatedHost).Error
}

func (m *Manager) resetContainerImagesValidation(host *models.Host, db *gorm.DB) error {
	return db.Model(&models.Host{}).Where("cluster_id = ? and id = ?", host.ClusterID.String(), host.ID.String()).Updates(
		map[string]interface{}{
			"images_status": "",
		}).Error
}

func (m *Manager) ResetHostValidation(ctx context.Context, hostID, clusterID strfmt.UUID, validationID string, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	h, err := common.GetHostFromDB(db, clusterID.String(), hostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, errors.Wrapf(err, "Host %s of cluster %s was not found", hostID.String(), clusterID.String()))
		}
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Unexpected error while getting host %s of cluster %s", hostID.String(), clusterID.String()))
	}
	log := logutil.FromContext(ctx, m.log)
	host := &h.Host
	switch validationID {
	case string(models.HostValidationIDSufficientInstallationDiskSpeed):
		return m.resetDiskSpeedValidation(host, log, db)
	case string(models.HostValidationIDContainerImagesAvailable):
		return m.resetContainerImagesValidation(host, db)
	default:
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Validation \"%s\" cannot be reset or does not exist", validationID))
	}
}

func (m Manager) PermanentHostsDeletion(olderThan strfmt.DateTime) error {
	var hosts []*models.Host
	db := m.db.Unscoped()
	if reply := db.Where("deleted_at < ?", olderThan).Delete(&hosts); reply.Error != nil {
		return reply.Error
	} else if reply.RowsAffected > 0 {
		m.log.Debugf("Deleted %s hosts from db", reply.RowsAffected)
	}
	return nil
}

func (m *Manager) getHostCluster(host *models.Host) (*common.Cluster, error) {
	var cluster common.Cluster
	err := m.db.First(&cluster, "id = ?", host.ClusterID).Error
	if err != nil {
		m.log.WithError(err).Errorf("Failed to find cluster %s", host.ClusterID)
		return nil, errors.Errorf("Failed to find cluster %s", host.ClusterID)
	}
	return &cluster, nil
}

func (m *Manager) allowStageOutOfOrder(h *models.Host, stage models.HostStage) bool {
	// Return True in case the given stage order is exceptional in case of SNO
	if stage != models.HostStageWritingImageToDisk {
		return false
	}
	cluster, err := m.getHostCluster(h)
	if err != nil {
		m.log.Debug("Can't check if host is part of single node OpenShift")
		return false
	}
	if !common.IsSingleNodeCluster(cluster) {
		return false
	}
	return true
}

type DisabledHostValidations map[string]struct{}

func (d *DisabledHostValidations) Decode(value string) error {
	disabledHostValidations := DisabledHostValidations{}
	if len(strings.Trim(value, "")) == 0 {
		*d = disabledHostValidations
		return nil
	}
	for _, element := range strings.Split(value, ",") {
		if len(element) == 0 {
			return fmt.Errorf("empty host validation ID found in '%s'", value)
		}
		disabledHostValidations[element] = struct{}{}
	}
	*d = disabledHostValidations
	return nil
}

func (d DisabledHostValidations) IsDisabled(id validationID) bool {
	_, ok := d[id.String()]
	return ok
}
