package odf

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/kelseyhightower/envconfig"
	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

const defaultStorageClassName = "ocs-storagecluster-cephfs"

var OcsOperator = models.MonitoredOperator{
	Name:             "ocs",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "ocs-operator",
	TimeoutSeconds:   30 * 60,
}

// NewOcsOperator creates new ODFOperator
func NewOcsOperator(log logrus.FieldLogger) *operator {
	return &operator{
		log: log,
	}
}

// operator is an ODF OLM operator plugin; it implements api.Operator
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var Operator = models.MonitoredOperator{
	Name:             "odf",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "odf-operator",
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleOpenShiftAI.ID,
	},
}

// NewOdfOperator creates new ODFOperator
func NewOdfOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newOdfOperatorWithConfig(log, &cfg)
}

// newOdfOperatorWithConfig creates new ODFOperator with given configuration
func newOdfOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "OpenShift Data Foundation"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return []string{lso.Operator.Name}, nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOdfRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOdfRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	status, message := o.validateRequirements(&cluster.Cluster)

	return api.ValidationResult{Status: status, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
}

func (o *operator) StorageClassName() string {
	return defaultStorageClassName
}

func (o *operator) getMinDiskSizeGB(additionalDiskRequirementsGb int64) int64 {
	return o.config.ODFMinDiskSizeGB + additionalDiskRequirementsGb
}

// Get valid disk count, and return error if no disk available
func (o *operator) getValidDiskCount(
	disks []*models.Disk,
	installationDiskID string,
	additionalOperatorRequirements *models.ClusterHostRequirementsDetails,
	mode odfDeploymentMode,
) (int64, error) {
	minDiskSizeGb := int64(0)
	if additionalOperatorRequirements != nil {
		minDiskSizeGb = additionalOperatorRequirements.DiskSizeGb
	}
	eligibleDisks, availableDisks := operatorscommon.NonInstallationDiskCount(disks, installationDiskID, o.getMinDiskSizeGB(minDiskSizeGb))
	var err error
	if eligibleDisks == 0 && availableDisks > 0 {
		err = fmt.Errorf("Insufficient resources to deploy ODF in %s mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of %d GB minimum and an installation disk.", strings.ToLower(string(mode)), o.getMinDiskSizeGB(minDiskSizeGb))
	}
	return eligibleDisks, err
}

// ValidateHost verifies whether this operator is valid for given host
func (o *operator) ValidateHost(_ context.Context, cluster *common.Cluster, host *models.Host, additionalOperatorRequirements *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	mode := getODFDeploymentMode(&cluster.Cluster, o.config.ODFNumMinimumHosts)
	shouldHostRunODF, err := shouldHostRunODF(&cluster.Cluster, mode, host.Role)

	log := o.log
	if cluster.ID != nil {
		log = log.WithField("cluster", cluster.ID.String())
	}

	if host.ID != nil {
		log = log.WithField("host", host.ID.String())
	}

	log.Debugf("ODF validate host - host role: %s", host.Role)
	log.Debugf("ODF validate host - mode: %s", string(mode))

	// Host role is auto-assign in standard mode.
	if err != nil {
		status := fmt.Sprintf("%s.", capitalizeFirstLetter(err.Error()))
		return api.ValidationResult{
			Status:       api.Pending,
			ValidationId: o.GetHostValidationID(),
			Reasons:      []string{status}}, nil
	}

	// No ODF requirement to validate in the host in this case.
	if shouldHostRunODF == nil || !*shouldHostRunODF {
		return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
	}

	if host.Inventory == "" {
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{"Missing Inventory in the host."}}, nil
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		message := "Failed to get inventory from host."
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, err
	}

	// getValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size.
	diskCount, err := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID, additionalOperatorRequirements, mode)
	if err != nil {
		return api.ValidationResult{
			Status:       api.Failure,
			ValidationId: o.GetHostValidationID(),
			Reasons:      []string{err.Error()}}, nil
	}

	// validate eligible extra disk.
	if diskCount == 0 {
		return api.ValidationResult{
			Status:       api.Failure,
			ValidationId: o.GetHostValidationID(),
			Reasons: []string{
				fmt.Sprintf(
					"Insufficient disks, ODF requires at least one non-installation SSD or HDD disk on each host in %s mode.",
					strings.ToLower(string(mode)),
				),
			}}, nil
	}

	return api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
		Reasons:      []string{}}, nil
}

