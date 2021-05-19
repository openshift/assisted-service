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
	DiskIsEligible(ctx context.Context, disk *models.Disk, cluster *common.Cluster, host *models.Host) ([]string, error)
	ListEligibleDisks(inventory *models.Inventory) []*models.Disk
	GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error)
	// GetPreflightHardwareRequirements provides hardware (host) requirements that can be calculated only using cluster information.
	// Returned information describe requirements coming from OCP and OLM operators.
	GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error)
	GetDefaultVersionRequirements(singleNode bool) (*models.VersionedHostRequirements, error)
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
	MinCPUCores                   int64                        `envconfig:"HW_VALIDATOR_MIN_CPU_CORES" default:"2"`
	MinCPUCoresSno                int64                        `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_SNO" default:"8"`
	MinRamGib                     int64                        `envconfig:"HW_VALIDATOR_MIN_RAM_GIB" default:"8"`
	MinRamGibSno                  int64                        `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_SNO" default:"32"`
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
func (v *validator) DiskIsEligible(ctx context.Context, disk *models.Disk, cluster *common.Cluster, host *models.Host) ([]string, error) {
	requirements, err := v.GetClusterHostRequirements(ctx, cluster, host)
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

	ocpRequirements, err := v.getOCPHostRoleRequirementsForVersion(cluster, host.Role)
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

func (v *validator) GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetPreflightRequirementsBreakdownForCluster(ctx, cluster)
	if err != nil {
		return nil, err
	}
	ocpRequirements, err := v.getPreflightOCPRequirements(cluster)
	if err != nil {
		return nil, err
	}

	return &models.PreflightHardwareRequirements{
		Operators: operatorsRequirements,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetDefaultVersionRequirements(singleNode bool) (*models.VersionedHostRequirements, error) {
	requirements, err := v.VersionedRequirements.GetVersionedHostRequirements(DefaultVersion)
	if err != nil {
		return nil, err
	}
	if singleNode {
		v.updateSingleNodeHwRequirements(requirements)
	}
	return requirements, nil
}

func (v *validator) GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error) {
	requirements, err := v.GetClusterHostRequirements(ctx, cluster, host)
	if err != nil {
		return 0, err
	}
	return requirements.Total.InstallationDiskSpeedThresholdMs, nil
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

func (v *validator) getOCPHostRoleRequirementsForVersion(cluster *common.Cluster, role models.HostRole) (models.ClusterHostRequirementsDetails, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster)
	if err != nil {
		return models.ClusterHostRequirementsDetails{}, err
	}
	if role == models.HostRoleMaster {
		return *requirements.MasterRequirements, nil
	}
	return *requirements.WorkerRequirements, nil
}

func (v *validator) getPreflightOCPRequirements(cluster *common.Cluster) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster)
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

func (v *validator) updateSingleNodeHwRequirements(requirements *models.VersionedHostRequirements) {
	requirements.MasterRequirements.CPUCores = v.ValidatorCfg.MinCPUCoresSno
	requirements.MasterRequirements.RAMMib = conversions.GibToMib(v.ValidatorCfg.MinRamGibSno)
}

func (v *validator) getOCPRequirementsForVersion(cluster *common.Cluster) (*models.VersionedHostRequirements, error) {
	requirements, err := v.VersionedRequirements.GetVersionedHostRequirements(cluster.OpenshiftVersion)
	if err != nil {
		return nil, err
	}

	if common.IsSingleNodeCluster(cluster) {
		v.updateSingleNodeHwRequirements(requirements)
	}

	return requirements, nil
}

func compileDiskReasonTemplate(template string, wildcards ...interface{}) *regexp.Regexp {
	tmp, err := regexp.Compile(fmt.Sprintf(regexp.QuoteMeta(template), wildcards...))
	if err != nil {
		panic(err)
	}
	return tmp
}
