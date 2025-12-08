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
	"github.com/openshift/assisted-service/internal/operators/amdgpu"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/operators/openshiftai"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "sigs.k8s.io/yaml"
)

const (
	controllerManifestFile          = "custom_manifests.json"
	controllerManifestConfigMapFile = "olm_operator_manifests.yaml"
)

var storageOperatorsPriority = []string{odf.Operator.Name, lvm.Operator.Name}

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

type OperatorFeatureSupportID struct {
	OperatorName     string
	FeatureSupportID models.FeatureSupportLevelID
	Dependencies     []models.FeatureSupportLevelID
}

// operatorMetadata is a struct to marshal the metadata content into YAML for the OLM Operator ConfigMap.
type operatorMetadata struct {
	Namespace        string   `json:"namespace" yaml:"namespace"`
	SubscriptionName string   `json:"subscriptionName" yaml:"subscriptionName"`
	Manifests        []string `json:"manifests" yaml:"manifests"`
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
	// ListBundles returns the list of available bundles filtered by feature support
	ListBundles(filters *featuresupport.SupportLevelFilters, featureIDs []models.FeatureSupportLevelID) []*models.Bundle
	// GetBundle returns the Bundle object with operators based on feature IDs
	GetBundle(bundleID string, featureIDs []models.FeatureSupportLevelID) (*models.Bundle, error)
	// GetOperatorDependenciesFeatureID returns the list of dependencies
	GetOperatorDependenciesFeatureID() []OperatorFeatureSupportID
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

func (mgr *Manager) getStorageOperator(cluster *models.Cluster) (api.StorageOperator, error) {
	for _, operatorName := range storageOperatorsPriority {
		for _, operator := range cluster.MonitoredOperators {
			if operator.Name == operatorName {
				o := mgr.olmOperators[operatorName]
				if storageOperator, ok := o.(api.StorageOperator); ok {
					return storageOperator, nil
				}
				mgr.log.Errorf("defined storage operator %s does not implement StorageOperator interface", operatorName)
			}
		}
	}
	return nil, fmt.Errorf("no storage operator found")
}

func hasMCEAndStorage(operators []*models.MonitoredOperator) bool {
	return operatorscommon.HasOperator(operators, mce.Operator.Name) &&
		(operatorscommon.HasOperator(operators, lvm.Operator.Name) ||
			operatorscommon.HasOperator(operators, odf.Operator.Name))
}

// GenerateManifests generates manifests for all enabled operators.
// Returns map assigning manifest content to its desired file name
func (mgr *Manager) GenerateManifests(ctx context.Context, cluster *common.Cluster) error {
	var controllerManifests []Manifest
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
				err = mgr.createInstallManifests(ctx, cluster, k, v, models.ManifestFolderOpenshift)
				if err != nil {
					return err
				}
			}

			controllerManifests = append(controllerManifests, Manifest{Name: clusterOperator.Name, Content: base64.StdEncoding.EncodeToString(manifest)})
		}
	}

	if hasMCEAndStorage(cluster.Cluster.MonitoredOperators) {
		storageOperator, err := mgr.getStorageOperator(&cluster.Cluster)
		if err != nil {
			return err
		}
		agentServiceConfigYaml, err := mce.GetAgentServiceConfigWithPVCManifest(storageOperator.StorageClassName())
		if err != nil {
			return err
		}
		// Name is important: controller will wait until this operator is ready. Should set
		// same value as the available storage
		controllerManifests = append(controllerManifests, Manifest{Name: storageOperator.GetName(), Content: base64.StdEncoding.EncodeToString(agentServiceConfigYaml)})
	}

	if len(controllerManifests) > 0 {
		content, err := json.Marshal(controllerManifests)
		if err != nil {
			return err
		}
		if err = mgr.createControllerManifest(ctx, cluster, string(content)); err != nil {
			return err
		}
		// Create ConfigMap with custom manifests to allow retrieval from assisted-installer
		// if API cannot be reached
		err = mgr.createOLMOperatorsConfigMap(ctx, cluster, &controllerManifests)
		if err != nil {
			return err
		}
	}

	return nil
}

// createControllerManifest create a file called custom_manifests.json, which is later obtained by the
// assisted-installer-controller, which apply this manifest file after the OLM is deployed,
// so user can provide here even CRs provisioned by the OLM.
func (mgr *Manager) createControllerManifest(ctx context.Context, cluster *common.Cluster, content string) error {
	objectFileName := path.Join(string(*cluster.ID), controllerManifestFile)
	if err := mgr.objectHandler.Upload(ctx, []byte(content), objectFileName); err != nil {
		return errors.Errorf("Failed to upload custom manifests for cluster %s", cluster.ID)
	}
	return nil
}

