package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"k8s.io/utils/pointer"
)

const (
	tooSmallDiskTemplate   = "Disk is too small (disk only has %s, but %s are required)"
	wrongDriveTypeTemplate = "Drive type is %s, it must be one of %s."
)

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
	GetHostInstallationPath(host *models.Host) string
	GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error)
	GetInfraEnvHostRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.ClusterHostRequirements, error)
	DiskIsEligible(ctx context.Context, disk *models.Disk, infraEnv *common.InfraEnv, cluster *common.Cluster, host *models.Host) ([]string, error)
	ListEligibleDisks(inventory *models.Inventory) []*models.Disk
	GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error)
	// GetPreflightHardwareRequirements provides hardware (host) requirements that can be calculated only using cluster information.
	// Returned information describe requirements coming from OCP and OLM operators.
	GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error)
	GetPreflightInfraEnvHardwareRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.PreflightHardwareRequirements, error)
}

func NewValidator(log logrus.FieldLogger, cfg ValidatorCfg, operatorsAPI operators.API) Validator {
	diskEligibilityMatchers := []*regexp.Regexp{
		compileDiskReasonTemplate(tooSmallDiskTemplate, ".*", ".*"),
		compileDiskReasonTemplate(wrongDriveTypeTemplate, ".*", ".*"),
	}
	return &validator{
		ValidatorCfg:            cfg,
		log:                     log,
		operatorsAPI:            operatorsAPI,
		diskEligibilityMatchers: diskEligibilityMatchers,
	}
}

type ValidatorCfg struct {
	MaximumAllowedTimeDiffMinutes int64                        `envconfig:"HW_VALIDATOR_MAX_TIME_DIFF_MINUTES" default:"4"`
	VersionedRequirements         VersionedRequirementsDecoder `envconfig:"HW_VALIDATOR_REQUIREMENTS" default:"[]"`
}

type validator struct {
	ValidatorCfg
	log                     logrus.FieldLogger
	operatorsAPI            operators.API
	diskEligibilityMatchers []*regexp.Regexp
}

// DiskEligibilityInitialized is used to detect inventories created by older versions of the agent/service
func DiskEligibilityInitialized(disk *models.Disk) bool {
	return disk.InstallationEligibility.Eligible || len(disk.InstallationEligibility.NotEligibleReasons) != 0
}

func (v *validator) GetHostInstallationPath(host *models.Host) string {
	return hostutil.GetHostInstallationPath(host)
}

func (v *validator) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	return v.ListEligibleDisks(&inventory), nil
}

func isNvme(name string) bool {
	return strings.HasPrefix(name, "nvme")
}

// DiskIsEligible checks if a disk is eligible for installation by testing
// it against a list of predicates. Returns all the reasons the disk
// was found to be not eligible, or an empty slice if it was found to
// be eligible
func (v *validator) DiskIsEligible(ctx context.Context, disk *models.Disk, infraEnv *common.InfraEnv, cluster *common.Cluster, host *models.Host) ([]string, error) {
	var requirements *models.ClusterHostRequirements
	var err error
	if cluster != nil {
		requirements, err = v.GetClusterHostRequirements(ctx, cluster, host)
	} else {
		requirements, err = v.GetInfraEnvHostRequirements(ctx, infraEnv)
	}
	if err != nil {
		return nil, err
	}
	// This method can be called on demand, so the disk may already have service non-eligibility reasons
	notEligibleReasons := v.purgeServiceReasons(disk.InstallationEligibility.NotEligibleReasons)

	minSizeBytes := conversions.GbToBytes(requirements.Total.DiskSizeGb)
	if disk.SizeBytes < minSizeBytes {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf(
				tooSmallDiskTemplate,
				humanize.Bytes(uint64(disk.SizeBytes)), humanize.Bytes(uint64(minSizeBytes))))
	}

	if allowedDriveTypes := []string{"HDD", "SSD"}; !funk.ContainsString(allowedDriveTypes, disk.DriveType) {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf(wrongDriveTypeTemplate, disk.DriveType, strings.Join(allowedDriveTypes, ", ")))
	}

	return notEligibleReasons, nil
}

func (v *validator) purgeServiceReasons(reasons []string) []string {
	var notEligibleReasons []string
	for _, reason := range reasons {
		var matches bool
		for _, matcher := range v.diskEligibilityMatchers {
			if matcher.MatchString(reason) {
				matches = true
				break
			}
		}
		if !matches {
			notEligibleReasons = append(notEligibleReasons, reason)
		}
	}
	return notEligibleReasons
}

