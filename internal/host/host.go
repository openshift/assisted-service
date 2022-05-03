package host

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	reflect "reflect"
	"strconv"
	"strings"
	"time"

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
)

var InstallationProgressTimeout = map[models.HostStage]time.Duration{
	models.HostStageStartingInstallation:   30 * time.Minute,
	models.HostStageWaitingForControlPlane: 60 * time.Minute,
	models.HostStageWaitingForController:   60 * time.Minute,
	models.HostStageWaitingForBootkube:     60 * time.Minute,
	models.HostStageInstalling:             60 * time.Minute,
	models.HostStageJoined:                 60 * time.Minute,
	models.HostStageWritingImageToDisk:     30 * time.Minute,
	models.HostStageRebooting:              40 * time.Minute,
	models.HostStageConfiguring:            60 * time.Minute,
	models.HostStageWaitingForIgnition:     24 * time.Hour,
	"DEFAULT":                              60 * time.Minute,
}

const singleNodeRebootTimeout = 80 * time.Minute

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

//Weights for sorting hosts in the monitor
const (
	HostWeightMinimumCpuCores        float64 = 4
	HostWeightMinimumMemGib          float64 = 16
	HostWeightMinimumDiskCapacityGib float64 = 100
	HostWeightMemWeight              float64 = 0.1
	HostWeightDiskWeight             float64 = 0.004
)

type LogTimeoutConfig struct {
	LogCollectionTimeout time.Duration `envconfig:"HOST_LOG_COLLECTION_TIMEOUT" default:"10m"`
	LogPendingTimeout    time.Duration `envconfig:"HOST_LOG_PENDING_TIMEOUT" default:"2m"`
}

type Config struct {
	LogTimeoutConfig
	EnableAutoReset         bool                    `envconfig:"ENABLE_AUTO_RESET" default:"false"`
	EnableAutoAssign        bool                    `envconfig:"ENABLE_AUTO_ASSIGN" default:"true"`
	ResetTimeout            time.Duration           `envconfig:"RESET_CLUSTER_TIMEOUT" default:"3m"`
	MonitorBatchSize        int                     `envconfig:"HOST_MONITOR_BATCH_SIZE" default:"100"`
	DisabledHostvalidations DisabledHostValidations `envconfig:"DISABLED_HOST_VALIDATIONS" default:""` // Which host validations to disable (should not run in preprocess)
	BootstrapHostMAC        string                  `envconfig:"BOOTSTRAP_HOST_MAC" default:""`        // For ephemeral installer to ensure the bootstrap for the (single) cluster lands on the same host as assisted-service
}

//go:generate mockgen --build_flags=--mod=mod -package=host -aux_files=github.com/openshift/assisted-service/internal/host/hostcommands=instruction_manager.go -destination=mock_host_api.go . API
type API interface {
	hostcommands.InstructionApi
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host, db *gorm.DB) error
	UnRegisterHost(ctx context.Context, hostID, infraEnvID string) error
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

	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) error
	GetStagesByRole(h *models.Host, isSNO bool) []models.HostStage
	IndexOfStage(element models.HostStage, data []models.HostStage) int
	IsInstallable(h *models.Host) bool
	// auto assign host role
	AutoAssignRole(ctx context.Context, h *models.Host, db *gorm.DB) (bool, error)
	RefreshRole(ctx context.Context, h *models.Host, db *gorm.DB) error
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
	UpdateIgnitionEndpointToken(ctx context.Context, db *gorm.DB, h *models.Host, token string) error
	UpdateNodeLabels(ctx context.Context, h *models.Host, nodeLabelsStr string, db *gorm.DB) error
	UpdateInstallationDisk(ctx context.Context, db *gorm.DB, h *models.Host, installationDiskId string) error
	UpdateKubeKeyNS(ctx context.Context, hostID, namespace string) error
	GetHostValidDisks(role *models.Host) ([]*models.Disk, error)
	UpdateImageStatus(ctx context.Context, h *models.Host, imageStatus *models.ContainerImageAvailability, db *gorm.DB) error
	SetDiskSpeed(ctx context.Context, h *models.Host, path string, speedMs int64, exitCode int64, db *gorm.DB) error
	ResetHostValidation(ctx context.Context, hostID, infraEnvID strfmt.UUID, validationID string, db *gorm.DB) error
	GetHostByKubeKey(key types.NamespacedName) (*common.Host, error)
	UpdateDomainNameResolution(ctx context.Context, h *models.Host, domainResolutionResponse models.DomainResolutionResponse, db *gorm.DB) error
	BindHost(ctx context.Context, h *models.Host, clusterID strfmt.UUID, db *gorm.DB) error
	UnbindHost(ctx context.Context, h *models.Host, db *gorm.DB) error
}

