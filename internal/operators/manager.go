package operators

import (
	"container/list"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const customManifestFile = "custom_manifests.json"
const minimalOpenShiftVersionForLVMAndCNV = "4.12.0"

// Manifest store the operator manifest used by assisted-installer to create CRs of the OLM.
type Manifest struct {
	// Name of the operator the CR manifest we want create
	Name string
	// Content of the manifest of the opreator
	Content string
}

// Manager is responsible for performing operations against additional operators
type Manager struct {
	log                logrus.FieldLogger
	olmOperators       map[string]api.Operator
	monitoredOperators map[string]*models.MonitoredOperator
	manifestsAPI       manifestsapi.ManifestsAPI
	objectHandler      s3wrapper.API
}

// API defines Operator management operation
//
//go:generate mockgen --build_flags=--mod=mod -package=operators -destination=mock_operators_api.go . API
type API interface {
	// ValidateCluster validates cluster requirements
	ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error)
	// ValidateHost validates host requirements
	ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]api.ValidationResult, error)
	// GenerateManifests generates manifests for all enabled operators.
	// Returns map assigning manifest content to its desired file name
	GenerateManifests(ctx context.Context, cluster *common.Cluster) error
	// AnyOLMOperatorEnabled checks whether any OLM operator has been enabled for the given cluster
	AnyOLMOperatorEnabled(cluster *common.Cluster) bool
	// ResolveDependencies amends the list of requested additional operators with any missing dependencies
	ResolveDependencies(cluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error)
	// GetMonitoredOperatorsList returns the monitored operators available by the manager.
	GetMonitoredOperatorsList() map[string]*models.MonitoredOperator
	// GetOperatorByName the manager's supported operator object by name.
	GetOperatorByName(operatorName string) (*models.MonitoredOperator, error)
	// GetSupportedOperatorsByType returns the manager's supported operator objects by type.
	GetSupportedOperatorsByType(operatorType models.OperatorType) []*models.MonitoredOperator
	// GetSupportedOperators returns a list of OLM operators that are supported
	GetSupportedOperators() []string
	// GetOperatorProperties provides description of properties of an operator
	GetOperatorProperties(operatorName string) (models.OperatorProperties, error)
	// GetRequirementsBreakdownForHostInCluster provides host requirements breakdown for each OLM operator in the cluster
	GetRequirementsBreakdownForHostInCluster(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]*models.OperatorHostRequirements, error)
	// GetPreflightRequirementsBreakdownForCluster provides host requirements breakdown for each supported OLM operator
	GetPreflightRequirementsBreakdownForCluster(ctx context.Context, cluster *common.Cluster) ([]*models.OperatorHardwareRequirements, error)
	// EnsureOperatorPrerequisite Ensure that for the given operators has the base prerequisite for installation
	EnsureOperatorPrerequisite(cluster *common.Cluster, openshiftVersion string, cpuArchitecture string, operators []*models.MonitoredOperator) error
}

// GetPreflightRequirementsBreakdownForCluster provides host requirements breakdown for each supported OLM operator
func (mgr Manager) GetPreflightRequirementsBreakdownForCluster(ctx context.Context, cluster *common.Cluster) ([]*models.OperatorHardwareRequirements, error) {
	logger := logutil.FromContext(ctx, mgr.log)
	var requirements []*models.OperatorHardwareRequirements
	if common.IsDay2Cluster(cluster) {
		return requirements, nil
	}
	for operatorName, operator := range mgr.olmOperators {
		reqs, err := operator.GetPreflightRequirements(ctx, cluster)
		if err != nil {
			logger.WithError(err).Errorf("Cannot get preflight requirements for %s operator", operatorName)
			return nil, err
		}
		requirements = append(requirements, reqs)
	}
	return requirements, nil
}