// Create a ConfigMap containing the operator custom manifests and add it to install manifests.
// This is needed when the assisted-installer cannot access the API to retrieve the manifest file, e.g.
// for the Agent-Based Installer.
// The data section of the ConfigMap will contain the manifests and additional metadata from
// MonitoredOperators
//   <operator-name>.metadata.yaml: |
//     namespace: <operator-namespace>
//     subscriptionName: <operator-subscriptionName>
//     manifests:
//       - <operator-name>-01.yaml
//   <operator-name>-01.yaml: |
//     <content of manifest>
func (mgr *Manager) createOLMOperatorsConfigMap(ctx context.Context, cluster *common.Cluster, manifests *[]Manifest) error {
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "olm-operator-manifests",
			Namespace: "assisted-installer",
		},
		Data: make(map[string]string),
	}
	// Track per-operator file counts and filenames across all batches
	counts := make(map[string]int)
	filesByOp := make(map[string][]string)
	opDetailsCache := make(map[string]*models.MonitoredOperator)

	for _, manifest := range *manifests {
		op, err := mgr.GetOperatorByName(manifest.Name)
		if err != nil {
			mgr.log.Infof("Could not find operator %s in monitored operators", manifest.Name)
			continue
		}

		// Cache operator info
		opDetailsCache[op.Name] = op

		// Decode the base64 controller manifest back to YAML bytes.
		decoded, err := base64.StdEncoding.DecodeString(manifest.Content)
		if err != nil {
			return fmt.Errorf("could not base64-decode manifest for %s: %w", manifest.Name, err)
		}
		// Split the manifest content into individual YAML documents.
		rawManifests, err := common.GetMultipleYamls[map[string]interface{}](decoded)
		if err != nil {
			return fmt.Errorf("could not decode YAML for %s: %w", manifest.Name, err)
		}

		// Re-marshal each document to YAML and add to the ConfigMap data.
		for _, doc := range rawManifests {
			if len(doc) == 0 {
				continue
			}
			b, err := k8syaml.Marshal(doc)
			if err != nil {
				return fmt.Errorf("failed to marshal YAML doc for %s: %w", manifest.Name, err)
			}
			trimmedManifest := strings.TrimSpace(string(b))
			if trimmedManifest == "" {
				continue
			}
			// Store base64-encoded content for assisted-installer
			encodedManifest := base64.StdEncoding.EncodeToString([]byte(trimmedManifest))

			counts[op.Name]++
			fileName := fmt.Sprintf("%s-%02d.yaml", op.Name, counts[op.Name])
			configMap.Data[fileName] = encodedManifest
			filesByOp[op.Name] = append(filesByOp[op.Name], fileName)
		}
	}

	// Emit one metadata entry per operator with the full list of files
	for name, fileNames := range filesByOp {
		info := opDetailsCache[name]
		metadata := operatorMetadata{
			Namespace:        info.Namespace,
			SubscriptionName: info.SubscriptionName,
			Manifests:        fileNames,
		}
		metadataYAML, err := k8syaml.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata for operator %s: %w", name, err)
		}
		metadataKey := fmt.Sprintf("%s.metadata.yaml", name)
		configMap.Data[metadataKey] = string(metadataYAML)
	}

	contents, err := k8syaml.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal configMap to yaml: %w", err)
	}

	return mgr.createInstallManifests(ctx, cluster, controllerManifestConfigMapFile, contents, models.ManifestFolderOpenshift)
}

func (mgr *Manager) createInstallManifests(ctx context.Context, cluster *common.Cluster, filename string, content []byte, folder string) error {
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
		if operatorscommon.HasOperator(cluster.Cluster.MonitoredOperators, operator.GetName()) {
			return true
		}
	}
	return false
}