type Manager struct {
	log                           logrus.FieldLogger
	db                            *gorm.DB
	instructionApi                hostcommands.InstructionApi
	hwValidator                   hardware.Validator
	eventsHandler                 eventsapi.Handler
	sm                            stateswitch.StateMachine
	rp                            *refreshPreprocessor
	metricApi                     metrics.API
	Config                        Config
	leaderElector                 leader.Leader
	monitorClusterQueryGenerator  *common.MonitorClusterQueryGenerator
	monitorInfraEnvQueryGenerator *common.MonitorInfraEnvQueryGenerator
}

func NewManager(log logrus.FieldLogger, db *gorm.DB, eventsHandler eventsapi.Handler, hwValidator hardware.Validator, instructionApi hostcommands.InstructionApi,
	hwValidatorCfg *hardware.ValidatorCfg, metricApi metrics.API, config *Config, leaderElector leader.ElectorInterface, operatorsApi operators.API, providerRegistry registry.ProviderRegistry) *Manager {
	th := &transitionHandler{
		db:            db,
		log:           log,
		config:        config,
		eventsHandler: eventsHandler,
	}
	sm := NewHostStateMachine(stateswitch.NewStateMachine(), th)
	sm = NewPoolHostStateMachine(sm, th)
	return &Manager{
		log:            log,
		db:             db,
		instructionApi: instructionApi,
		hwValidator:    hwValidator,
		eventsHandler:  eventsHandler,
		sm:             sm,
		rp:             newRefreshPreprocessor(log, hwValidatorCfg, hwValidator, operatorsApi, config.DisabledHostvalidations, providerRegistry),
		metricApi:      metricApi,
		Config:         *config,
		leaderElector:  leaderElector,
	}
}