func (v *validator) ListEligibleDisks(inventory *models.Inventory) []*models.Disk {
	eligibleDisks := funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
		return disk.InstallationEligibility.Eligible
	}).([]*models.Disk)

	// Sorting list by size increase
	sort.Slice(eligibleDisks, func(i, j int) bool {
		isNvme1 := isNvme(eligibleDisks[i].Name)
		isNvme2 := isNvme(eligibleDisks[j].Name)
		if isNvme1 != isNvme2 {
			return isNvme2
		}

		// HDD is before SSD
		switch v := strings.Compare(eligibleDisks[i].DriveType, eligibleDisks[j].DriveType); v {
		case 0:
			return eligibleDisks[i].SizeBytes < eligibleDisks[j].SizeBytes
		default:
			return v < 0
		}
	})

	return eligibleDisks
}

func (v *validator) GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetRequirementsBreakdownForHostInCluster(ctx, cluster, host)
	if err != nil {
		return nil, err
	}

	ocpRequirements, err := v.getOCPClusterHostRoleRequirementsForVersion(cluster, common.GetEffectiveRole(host))
	if err != nil {
		return nil, err
	}
	total := totalizeRequirements(ocpRequirements, operatorsRequirements)
	return &models.ClusterHostRequirements{
		HostID:    *host.ID,
		Ocp:       &ocpRequirements,
		Operators: operatorsRequirements,
		Total:     &total,
	}, nil
}

func (v *validator) GetInfraEnvHostRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.ClusterHostRequirements, error) {
	masterOcpRequirements, err := v.getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv, models.HostRoleMaster)
	if err != nil {
		return nil, err
	}
	workerOcpRequirements, err := v.getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv, models.HostRoleWorker)
	if err != nil {
		return nil, err
	}

	requirements := &workerOcpRequirements
	if workerOcpRequirements.DiskSizeGb > masterOcpRequirements.DiskSizeGb {
		requirements = &masterOcpRequirements
	}

	return &models.ClusterHostRequirements{
		HostID:    "",
		Ocp:       requirements,
		Operators: nil,
		Total:     requirements,
	}, nil
}

func isDiskEncryptionSetWithTpm(c *common.Cluster) bool {
	return c.DiskEncryption != nil &&
		swag.StringValue(c.DiskEncryption.EnableOn) != models.DiskEncryptionEnableOnNone &&
		swag.StringValue(c.DiskEncryption.Mode) == models.DiskEncryptionModeTpmv2
}