// GetRequirementsBreakdownForRoleInCluster provides host requirements breakdown for each OLM operator in the cluster
func (mgr *Manager) GetRequirementsBreakdownForHostInCluster(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]*models.OperatorHostRequirements, error) {
	logger := logutil.FromContext(ctx, mgr.log)
	var requirements []*models.OperatorHostRequirements
	if common.IsDay2Cluster(cluster) {
		return requirements, nil
	}
	for _, monitoredOperator := range cluster.MonitoredOperators {
		operatorName := monitoredOperator.Name
		operator := mgr.olmOperators[operatorName]
		if operator != nil {
			reqs, err := operator.GetHostRequirements(ctx, cluster, host)
			if err != nil {
				logger.WithError(err).Errorf("Cannot get host requirements for %s operator", operatorName)
				return nil, err
			}
			opHostRequirements := models.OperatorHostRequirements{
				OperatorName: operatorName,
				Requirements: reqs,
			}
			requirements = append(requirements, &opHostRequirements)
		}
	}
	return requirements, nil
}

// GenerateManifests generates manifests for all enabled operators.
// Returns map assigning manifest content to its desired file name
func (mgr *Manager) GenerateManifests(ctx context.Context, cluster *common.Cluster) error {
	var customManifests []Manifest
	// Generate manifests for all the generic operators
	for _, clusterOperator := range cluster.MonitoredOperators {
		if clusterOperator.OperatorType != models.OperatorTypeOlm {
			continue
		}

		operator := mgr.olmOperators[clusterOperator.Name]
		if operator != nil {
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)
			if err != nil {
				mgr.log.Error(fmt.Sprintf("Cannot generate %s manifests due to ", clusterOperator.Name), err)
				return err
			}
			for k, v := range openshiftManifests {
				err = mgr.createManifests(ctx, cluster, k, v, models.ManifestFolderOpenshift)
				if err != nil {
					return err
				}
			}

			customManifests = append(customManifests, Manifest{Name: clusterOperator.Name, Content: base64.StdEncoding.EncodeToString(manifest)})
		}
	}

	if len(customManifests) > 0 {
		content, err := json.Marshal(customManifests)
		if err != nil {
			return err
		}
		if err := mgr.createCustomManifest(ctx, cluster, string(content)); err != nil {
			return err
		}
	}

	return nil
}

// createCustomManifest create a file called custom_manifests.json, which is later obtained by the
// assisted-installer-controller, which apply this manifest file after the OLM is deployed,
// so user can provide here even CRs provisioned by the OLM.
func (mgr *Manager) createCustomManifest(ctx context.Context, cluster *common.Cluster, content string) error {
	objectFileName := path.Join(string(*cluster.ID), customManifestFile)
	if err := mgr.objectHandler.Upload(ctx, []byte(content), objectFileName); err != nil {
		return errors.Errorf("Failed to upload custom manifests for cluster %s", cluster.ID)
	}
	return nil
}

func (mgr *Manager) createManifests(ctx context.Context, cluster *common.Cluster, filename string, content []byte, folder string) error {
	// all relevant logs of creating manifest will be inside CreateClusterManifest
	_, err := mgr.manifestsAPI.CreateClusterManifestInternal(ctx, operations.V2CreateClusterManifestParams{
		ClusterID: *cluster.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(content)),
			FileName: &filename,
			Folder:   swag.String(folder),
		},
	}, false)

	if err != nil {
		return errors.Wrapf(err, "Failed to create manifest %s for cluster %s", filename, cluster.ID)
	}
	return nil
}

// AnyOLMOperatorEnabled checks whether any OLM operator has been enabled for the given cluster
func (mgr *Manager) AnyOLMOperatorEnabled(cluster *common.Cluster) bool {
	for _, operator := range mgr.olmOperators {
		if IsEnabled(cluster.MonitoredOperators, operator.GetName()) {
			return true
		}
	}
	return false
}

// ValidateHost validates host requirements
func (mgr *Manager) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]api.ValidationResult, error) {
	results := make([]api.ValidationResult, 0, len(mgr.olmOperators))

	// To track operators that are disabled or not present in the cluster configuration, but have to be present
	// in the validation results and marked as valid.
	pendingOperators := make(map[string]struct{})
	for k := range mgr.olmOperators {
		pendingOperators[k] = struct{}{}
	}

	for _, clusterOperator := range cluster.MonitoredOperators {
		if clusterOperator.OperatorType != models.OperatorTypeOlm {
			continue
		}

		operator := mgr.olmOperators[clusterOperator.Name]
		if operator != nil {
			result, err := operator.ValidateHost(ctx, cluster, host)
			if err != nil {
				return nil, err
			}
			delete(pendingOperators, clusterOperator.Name)
			results = append(results, result)
		}
	}
	// Add successful validation result for disabled operators
	for OpName := range pendingOperators {
		operator := mgr.olmOperators[OpName]
		result := api.ValidationResult{
			Status:       api.Success,
			ValidationId: operator.GetHostValidationID(),
			Reasons: []string{
				fmt.Sprintf("%s is disabled", OpName),
			},
		}
		results = append(results, result)
	}
	return results, nil
}