func (m *Manager) RegisterHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	dbHost, err := common.GetHostFromDB(db, h.InfraEnvID.String(), h.ID.String())
	var host *models.Host
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// Delete any previews record of the host if it was soft deleted from the cluster,
		// no error will be returned if the host was not existed.
		if err := db.Unscoped().Delete(&common.Host{}, "id = ? and infra_env_id = ?", *h.ID, h.InfraEnvID).Error; err != nil {
			return errors.Wrapf(
				err,
				"error while trying to delete previews record from db (if exists) of host %s in infra env %s",
				h.ID.String(), h.InfraEnvID.String())
		}

		host = h
	} else {
		host = &dbHost.Host
		if h != nil {
			host.Kind = h.Kind
		}
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
func (m *Manager) populateDisksEligibility(ctx context.Context, inventory *models.Inventory, infraEnv *common.InfraEnv, cluster *common.Cluster, host *models.Host) error {
	for _, disk := range inventory.Disks {
		if !hardware.DiskEligibilityInitialized(disk) {
			// for backwards compatibility, pretend that the agent has decided that this disk is eligible
			disk.InstallationEligibility.Eligible = true
			disk.InstallationEligibility.NotEligibleReasons = make([]string, 0)
		}

		// Append to the existing reasons already filled in by the agent
		reasons, err := m.hwValidator.DiskIsEligible(ctx, disk, infraEnv, cluster, host)
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
	return m.updateInventory(ctx, nil, h, inventoryStr, m.db)
}

func (m *Manager) updateInventory(ctx context.Context, cluster *common.Cluster, h *models.Host, inventoryStr string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, m.log)

	hostStatus := swag.StringValue(h.Status)
	allowedStatuses := append(hostStatusesBeforeInstallation[:], models.HostStatusInstallingInProgress)
	allowedStatuses = append(allowedStatuses, hostStatusesInInfraEnv[:]...)

	if !funk.ContainsString(allowedStatuses, hostStatus) {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Host is in %s state, host can be updated only in one of %s states",
				hostStatus, allowedStatuses))
	}
	inventory, err := common.UnmarshalInventory(inventoryStr)
	if err != nil {
		return err
	}

	if h.ClusterID != nil && h.ClusterID.String() != "" {
		cluster, err = common.GetClusterFromDB(m.db, *h.ClusterID, common.UseEagerLoading)
		if err != nil {
			log.WithError(err).Errorf("not updating inventory - failed to find cluster %s", h.ClusterID.String())
			return common.NewApiError(http.StatusNotFound, err)
		}
	}

	infraEnv, err := common.GetInfraEnvFromDB(m.db, h.InfraEnvID)
	if err != nil {
		log.WithError(err).Errorf("not updating inventory - failed to find infra env %s", h.InfraEnvID.String())
		return common.NewApiError(http.StatusNotFound, err)
	}

	if m.Config.BootstrapHostMAC != "" && !h.Bootstrap {
		for _, iface := range inventory.Interfaces {
			if iface.MacAddress == m.Config.BootstrapHostMAC {
				log.Infof("selected local bootstrap host %s for cluster %s", h.ID, cluster.ID)
				err = updateRole(log, h, models.HostRoleMaster, models.HostRoleMaster, db, string(h.Role))
				if err != nil {
					log.WithError(err).Errorf("failed to set master role on bootstrap host for cluster %s", cluster.ID)
					return errors.Wrapf(err, "Failed to set master role on bootstrap host for cluster %s", cluster.ID)
				}
				err = m.SetBootstrap(ctx, h, true, db)
				if err != nil {
					log.WithError(err).Errorf("failed to update bootstrap host for cluster %s", cluster.ID)
					return errors.Wrapf(err, "Failed to update bootstrap host for cluster %s", cluster.ID)
				}
				break
			}
		}
	}

	err = m.populateDisksEligibility(ctx, inventory, infraEnv, cluster, h)
	if err != nil {
		log.WithError(err).Errorf("not updating inventory - failed to check disks eligibility for host %s", h.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	m.populateDisksId(inventory)
	inventoryStr, err = common.MarshalInventory(inventory)
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
	return db.Model(h).Updates(map[string]interface{}{
		"inventory":              inventoryStr,
		"installation_disk_path": installationDiskPath,
		"installation_disk_id":   installationDiskID,
	}).Error
}

func (m *Manager) refreshRoleInternal(ctx context.Context, h *models.Host, db *gorm.DB, forceRefresh bool) error {
	//update suggested role, if not yet set
	var suggestedRole models.HostRole
	var err error
	if m.Config.EnableAutoAssign || forceRefresh {
		//because of possible hw changes, suggested role should be calculated
		//periodically even if the suggested role is already set
		if h.Role == models.HostRoleAutoAssign &&
			funk.ContainsString(hostStatusesBeforeInstallation[:], *h.Status) {
			if suggestedRole, err = m.autoRoleSelection(ctx, h, db); err == nil {
				if h.SuggestedRole != suggestedRole {
					if err = updateRole(m.log, h, h.Role, suggestedRole, db, string(h.Role)); err == nil {
						h.SuggestedRole = suggestedRole
						m.log.Infof("suggested role for host %s is %s", *h.ID, suggestedRole)
						eventgen.SendHostRoleUpdatedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, hostutil.GetHostnameForMsg(h), string(suggestedRole))
					}
				}
			}
		}
	}
	return err
}