// ValidateHost validates host requirements
func (mgr *Manager) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]api.ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]api.ValidationResult, 0, len(mgr.olmOperators))
	additionalOperatorRequirements := &models.ClusterHostRequirementsDetails{}

	if hasMCEAndStorage(cluster.Cluster.MonitoredOperators) {
		additionalOperatorRequirements.DiskSizeGb = mce.GetMinDiskSizeGB(&cluster.Cluster)
	}

	// To track operators that are disabled or not present in the cluster configuration, but have to be present
	// in the validation results and marked as valid.
	pendingOperators := make(map[string]struct{})
	for k := range mgr.olmOperators {
		pendingOperators[k] = struct{}{}
	}

	for _, clusterOperator := range cluster.MonitoredOperators {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if clusterOperator.OperatorType != models.OperatorTypeOlm {
			continue
		}

		operator := mgr.olmOperators[clusterOperator.Name]
		if operator != nil {
			result, err := operator.ValidateHost(ctx, cluster, host, additionalOperatorRequirements)
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
			results = append(results, result...)
		}
	}
	// Add successful validation result for disabled operators
	for opName := range pendingOperators {
		operator := mgr.olmOperators[opName]
		for _, validationID := range operator.GetClusterValidationIDs() {
			result := api.ValidationResult{
				Status:       api.Success,
				ValidationId: validationID,
				Reasons: []string{
					fmt.Sprintf("%s is disabled", opName),
				},
			}
			results = append(results, result)
		}
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
	ret := make([]*models.MonitoredOperator, 0)
	alreadyPresent := make([]string, 0)
	currentDependencies := make(map[string]*models.MonitoredOperator)

	// Compute list of operator without dependencies (they might be not required anymore)
	for _, operator := range operators {
		if operator.DependencyOnly {
			// Keep the current dependency definition to be sure, properties and others fields are consistent
			currentDependencies[operator.Name] = operator

			continue
		}

		ret = append(ret, operator)
		alreadyPresent = append(alreadyPresent, operator.Name)
	}

	// Get dependent operators
	allDependentOperators, err := mgr.getDependencies(cluster, ret)
	if err != nil {
		return nil, err
	}

	for operatorName := range allDependentOperators {
		if funk.Contains(alreadyPresent, operatorName) {
			continue
		}

		operator, err := mgr.getDependency(operatorName, currentDependencies)
		if err != nil {
			return nil, err
		}

		operator.DependencyOnly = true

		ret = append(ret, operator)
		alreadyPresent = append(alreadyPresent, operatorName)
	}

	// If openshift-ai is included, mark nvidia-gpu & amd-gpu as dependency only
	if operatorscommon.HasOperator(ret, openshiftai.Operator.Name) {
		for _, operator := range ret {
			if operator.Name == nvidiagpu.Operator.Name || operator.Name == amdgpu.Operator.Name {
				operator.DependencyOnly = true
			}
		}
	}

	return ret, nil
}

func (mgr *Manager) getDependency(name string, definitions map[string]*models.MonitoredOperator) (*models.MonitoredOperator, error) {
	if ret, ok := definitions[name]; ok {
		return ret, nil
	}

	return mgr.GetOperatorByName(name)
}