// ValidateCluster validates cluster requirements
func (mgr *Manager) ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	results := make([]api.ValidationResult, 0, len(mgr.olmOperators))

	pendingOperators := make(map[string]struct{})
	for k := range mgr.olmOperators {
		pendingOperators[k] = struct{}{}
	}

	for _, clusterOperator := range cluster.MonitoredOperators {
		if clusterOperator.OperatorType != models.OperatorTypeOlm {
			continue
		}

		operator := mgr.olmOperators[clusterOperator.Name]
		if operator != nil {
			result, err := operator.ValidateCluster(ctx, cluster)
			if err != nil {
				return nil, err
			}
			delete(pendingOperators, clusterOperator.Name)
			results = append(results, result)
		}
	}
	// Add successful validation result for disabled operators
	for opName := range pendingOperators {
		operator := mgr.olmOperators[opName]
		result := api.ValidationResult{
			Status:       api.Success,
			ValidationId: operator.GetClusterValidationID(),
			Reasons: []string{
				fmt.Sprintf("%s is disabled", opName),
			},
		}
		results = append(results, result)
	}
	return results, nil
}

// GetSupportedOperators returns a list of OLM operators that are supported
func (mgr *Manager) GetSupportedOperators() []string {
	keys := make([]string, 0, len(mgr.olmOperators))
	for k := range mgr.olmOperators {
		keys = append(keys, k)
	}
	return keys
}

// GetOperatorProperties provides description of properties of an operator
func (mgr *Manager) GetOperatorProperties(operatorName string) (models.OperatorProperties, error) {
	if operator, ok := mgr.olmOperators[operatorName]; ok {
		return operator.GetProperties(), nil
	}
	return nil, errors.Errorf("Operator %s not found", operatorName)
}

func (mgr *Manager) ResolveDependencies(cluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
	allDependentOperators, err := mgr.getDependencies(cluster, operators)
	if err != nil {
		return operators, nil
	}

	inputOperatorNames := make([]string, len(operators))
	for _, inputOperator := range operators {
		inputOperatorNames = append(inputOperatorNames, inputOperator.Name)
	}

	for operatorName := range allDependentOperators {
		if funk.Contains(inputOperatorNames, operatorName) {
			continue
		}

		operator, err := mgr.GetOperatorByName(operatorName)
		if err != nil {
			return nil, err
		}

		operators = append(operators, operator)
	}

	return operators, nil
}

func (mgr *Manager) getDependencies(cluster *common.Cluster, operators []*models.MonitoredOperator) (map[string]bool, error) {
	fifo := list.New()
	visited := make(map[string]bool)
	for _, op := range operators {
		if op.OperatorType != models.OperatorTypeOlm {
			continue
		}
		mgr.log.Infof("Attempting to resolve %s operator dependencies", op.Name)
		deps, err := mgr.olmOperators[op.Name].GetDependencies(cluster)
		if err != nil {
			return map[string]bool{}, err
		}
		visited[op.Name] = true
		mgr.log.Infof("Dependencies found for %s operator: %+v ", op.Name, deps)
		for _, dep := range deps {
			fifo.PushBack(dep)
		}
	}
	for fifo.Len() > 0 {
		first := fifo.Front()
		op := first.Value.(string)
		deps, err := mgr.olmOperators[op].GetDependencies(cluster)
		if err != nil {
			return map[string]bool{}, err
		}
		for _, dep := range deps {
			if !visited[dep] {
				fifo.PushBack(dep)
			}
		}
		visited[op] = true
		fifo.Remove(first)
	}

	return visited, nil
}

func findOperator(operators []*models.MonitoredOperator, operatorName string) *models.MonitoredOperator {
	for _, operator := range operators {
		if operator.Name == operatorName {
			return operator
		}
	}
	return nil
}