func (m *Manager) refreshStatusInternal(ctx context.Context, h *models.Host, c *common.Cluster, i *common.InfraEnv, db *gorm.DB) error {
	log := logutil.FromContext(ctx, m.log)
	if db == nil {
		db = m.db
	}
	var (
		vc               *validationContext
		err              error
		conditions       map[string]bool
		newValidationRes ValidationsStatus
	)
	vc, err = newValidationContext(h, c, i, db, m.hwValidator)
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
	log.Debugf("Host %s: validation details: %+v", hostutil.GetHostnameForMsg(h), currentValidationRes)
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

func (m *Manager) RefreshRole(ctx context.Context, h *models.Host, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	return m.refreshRoleInternal(ctx, h, db, true)
}

func (m *Manager) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	return m.refreshStatusInternal(ctx, h, nil, nil, db)
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

func (m *Manager) BindHost(ctx context.Context, h *models.Host, clusterID strfmt.UUID, db *gorm.DB) error {
	return m.sm.Run(TransitionTypeBindHost, newStateHost(h), &TransitionArgsBindHost{
		ctx:       ctx,
		db:        db,
		clusterID: clusterID,
	})
}

func (m *Manager) UnbindHost(ctx context.Context, h *models.Host, db *gorm.DB) error {
	return m.sm.Run(TransitionTypeUnbindHost, newStateHost(h), &TransitionArgsUnbindHost{
		ctx: ctx,
		db:  db,
	})
}

func (m *Manager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {
	return m.instructionApi.GetNextSteps(ctx, host)
}

func (m *Manager) IndexOfStage(element models.HostStage, data []models.HostStage) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1 // not found.
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

	var extra []interface{}
	if progress.CurrentStage != models.HostStageFailed {
		isSno := hostutil.IsSingleNode(m.log, m.db, h)

		stages := m.GetStagesByRole(h, isSno)
		if previousProgress != nil && previousProgress.CurrentStage != "" {
			// Verify the new stage is higher or equal to the current host stage according to its role stages array
			currentIndex := m.IndexOfStage(progress.CurrentStage, stages)

			if currentIndex == -1 {
				return errors.Errorf("Stages %s isn't available for host role %s bootstrap %s",
					progress.CurrentStage, h.Role, strconv.FormatBool(h.Bootstrap))
			}
			if currentIndex < m.IndexOfStage(previousProgress.CurrentStage, stages) {
				return errors.Errorf("Can't assign lower stage \"%s\" after host has been in stage \"%s\"",
					progress.CurrentStage, previousProgress.CurrentStage)
			}
		}

		currentIndex := m.IndexOfStage(progress.CurrentStage, stages)
		installationPercentage := (float64(currentIndex+1) / float64(len(stages))) * 100
		extra = append(extra, "progress_installation_percentage", installationPercentage)
	}

	statusInfo := string(progress.CurrentStage)

	var err error
	switch progress.CurrentStage {
	case models.HostStageDone:
		_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.InfraEnvID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusInstalled, statusInfo,
			previousProgress.CurrentStage, progress.CurrentStage, progress.ProgressInfo, extra...)
	case models.HostStageFailed:
		// Keeps the last progress

		if progress.ProgressInfo != "" {
			statusInfo += fmt.Sprintf(" - %s", progress.ProgressInfo)
		}

		_, err = hostutil.UpdateHostStatus(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.InfraEnvID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusError, statusInfo)
	case models.HostStageRebooting:
		if swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost {
			_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.InfraEnvID, *h.ID,
				swag.StringValue(h.Status), models.HostStatusAddedToExistingCluster, statusInfoRebootingDay2,
				h.Progress.CurrentStage, models.HostStageDone, progress.ProgressInfo, extra...)
			break
		}
		fallthrough
	default:
		_, err = hostutil.UpdateHostProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.InfraEnvID, *h.ID,
			swag.StringValue(h.Status), models.HostStatusInstallingInProgress, statusInfo,
			previousProgress.CurrentStage, progress.CurrentStage, progress.ProgressInfo, extra...)
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
		eventgen.SendHostBootstrapSetEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID, hostutil.GetHostnameForMsg(h))
	}
	return nil
}
func (m *Manager) UpdateLogsProgress(ctx context.Context, h *models.Host, progress string) error {
	_, err := hostutil.UpdateLogsProgress(ctx, logutil.FromContext(ctx, m.log), m.db, m.eventsHandler, h.InfraEnvID, *h.ID,
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

func (m *Manager) UpdateConnectivityReport(ctx context.Context, h *models.Host, connectivityReport string) error {
	if h.Connectivity != connectivityReport {
		// Only if the connectivity between the hosts changed change the updated_at field
		if err := m.db.Model(h).Update("connectivity", connectivityReport).Error; err != nil {
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

	//UpdateRole is always invoked from an API and therefore
	//the roles are user-selected. In this case suggested roles
	//takes the user selection
	return updateRole(m.log, h, role, role, cdb, string(h.Role))
}

func (m *Manager) UpdateMachineConfigPoolName(ctx context.Context, db *gorm.DB, h *models.Host, machineConfigPoolName string) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host machine config pool can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	cdb := m.db
	if db != nil {
		cdb = db
	}

	return cdb.Model(common.Host{Host: *h}).Updates(map[string]interface{}{"machine_config_pool_name": machineConfigPoolName, "trigger_monitor_timestamp": time.Now()}).Error
}

func (m *Manager) UpdateIgnitionEndpointToken(ctx context.Context, db *gorm.DB, h *models.Host, token string) error {
	hostStatus := swag.StringValue(h.Status)
	if token != "" && !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host ignition endpoint token can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	cdb := m.db
	if db != nil {
		cdb = db
	}

	tokenSet := true
	if token == "" {
		tokenSet = false
	}

	return cdb.Model(common.Host{Host: *h}).Updates(map[string]interface{}{
		"ignition_endpoint_token":     token,
		"ignition_endpoint_token_set": tokenSet,
		"trigger_monitor_timestamp":   time.Now()}).Error
}

func (m *Manager) UpdateNodeLabels(ctx context.Context, h *models.Host, nodeLabelsStr string, db *gorm.DB) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, labels can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	h.NodeLabels = nodeLabelsStr
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return cdb.Model(common.Host{Host: *h}).Updates(map[string]interface{}{"node_labels": nodeLabelsStr, "trigger_monitor_timestamp": time.Now()}).Error
}

func (m *Manager) UpdateNTP(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource, db *gorm.DB) error {
	bytes, err := json.Marshal(ntpSources)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal NTP sources for host %s", h.ID.String())
	}

	m.log.Infof("Updating ntp source of host %s to %s", h.ID, string(bytes))
	return db.Model(h).Update("ntp_sources", string(bytes)).Error
}

func (m *Manager) UpdateDomainNameResolution(ctx context.Context, h *models.Host, domainResolutionResponse models.DomainResolutionResponse, db *gorm.DB) error {
	response, err := json.Marshal(domainResolutionResponse)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal domain name resolution for host %s", h.ID.String())
	}
	if db == nil {
		db = m.db
	}
	if string(response) != h.DomainNameResolutions {
		if err := db.Model(h).Update("domain_name_resolutions", string(response)).Error; err != nil {
			return errors.Wrapf(err, "failed to update api_domain_name_resolution to host %s", h.ID.String())
		}
	}
	return nil
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

		var eventInfo string
		if newImageStatus.SizeBytes > 0 {
			eventInfo += fmt.Sprintf("time: %.2f seconds; size: %.2f Megabytes; download rate: %.2f MBps",
				newImageStatus.Time, newImageStatus.SizeBytes/math.Pow(1024, 2), newImageStatus.DownloadRate)
		}

		eventgen.SendImageStatusUpdatedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
			hostutil.GetHostnameForMsg(h), newImageStatus.Name, string(newImageStatus.Result), eventInfo)
	}
	marshalledStatuses, err := common.MarshalImageStatuses(hostImageStatuses)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal image statuses for host %s", h.ID.String())
	}

	return db.Model(h).Update("images_status", marshalledStatuses).Error
}