func (v *validator) GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetPreflightRequirementsBreakdownForCluster(ctx, cluster)
	if err != nil {
		return nil, err
	}
	ocpRequirements, err := v.getClusterPreflightOCPRequirements(cluster)
	if err != nil {
		return nil, err
	}
	if isDiskEncryptionSetWithTpm(cluster) {
		switch swag.StringValue(cluster.DiskEncryption.EnableOn) {
		case models.DiskEncryptionEnableOnAll:
			ocpRequirements.Master.Quantitative.TpmEnabledInBios = true
			ocpRequirements.Worker.Quantitative.TpmEnabledInBios = true
		case models.DiskEncryptionEnableOnMasters:
			ocpRequirements.Master.Quantitative.TpmEnabledInBios = true
		case models.DiskEncryptionEnableOnWorkers:
			ocpRequirements.Worker.Quantitative.TpmEnabledInBios = true
		default:
			return nil, fmt.Errorf("disk-encryption is enabled on non-valid role: %s", swag.StringValue(cluster.DiskEncryption.EnableOn))
		}
	}

	return &models.PreflightHardwareRequirements{
		Operators: operatorsRequirements,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetPreflightInfraEnvHardwareRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.PreflightHardwareRequirements, error) {
	ocpRequirements, err := v.getInfraEnvPreflightOCPRequirements(infraEnv)
	if err != nil {
		return nil, err
	}

	return &models.PreflightHardwareRequirements{
		Operators: nil,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error) {
	ocpRequirements, err := v.getOCPClusterHostRoleRequirementsForVersion(cluster, common.GetEffectiveRole(host))
	if err != nil {
		return 0, err
	}
	return ocpRequirements.InstallationDiskSpeedThresholdMs, nil
}

func totalizeRequirements(ocpRequirements models.ClusterHostRequirementsDetails, operatorRequirements []*models.OperatorHostRequirements) models.ClusterHostRequirementsDetails {
	total := ocpRequirements

	for _, req := range operatorRequirements {
		details := req.Requirements
		total.RAMMib = total.RAMMib + details.RAMMib
		total.CPUCores = total.CPUCores + details.CPUCores
		total.DiskSizeGb = total.DiskSizeGb + details.DiskSizeGb

		if details.InstallationDiskSpeedThresholdMs > 0 {
			if total.InstallationDiskSpeedThresholdMs == 0 || details.InstallationDiskSpeedThresholdMs < total.InstallationDiskSpeedThresholdMs {
				total.InstallationDiskSpeedThresholdMs = details.InstallationDiskSpeedThresholdMs
			}
		}
		if details.NetworkLatencyThresholdMs != nil && *details.NetworkLatencyThresholdMs >= 0 {
			if total.NetworkLatencyThresholdMs == nil {
				total.NetworkLatencyThresholdMs = details.NetworkLatencyThresholdMs
			} else {
				total.NetworkLatencyThresholdMs = pointer.Float64Ptr(math.Min(*total.NetworkLatencyThresholdMs, *details.NetworkLatencyThresholdMs))
			}
		}
		if details.PacketLossPercentage != nil && *details.PacketLossPercentage >= 0 {
			if total.PacketLossPercentage == nil {
				total.PacketLossPercentage = details.PacketLossPercentage
			} else {
				total.PacketLossPercentage = pointer.Float64Ptr(math.Min(*total.PacketLossPercentage, *details.PacketLossPercentage))
			}
		}
	}
	return total
}

func (v *validator) getOCPClusterHostRoleRequirementsForVersion(cluster *common.Cluster, role models.HostRole) (models.ClusterHostRequirementsDetails, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster.OpenshiftVersion)
	if err != nil {
		return models.ClusterHostRequirementsDetails{}, err
	}

	if role == models.HostRoleMaster {
		if common.IsSingleNodeCluster(cluster) {
			return *requirements.SNORequirements, nil
		}
		return *requirements.MasterRequirements, nil
	}
	return *requirements.WorkerRequirements, nil
}

func (v *validator) getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv *common.InfraEnv, role models.HostRole) (models.ClusterHostRequirementsDetails, error) {
	requirements, err := v.getOCPRequirementsForVersion(infraEnv.OpenshiftVersion)
	if err != nil {
		return models.ClusterHostRequirementsDetails{}, err
	}

	if role == models.HostRoleMaster {
		return *requirements.MasterRequirements, nil
	}
	if role == models.HostRoleWorker || role == models.HostRoleAutoAssign {
		return *requirements.WorkerRequirements, nil
	}
	return models.ClusterHostRequirementsDetails{}, fmt.Errorf("Invalid role for host %s", role)
}

func (v *validator) getClusterPreflightOCPRequirements(cluster *common.Cluster) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster.OpenshiftVersion)
	if err != nil {
		return nil, err
	}
	return &models.HostTypeHardwareRequirementsWrapper{
		Master: &models.HostTypeHardwareRequirements{
			Quantitative: v.getMasterRequirements(cluster, requirements),
		},
		Worker: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.WorkerRequirements,
		},
	}, nil
}

func (v *validator) getInfraEnvPreflightOCPRequirements(infraEnv *common.InfraEnv) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements, err := v.getOCPRequirementsForVersion(infraEnv.OpenshiftVersion)
	if err != nil {
		return nil, err
	}
	return &models.HostTypeHardwareRequirementsWrapper{
		Master: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.MasterRequirements,
		},
		Worker: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.WorkerRequirements,
		},
	}, nil
}

func (v *validator) getMasterRequirements(cluster *common.Cluster, requirements *models.VersionedHostRequirements) *models.ClusterHostRequirementsDetails {
	if common.IsSingleNodeCluster(cluster) {
		return requirements.SNORequirements
	}
	return requirements.MasterRequirements
}

func (v *validator) getOCPRequirementsForVersion(openshiftVersion string) (*models.VersionedHostRequirements, error) {
	return v.VersionedRequirements.GetVersionedHostRequirements(openshiftVersion)
}

func compileDiskReasonTemplate(template string, wildcards ...interface{}) *regexp.Regexp {
	tmp, err := regexp.Compile(fmt.Sprintf(regexp.QuoteMeta(template), wildcards...))
	if err != nil {
		panic(err)
	}
	return tmp
}