func (mgr *Manager) getDependencies(cluster *common.Cluster, operators []*models.MonitoredOperator) (map[string]bool, error) {
	fifo := list.New()
	visited := make(map[string]bool)
	for _, op := range operators {
		if op.OperatorType != models.OperatorTypeOlm {
			continue
		}
		mgr.log.Debugf("Attempting to resolve %s operator dependencies", op.Name)
		deps, err := mgr.olmOperators[op.Name].GetDependencies(cluster)
		if err != nil {
			return map[string]bool{}, err
		}
		visited[op.Name] = true
		mgr.log.Debugf("Dependencies found for %s operator: %+v ", op.Name, deps)
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

func (mgr *Manager) GetMonitoredOperatorsList() map[string]*models.MonitoredOperator {
	return mgr.monitoredOperators
}

func (mgr *Manager) GetOperatorByName(operatorName string) (*models.MonitoredOperator, error) {
	operator, ok := mgr.monitoredOperators[operatorName]
	if !ok {
		return nil, fmt.Errorf("operator %s isn't supported", operatorName)
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
	var cnvEnabled, lvmEnabled bool

	for _, updatedOperator := range operators {
		if updatedOperator.Name == "lvm" {
			lvmEnabled = true
		}

		if updatedOperator.Name == "cnv" {
			cnvEnabled = true
		}
	}

	if !lvmEnabled || !cnvEnabled {
		return nil
	}

	if isGreaterOrEqual, _ := common.BaseVersionGreaterOrEqual(lvm.LvmMinMultiNodeSupportVersion, openshiftVersion); isGreaterOrEqual {
		return nil
	}
	// Openshift version greater or Equal to 4.12.0 support cnv and lvms
	if isGreaterOrEqual, _ := common.BaseVersionGreaterOrEqual(lvm.LvmsMinOpenshiftVersion4_12, openshiftVersion); !isGreaterOrEqual {
		return errors.New("Currently, you can not install Logical Volume Manager operator at the same time as Virtualization operator.")
	}

	// before 4.15 multi node LVM not supported
	if !common.IsSingleNodeCluster(cluster) {
		message := fmt.Sprintf("Logical Volume Manager is only supported for highly available openshift with version %s or above", lvm.LvmMinMultiNodeSupportVersion)
		return errors.New(message)
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

// ListBundles returns a list of available bundles filtered by feature support.
func (mgr *Manager) ListBundles(filters *featuresupport.SupportLevelFilters, featureIDs []models.FeatureSupportLevelID) []*models.Bundle {
	var ret []*models.Bundle

	for _, basicBundleDetails := range operatorscommon.Bundles {
		// Get the bundle with operators based on feature IDs
		completeBundleDetails, err := mgr.GetBundle(basicBundleDetails.ID, featureIDs)
		if err != nil {
			mgr.log.Error(err)
			continue
		}

		// Check if all operators in the bundle are supported using featuresupport API
		if mgr.isBundleSupported(completeBundleDetails, filters) {
			ret = append(ret, completeBundleDetails)
		}
	}

	return ret
}

// GetBundle returns the Bundle object with operators based on feature IDs
func (mgr *Manager) GetBundle(bundleID string, featureIDs []models.FeatureSupportLevelID) (*models.Bundle, error) {
	bundle, ok := mgr.lookupBundle(bundleID)
	if !ok {
		return nil, fmt.Errorf("bundle '%s' is not supported", bundleID)
	}

	// Get all operators for the bundle based on feature IDs
	for _, operator := range mgr.olmOperators {
		operatorBundles := operator.GetBundleLabels(featureIDs)
		for _, operatorBundle := range operatorBundles {
			if operatorBundle == bundleID {
				operatorName := operator.GetName()
				bundle.Operators = append(bundle.Operators, operatorName)
				break
			}
		}
	}

	return bundle, nil
}

// lookupBundle tries to find a bundle with the given identifier. Returns a pointer to the basic information of the
// bundle and a boolean flag indicating if it was found. Note that the result does not contain the list of operators
// that are part of the bundle.
func (mgr *Manager) lookupBundle(bundleID string) (result *models.Bundle, ok bool) {
	for _, bundle := range operatorscommon.Bundles {
		if bundle.ID == bundleID {
			result = new(models.Bundle)
			*result = *bundle
			ok = true
			return
		}
	}

	return
}

func (mgr *Manager) GetOperatorDependenciesFeatureID() []OperatorFeatureSupportID {
	ret := make([]OperatorFeatureSupportID, 0)

	for _, operator := range mgr.olmOperators {
		ret = append(ret, OperatorFeatureSupportID{
			OperatorName:     operator.GetName(),
			FeatureSupportID: operator.GetFeatureSupportID(),
			Dependencies:     operator.GetDependenciesFeatureSupportID(),
		})
	}

	return ret
}

// isBundleSupported checks if all operators in a bundle are supported using featuresupport API
func (mgr *Manager) isBundleSupported(bundle *models.Bundle, filters *featuresupport.SupportLevelFilters) bool {
	// If bundle has no operators, it's not supported
	if len(bundle.Operators) == 0 {
		return false
	}

	// If there is no filter, it's always supported
	if filters == nil {
		return true
	}

	// Check each operator in the bundle using featuresupport API
	for _, operatorName := range bundle.Operators {
		operatorFeatureSupportID, err := mgr.getOperatorFeatureSupportID(operatorName)
		if err != nil {
			mgr.log.WithError(err).Warnf("Operator %s has no feature support ID", operatorName)

			return false
		}

		if !mgr.isOperatorSupported(operatorFeatureSupportID, *filters) {
			return false
		}
	}

	return true
}

// getOperatorFeatureSupportID gets the feature support ID for an operator
func (mgr *Manager) getOperatorFeatureSupportID(operatorName string) (models.FeatureSupportLevelID, error) {
	operator, ok := mgr.olmOperators[operatorName]
	if !ok {
		return "", fmt.Errorf("operator %s not found", operatorName)
	}

	return operator.GetFeatureSupportID(), nil
}

// isOperatorSupported checks if an operator is supported using featuresupport API
func (mgr *Manager) isOperatorSupported(featureID models.FeatureSupportLevelID, filters featuresupport.SupportLevelFilters) bool {
	supportLevel := featuresupport.GetSupportLevel(featureID, filters)

	// Consider the operator supported if it's not unavailable or unsupported
	return supportLevel != models.SupportLevelUnavailable && supportLevel != models.SupportLevelUnsupported
}