func (m *Manager) UpdateHostname(ctx context.Context, h *models.Host, hostname string, db *gorm.DB) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
		return common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Host is in %s state, host name can be set only in one of %s states",
				hostStatus, hostStatusesBeforeInstallation[:]))
	}

	h.RequestedHostname = hostname
	cdb := m.db
	if db != nil {
		cdb = db
	}
	return cdb.Model(common.Host{Host: *h}).Updates(map[string]interface{}{"requested_hostname": hostname, "trigger_monitor_timestamp": time.Now()}).Error
}

func (m *Manager) UpdateInstallationDisk(ctx context.Context, db *gorm.DB, h *models.Host, installationDiskPath string) error {
	hostStatus := swag.StringValue(h.Status)
	if !funk.ContainsString(hostStatusesBeforeInstallationOrUnbound[:], hostStatus) {
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
	return cdb.Model(common.Host{Host: *h}).Updates(map[string]interface{}{
		"installation_disk_path":    h.InstallationDiskPath,
		"installation_disk_id":      h.InstallationDiskID,
		"trigger_monitor_timestamp": time.Now(),
	}).Error
}

func (m *Manager) UpdateKubeKeyNS(ctx context.Context, hostID, namespace string) error {
	return m.db.Model(&common.Host{}).Where("id = ?", hostID).Update("kube_key_namespace", namespace).Error
}

func (m *Manager) CancelInstallation(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	shouldAddEvent := true
	isFailed := false
	var err error
	defer func() {
		if shouldAddEvent {
			if isFailed {
				eventgen.SendHostCancelInstallationFailedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h), err.Error())
			} else {
				eventgen.SendHostInstallationCancelledEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h))
			}
		}
	}()

	err = m.sm.Run(TransitionTypeCancelInstallation, newStateHost(h), &TransitionArgsCancelInstallation{
		ctx:    ctx,
		reason: reason,
		db:     db,
	})
	if err != nil {
		isFailed = true
		return common.NewApiError(http.StatusConflict, err)
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
	hostStage := h.Progress.CurrentStage
	if funk.Contains(manualRebootStages, hostStage) {
		m.log.Infof("Cluster %s Host %s is in stage %s and must be restarted by user to the live image "+
			"in order to reset the installation.", h.ClusterID.String(), h.ID.String(), hostStage)
		return true
	}
	return false
}