func IsEnabled(operators []*models.MonitoredOperator, operatorName string) bool {
	return findOperator(operators, operatorName) != nil
}

func (mgr *Manager) GetMonitoredOperatorsList() map[string]*models.MonitoredOperator {
	return mgr.monitoredOperators
}

func (mgr *Manager) GetOperatorByName(operatorName string) (*models.MonitoredOperator, error) {
	operator, ok := mgr.monitoredOperators[operatorName]
	if !ok {
		return nil, fmt.Errorf("Operator %s isn't supported", operatorName)
	}

	return &models.MonitoredOperator{
		Name:             operator.Name,
		OperatorType:     operator.OperatorType,
		TimeoutSeconds:   operator.TimeoutSeconds,
		Namespace:        operator.Namespace,
		SubscriptionName: operator.SubscriptionName,
	}, nil
}

func (mgr *Manager) GetSupportedOperatorsByType(operatorType models.OperatorType) []*models.MonitoredOperator {
	operators := make([]*models.MonitoredOperator, 0)

	for _, operator := range mgr.GetMonitoredOperatorsList() {
		if operator.OperatorType == operatorType {
			operator, _ = mgr.GetOperatorByName(operator.Name)
			operators = append(operators, operator)
		}
	}

	return operators
}

func isOperatorCompatibleWithArchitecture(cluster *common.Cluster, cpuArchitecture string, operator api.Operator) bool {
	featureId := operator.GetFeatureSupportID()
	return featuresupport.IsFeatureCompatibleWithArchitecture(featureId, cluster.OpenshiftVersion, cpuArchitecture)
}

func (mgr *Manager) EnsureOperatorArchCapability(cluster *common.Cluster, cpuArchitecture string, operators []*models.MonitoredOperator) error {
	var monitoredOperators []string
	var failedOperators []string

	for _, monitoredOperator := range operators {
		monitoredOperators = append(monitoredOperators, monitoredOperator.Name)
	}

	for operatorName, operator := range mgr.olmOperators {
		if !funk.ContainsString(monitoredOperators, operatorName) {
			continue
		}

		if !isOperatorCompatibleWithArchitecture(cluster, cpuArchitecture, operator) {
			failedOperators = append(failedOperators, operator.GetFullName())
		}
	}

	if failedOperators != nil {
		return errors.New(fmt.Sprintf("%s is not available when %s CPU architecture is selected", strings.Join(failedOperators, ", "), cluster.CPUArchitecture))
	}

	return nil
}

func EnsureLVMAndCNVDoNotClash(cluster *common.Cluster, openshiftVersion string, operators []*models.MonitoredOperator) error {
	// Openshift version greater or Equal to 4.12.0 support cnv and lvms
	if isGreaterOrEqual, _ := common.BaseVersionGreaterOrEqual(openshiftVersion, lvm.LvmsMinOpenshiftVersion); !isGreaterOrEqual {
		return nil
	}

	cnvEnabled := false
	lvmEnabled := false

	operatorsCanCoexist, err := common.BaseVersionGreaterOrEqual(minimalOpenShiftVersionForLVMAndCNV, openshiftVersion)
	if err != nil {
		return err
	}
	if operatorsCanCoexist {
		return nil
	}

	for _, updatedOperator := range operators {
		if updatedOperator.Name == "lvm" {
			lvmEnabled = true
		}

		if updatedOperator.Name == "cnv" {
			cnvEnabled = true
		}
	}

	if lvmEnabled && cnvEnabled {
		return errors.Errorf("Currently, you can not install OpenShift Data Foundation Logical Volume Manager operator at the same time as Virtualization operator.")
	}

	if lvmEnabled && !common.IsSingleNodeCluster(cluster) {
		return errors.Errorf("OpenShift Data Foundation Logical Volume Manager operator is only supported for Single Node Openshift")
	}
	return nil
}

func (mgr *Manager) EnsureOperatorPrerequisite(cluster *common.Cluster, openshiftVersion string, cpuArchitecture string, operators []*models.MonitoredOperator) error {
	err := EnsureLVMAndCNVDoNotClash(cluster, openshiftVersion, operators)
	if err != nil {
		return err
	}

	err = mgr.EnsureOperatorArchCapability(cluster, cpuArchitecture, operators)
	if err != nil {
		return err
	}

	return nil
}