// GenerateManifests generates manifests for the operator
// We recalculate the resources for all nodes because the computation that takes place during
// odf validations may be performed by a different replica
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
	mode := getODFDeploymentMode(&cluster.Cluster, o.config.ODFNumMinimumHosts)

	odfClusterResources := odfClusterResourcesInfo{}
	_, err := o.computeResourcesAllNodes(&cluster.Cluster, &odfClusterResources, mode)
	if err != nil {
		return nil, nil, err
	}

	o.log.Info("No. of ODF eligible disks in cluster ", cluster.ID, " are ", odfClusterResources.numberOfDisks)
	return Manifests(mode, odfClusterResources.numberOfDisks, cluster.OpenshiftVersion)
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the ODF Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(_ context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	mode := getODFDeploymentMode(&cluster.Cluster, o.config.ODFNumMinimumHosts)
	shouldHostRunODF, err := shouldHostRunODF(&cluster.Cluster, mode, host.Role)

	log := o.log
	if cluster.ID != nil {
		log = log.WithField("cluster", cluster.ID.String())
	}

	if host.ID != nil {
		log = log.WithField("host", host.ID.String())
	}

	log.Debugf("ODF validate host - host role: %s", host.Role)
	log.Debugf("ODF validate host - mode: %s", string(mode))

	// If the host is not going to run ODF workoads, we return 0 extra requirements for ODF.
	if err != nil || shouldHostRunODF == nil || !*shouldHostRunODF {
		return &models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0}, nil
	}

	var diskCount int64 = 0
	if host.Inventory != "" {
		inventory, err := common.UnmarshalInventory(host.Inventory)
		if err != nil {
			return nil, err
		}

		/* getValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size,
		we ignore the error as its treated as 500 in the UI */
		diskCount, _ = o.getValidDiskCount(inventory.Disks, host.InstallationDiskID, nil, mode)
	}

	if mode == compactMode {
		diskCount = max(1, diskCount)

		// Each ODF disk requires 2 CPUs and 5 GiB RAM
		return &models.ClusterHostRequirementsDetails{
			CPUCores: o.config.ODFPerHostCPUCompactMode + (diskCount * o.config.ODFPerDiskCPUCount),
			RAMMib:   conversions.GibToMib(o.config.ODFPerHostMemoryGiBCompactMode + (diskCount * o.config.ODFPerDiskRAMGiB)),
		}, nil
	}

	// worker in standard mode
	// Each ODF disk odf requires 2 CPUs and 5 GiB RAM
	return &models.ClusterHostRequirementsDetails{
		CPUCores: o.config.ODFPerHostCPUStandardMode + (diskCount * o.config.ODFPerDiskCPUCount),
		RAMMib:   conversions.GibToMib(o.config.ODFPerHostMemoryGiBStandardMode + (diskCount * o.config.ODFPerDiskRAMGiB)),
	}, nil

}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.ODFPerHostCPUCompactMode,
					RAMMib:   conversions.GibToMib(o.config.ODFPerHostMemoryGiBCompactMode),
				},
				Qualitative: []string{
					"Requirements apply only for master-only clusters",
					"At least 3 hosts",
					"At least 1 non-boot SSD or HDD disk on 3 hosts",
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.ODFPerHostCPUStandardMode,
					RAMMib:   conversions.GibToMib(o.config.ODFPerHostMemoryGiBStandardMode),
				},
				Qualitative: []string{
					"Requirements apply only for clusters with workers",
					fmt.Sprintf("%v GiB of additional RAM for each non-boot disk", o.config.ODFPerDiskRAMGiB),
					fmt.Sprintf("%v additional CPUs for each non-boot disk", o.config.ODFPerDiskCPUCount),
					"At least 3 workers",
					"At least 1 non-boot SSD or HDD disk on 3 workers",
				},
			},
		},
	}, nil
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDODF
}

// GetBundleLabels returns the bundle labels for the LSO operator
func (l *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}

func capitalizeFirstLetter(s string) string {
	if len(s) == 0 {
		return s
	}

	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