func (m *Manager) ResetHost(ctx context.Context, h *models.Host, reason string, db *gorm.DB) *common.ApiErrorResponse {
	shouldAddEvent := true
	isFailed := false
	var err error
	defer func() {
		if shouldAddEvent {
			if isFailed {
				eventgen.SendHostInstallationResetFailedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h), err.Error())
			} else {
				eventgen.SendHostInstallationResetEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h))
			}
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

	if err = m.sm.Run(transitionType, newStateHost(h), transitionArgs); err != nil {
		isFailed = true
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (m *Manager) ResetPendingUserAction(ctx context.Context, h *models.Host, db *gorm.DB) error {
	shouldAddEvent := true
	isFailed := false
	var err error
	defer func() {
		if shouldAddEvent {
			if isFailed {
				eventgen.SendHostSetStatusFailedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h), err.Error())
			} else {
				eventgen.SendUserRequiredCompleteInstallationResetEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
					hostutil.GetHostnameForMsg(h))
			}
		}
	}()

	err = m.sm.Run(TransitionTypeResettingPendingUserAction, newStateHost(h), &TransitionResettingPendingUserAction{
		ctx: ctx,
		db:  db,
	})
	if err != nil {
		isFailed = true
		return err
	}
	return nil
}

func (m *Manager) GetStagesByRole(h *models.Host, isSNO bool) []models.HostStage {
	stages := FindMatchingStages(h.Role, h.Bootstrap, isSNO)

	// for day2 hosts, rebooting stage is considered as the last state as we don't have any way to follow up on it further.
	if swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost && len(stages) > 0 {
		rebootingIndex := m.IndexOfStage(models.HostStageRebooting, stages)
		stages = stages[:rebootingIndex+1]
	}
	return stages
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

	m.metricApi.ReportHostInstallationMetrics(ctx, cluster.OpenshiftVersion, *h.ClusterID, cluster.EmailDomain, boot, h, previousProgress, CurrentStage)
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
	log := logutil.FromContext(ctx, m.log)
	for vCategory, vRes := range newValidationRes {
		for _, v := range vRes {
			if currentStatus, ok := m.getValidationStatus(currentValidationRes, vCategory, v.ID); ok {
				if v.Status == ValidationFailure && currentStatus == ValidationSuccess {
					log.Errorf("Host %s: validation '%s' that used to succeed is now failing", hostutil.GetHostnameForMsg(h), v.ID)
					if vc.cluster != nil {
						m.metricApi.HostValidationChanged(vc.cluster.OpenshiftVersion, vc.cluster.EmailDomain, models.HostValidationID(v.ID))
					} else if vc.infraEnv != nil {
						m.metricApi.HostValidationChanged(vc.infraEnv.OpenshiftVersion, vc.infraEnv.EmailDomain, models.HostValidationID(v.ID))
					}
					eventgen.SendHostValidationFailedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
						hostutil.GetHostnameForMsg(h), v.ID.String())
				}
				if v.Status == ValidationSuccess && currentStatus == ValidationFailure {
					log.Infof("Host %s: validation '%s' is now fixed", hostutil.GetHostnameForMsg(h), v.ID)
					eventgen.SendHostValidationFixedEvent(ctx, m.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
						hostutil.GetHostnameForMsg(h), v.ID.String())
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
	return hostutil.UpdateHost(logutil.FromContext(ctx, m.log), db, h.InfraEnvID, *h.ID, *h.Status, "validations_info", string(b))
}

func (m *Manager) AutoAssignRole(ctx context.Context, h *models.Host, db *gorm.DB) (bool, error) {
	if h.Role == models.HostRoleAutoAssign {
		log := logutil.FromContext(ctx, m.log)
		// If role is auto-assigned calculate the suggested roles
		// to make sure the suggestion is fresh
		if err := m.RefreshRole(ctx, h, db); err != nil { //force refresh
			return false, err
		}

		//copy the suggested role into the role and update the host record
		log.Infof("suggested role %s for host %s cluster %s", h.SuggestedRole, h.ID.String(), h.ClusterID.String())
		if err := updateRole(m.log, h, h.SuggestedRole, h.SuggestedRole, db, string(models.HostRoleAutoAssign)); err != nil {
			log.WithError(err).Errorf("failed to update role %s for host %s cluster %s",
				h.SuggestedRole, h.ID.String(), h.ClusterID.String())
			return true, err
		}

		// update the host in memory with the recent database state
		return true, db.Model(&models.Host{}).
			Take(h, "id = ? and infra_env_id = ?", h.ID.String(), h.InfraEnvID.String()).Error
	}

	return false, nil
}

func (m *Manager) autoRoleSelection(ctx context.Context, host *models.Host, db *gorm.DB) (models.HostRole, error) {
	h := *host

	suggestedRole, err := m.selectRole(ctx, &h, db)
	return suggestedRole, err
}

// This function recommends a role for a given host based on these criteria:
// 1. if there are not enough masters and the host has enough capabilities to be
//    a master the function select it to be a master
// 2. if there are enough masters, or it is a day2 host, or it has not enough capabilities
//    to be a master the function select it to be a  worker
// 3. in case of missing inventory or an internal error the function returns auto-assign
func (m *Manager) selectRole(ctx context.Context, h *models.Host, db *gorm.DB) (models.HostRole, error) {
	var (
		autoSelectedRole = models.HostRoleAutoAssign
		log              = logutil.FromContext(ctx, m.log)
		err              error
		vc               *validationContext
	)

	if hostutil.IsDay2Host(h) {
		return models.HostRoleWorker, nil
	}

	if h.Inventory == "" {
		return autoSelectedRole, errors.Errorf("host %s from cluster %s don't have hardware info",
			h.ID.String(), h.ClusterID.String())
	}

	// count already existing masters or hosts with suggested role of master
	// since aggregated functions can not run within a FOR UPDATE transaction
	// we are now calculating the master count with SELECT query (Bug 2012570)
	var masters []string
	reply := db.Model(&models.Host{}).Where("cluster_id = ? and id != ? and (role = ? or suggested_role = ?)",
		h.ClusterID, h.ID, models.HostRoleMaster, models.HostRoleMaster).Pluck("id", &masters)

	if err = reply.Error; err != nil {
		log.WithError(err).Errorf("failed to count masters in cluster %s", h.ClusterID.String())
		return autoSelectedRole, err
	}

	if len(masters) < common.MinMasterHostsNeededForInstallation {
		h.Role = models.HostRoleMaster
		vc, err = newValidationContext(h, nil, nil, db, m.hwValidator)
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

	return models.HostRoleWorker, nil
}

func (m *Manager) IsValidMasterCandidate(h *models.Host, c *common.Cluster, db *gorm.DB, log logrus.FieldLogger) (bool, error) {
	if h.Role == models.HostRoleWorker {
		return false, nil
	}

	h.Role = models.HostRoleMaster

	vc, err := newValidationContext(h, c, nil, db, m.hwValidator)
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
	return conditions[HasCPUCoresForRole.String()] &&
		conditions[HasMemoryForRole.String()] &&
		conditions[AreLsoRequirementsSatisfied.String()] &&
		conditions[AreOdfRequirementsSatisfied.String()] &&
		conditions[AreCnvRequirementsSatisfied.String()]
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
	return db.Model(&models.Host{}).Where("cluster_id = ? and id = ?", host.ClusterID.String(), host.ID.String()).Updates(&updatedHost).Error
}

func (m *Manager) resetContainerImagesValidation(host *models.Host, db *gorm.DB) error {
	return db.Model(&models.Host{}).Where("cluster_id = ? and id = ?", host.ClusterID.String(), host.ID.String()).Updates(
		map[string]interface{}{
			"images_status": "",
		}).Error
}

func (m *Manager) ResetHostValidation(ctx context.Context, hostID, infraEnvID strfmt.UUID, validationID string, db *gorm.DB) error {
	if db == nil {
		db = m.db
	}
	h, err := common.GetHostFromDB(db, infraEnvID.String(), hostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, errors.Wrapf(err, "Host %s of cluster %s was not found", hostID.String(), infraEnvID.String()))
		}
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Unexpected error while getting host %s of cluster %s", hostID.String(), infraEnvID.String()))
	}

	log := logutil.FromContext(ctx, m.log)

	// Cluster ID could be potentially nil in case of V2 call:
	if h.ClusterID == nil {
		err = fmt.Errorf("host %s is not bound to any cluster, reset validation", hostID)
		log.WithError(err).Error()
		return common.NewApiError(http.StatusInternalServerError, err)
	}

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

func (m *Manager) GetHostByKubeKey(key types.NamespacedName) (*common.Host, error) {
	host, err := common.GetHostFromDBWhere(m.db, "id = ? and kube_key_namespace = ?", key.Name, key.Namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get host from DB: %+v", key)
	}
	return host, nil
}

func (m *Manager) UnRegisterHost(ctx context.Context, hostID, infraEnvID string) error {
	return common.DeleteHostFromDB(m.db, hostID, infraEnvID)
}
