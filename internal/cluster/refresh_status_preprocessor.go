package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dustin/go-humanize/english"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorcommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type ValidationResult struct {
	ID      ValidationID     `json:"id"`
	Status  ValidationStatus `json:"status"`
	Message string           `json:"message"`
}

type ValidationsStatus map[string][]ValidationResult

type stringer interface {
	String() string
}

type refreshPreprocessor struct {
	log           logrus.FieldLogger
	validations   []validation
	conditions    []condition
	operatorsAPI  operators.API
	usageAPI      usage.API
	eventsHandler eventsapi.Handler
}

func newRefreshPreprocessor(log logrus.FieldLogger, hostAPI host.API, operatorsAPI operators.API, usageAPI usage.API,
	eventsHandler eventsapi.Handler) *refreshPreprocessor {
	v := clusterValidator{
		log:     log,
		hostAPI: hostAPI,
	}

	return &refreshPreprocessor{
		log:           log,
		validations:   newValidations(&v),
		conditions:    newConditions(&v),
		operatorsAPI:  operatorsAPI,
		usageAPI:      usageAPI,
		eventsHandler: eventsHandler,
	}
}

func (r *refreshPreprocessor) preprocess(ctx context.Context, c *clusterPreprocessContext) (map[string]bool, map[string][]ValidationResult, error) {
	stateMachineInput := make(map[string]bool)
	validationsOutput := make(map[string][]ValidationResult)
	checkValidationsInStatuses := []string{
		models.ClusterStatusInsufficient, models.ClusterStatusReady, models.ClusterStatusPendingForInput, models.ClusterStatusPreparingForInstallation,
	}
	var ignoredValidations []string
	var err error
	if c.cluster != nil {
		ignoredValidations, err = common.DeserializeJSONList(c.cluster.IgnoredClusterValidations)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to deserialize ignored cluster validations for cluster %s: %w", c.cluster.ID.String(), err)
		}
	}

	//if the cluster is not on discovery stages - skip the validations check
	if !funk.ContainsString(checkValidationsInStatuses, swag.StringValue(c.cluster.Status)) {
		return stateMachineInput, validationsOutput, nil
	}
	for _, v := range r.validations {
		st, message := v.condition(c)
		stateMachineInput[v.id.String()] = st == ValidationSuccess
		var category string
		category, err = v.id.Category()
		if err != nil {
			r.log.WithError(err).Warn("id.category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], ValidationResult{
			ID:      v.id,
			Status:  st,
			Message: message,
		})
	}

	// Before validating the operators we need to recalculate the dependencies because changes in the hosts may
	// imply changes in the dependencies between operators. For example, if the OpenShift AI operator is enabled and
	// a new host with an NVIDIA GPU has been discovered, then the NVIDIA GPU operator will need to be added as a
	// dependency, and then we will need to validate that secure boot is disabled.
	err = r.recalculateOperatorDependencies(ctx, c)
	if err != nil {
		err = fmt.Errorf("failed to recalculate operator dependencies for cluster '%s': %w", c.clusterId, err)
		return nil, nil, err
	}

	// Validate operators
	results, err := r.operatorsAPI.ValidateCluster(ctx, c.cluster)
	if err != nil {
		return nil, nil, err
	}
	for _, result := range results {
		stateMachineInput[result.ValidationId] = result.Status == api.Success
		id := ValidationID(result.ValidationId)

		category, err := id.Category()
		if err != nil {
			r.log.WithError(err).Warn("id.category()")
			return nil, nil, err
		}

		status := ValidationStatus(result.Status)
		validationsOutput[category] = append(validationsOutput[category], ValidationResult{
			ID:      id,
			Status:  status,
			Message: strings.Join(result.Reasons, "\n"),
		})
	}

	for _, condition := range r.conditions {
		stateMachineInput[condition.id.String()] = condition.fn(c)
	}
	for _, validationResults := range validationsOutput {
		sortByValidationResultID(validationResults)
	}
	for _, currentResult := range validationsOutput {
		for _, v := range currentResult {
			if common.ShouldIgnoreValidation(ignoredValidations, string(v.ID), common.NonIgnorableClusterValidations) {
				// Set the condition to true to force the validation to pass.
				stateMachineInput[string(v.ID)] = true
			}
		}
	}
	return stateMachineInput, validationsOutput, nil
}

// recalculateOperatorDependencies calculates the operator dependencies and updates the database and the passed cluster
// accordingly.
func (r *refreshPreprocessor) recalculateOperatorDependencies(ctx context.Context, c *clusterPreprocessContext) error {
	// Calculate and save the operators that have been added, updated or deleted:
	operatorsBeforeResolve := c.cluster.MonitoredOperators

	operatorsAfterResolve, err := r.operatorsAPI.ResolveDependencies(c.cluster, c.cluster.MonitoredOperators)
	if err != nil {
		return fmt.Errorf("failed to resolve operator dependencies: %w", err)
	}

	var addedOperators, updatedOperators, deletedOperators []*models.MonitoredOperator

	for _, operatorAfterResolve := range operatorsAfterResolve {
		if operatorAfterResolve.ClusterID == "" {
			operatorAfterResolve.ClusterID = c.clusterId
		}

		operatorBeforeResolve := operatorcommon.GetOperator(operatorsBeforeResolve, operatorAfterResolve.Name)
		if operatorBeforeResolve != nil {
			if !reflect.DeepEqual(operatorAfterResolve, operatorBeforeResolve) {
				updatedOperators = append(updatedOperators, operatorAfterResolve)
			}
		} else {
			addedOperators = append(addedOperators, operatorAfterResolve)
		}
	}

	for _, operatorBeforeResolve := range operatorsBeforeResolve {
		if !operatorcommon.HasOperator(operatorsAfterResolve, operatorBeforeResolve.Name) {
			deletedOperators = append(deletedOperators, operatorBeforeResolve)
		}
	}

	// If nothing changed, nothing needs to be done
	if len(addedOperators) == 0 && len(deletedOperators) == 0 && len(updatedOperators) == 0 {
		return nil
	}

	// Validate with cluster CPU architecture
	err = r.operatorsAPI.EnsureOperatorPrerequisite(c.cluster, c.cluster.OpenshiftVersion, c.cluster.CPUArchitecture, operatorsAfterResolve)
	if err != nil {
		return fmt.Errorf("failed to validate operator prerequisite: %w", err)
	}

	c.cluster.MonitoredOperators = operatorsAfterResolve

	err = c.db.Transaction(func(tx *gorm.DB) error {
		for _, addedOperator := range addedOperators {
			err = tx.Save(addedOperator).Error
			if err != nil {
				return fmt.Errorf("failed to add operator '%s': %w", addedOperator.Name, err)
			}
		}

		for _, updatedOperator := range updatedOperators {
			err = tx.Save(updatedOperator).Error
			if err != nil {
				return fmt.Errorf("failed to update operator '%s': %w", updatedOperator.Name, err)
			}
		}

		for _, deletedOperator := range deletedOperators {
			err = tx.Delete(deletedOperator).Error
			if err != nil {
				return fmt.Errorf("failed to delete operator '%s': %w", deletedOperator.Name, err)
			}
		}

		// If any operator has been added or deleted then we need to update the corresponding feature usage
		if len(addedOperators) > 0 || len(deletedOperators) > 0 {
			err = r.recalculateOperatorFeatureUsage(c, tx, addedOperators, deletedOperators)
			if err != nil {
				return fmt.Errorf("failed to recalculate operator feature usage: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("transaction to update monitored operators, the associated usage and reset roles failed: %w", err)
	}

	// If everything went smoothly, notify about the change
	if len(addedOperators) > 0 || len(deletedOperators) > 0 {
		err = r.notifyOperatorFeatureUsageChange(ctx, c, addedOperators, deletedOperators)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *refreshPreprocessor) recalculateOperatorFeatureUsage(c *clusterPreprocessContext, db *gorm.DB,
	addedOperators, deletedOperators []*models.MonitoredOperator) error {
	if r.usageAPI == nil {
		return nil
	}

	usages, err := usage.Unmarshal(c.cluster.FeatureUsage)
	if err != nil {
		return fmt.Errorf("failed to read feature usage: %w", err)
	}

	for _, addedOperator := range addedOperators {
		featureName := strings.ToUpper(addedOperator.Name)
		r.usageAPI.Add(usages, featureName, nil)
	}

	for _, deletedOperator := range deletedOperators {
		featureName := strings.ToUpper(deletedOperator.Name)
		r.usageAPI.Remove(usages, featureName)
	}

	data, err := json.Marshal(usages)
	if err != nil {
		return fmt.Errorf("failed to write feature usage: %w", err)
	}

	c.cluster.FeatureUsage = string(data)
	r.usageAPI.Save(db, c.clusterId, usages)

	return nil
}

func (r refreshPreprocessor) notifyOperatorFeatureUsageChange(ctx context.Context, c *clusterPreprocessContext,
	addedOperators, deletedOperators []*models.MonitoredOperator) error {
	if r.eventsHandler == nil {
		return nil
	}
	if len(addedOperators) > 0 {
		r.notifyAddedOperatorFeatures(ctx, c, addedOperators)
	}
	if len(deletedOperators) > 0 {
		r.notifyDeletedOperatorFeatures(ctx, c, deletedOperators)
	}
	return nil
}

func (r *refreshPreprocessor) notifyAddedOperatorFeatures(ctx context.Context, c *clusterPreprocessContext,
	operators []*models.MonitoredOperator) {
	featureList := r.calculateOperatorFeatureList(operators)
	var message string
	if len(operators) == 1 {
		message = fmt.Sprintf("Cluster %s: added operator feature %s", c.clusterId, featureList)
	} else {
		message = fmt.Sprintf("Cluster %s: added operator features %s", c.clusterId, featureList)
	}
	r.eventsHandler.NotifyInternalEvent(ctx, &c.clusterId, nil, nil, message)
}

func (r *refreshPreprocessor) notifyDeletedOperatorFeatures(ctx context.Context, c *clusterPreprocessContext,
	operators []*models.MonitoredOperator) {
	featureList := r.calculateOperatorFeatureList(operators)
	var message string
	if len(operators) == 1 {
		message = fmt.Sprintf("Cluster %s: deleted operator feature %s", c.clusterId, featureList)
	} else {
		message = fmt.Sprintf("Cluster %s: deleted operator features %s", c.clusterId, featureList)
	}
	r.eventsHandler.NotifyInternalEvent(ctx, &c.clusterId, nil, nil, message)
}

func (r *refreshPreprocessor) calculateOperatorFeatureList(operators []*models.MonitoredOperator) string {
	featureNames := make([]string, len(operators))
	for i, operator := range operators {
		featureNames[i] = strings.ToUpper(operator.Name)
	}
	sort.Strings(featureNames)
	return english.WordSeries(featureNames, "and")
}

// sortByValidationResultID sorts results by models.ClusterValidationID
func sortByValidationResultID(validationResults []ValidationResult) {
	sort.SliceStable(validationResults, func(i, j int) bool {
		return validationResults[i].ID < validationResults[j].ID
	})
}

func newValidations(v *clusterValidator) []validation {
	ret := []validation{
		{
			id:        IsMachineCidrDefined,
			condition: v.isMachineCidrDefined,
		},
		{
			id:        IsMachineCidrEqualsToCalculatedCidr,
			condition: v.isMachineCidrEqualsToCalculatedCidr,
		},
		{
			id:        AreApiVipsDefined,
			condition: v.areApiVipsDefined,
		},
		{
			id:        AreApiVipsValid,
			condition: v.areApiVipsValid,
		},
		{
			id:        isNetworkTypeValid,
			condition: v.isNetworkTypeValid,
		},
		{
			id:        AreIngressVipsDefined,
			condition: v.areIngressVipsDefined,
		},
		{
			id:        AreIngressVipsValid,
			condition: v.areIngressVipsValid,
		},
		{
			id:        AllHostsAreReadyToInstall,
			condition: v.allHostsAreReadyToInstall,
		},
		{
			id:        SufficientMastersCount,
			condition: v.SufficientMastersCount,
		},
		{
			id:        IsDNSDomainDefined,
			condition: v.isDNSDomainDefined,
		},
		{
			id:        IsPullSecretSet,
			condition: v.isPullSecretSet,
		},
		{
			id:        isClusterCidrDefined,
			condition: v.isClusterCidrDefined,
		},
		{
			id:        isServiceCidrDefined,
			condition: v.isServiceCidrDefined,
		},
		{
			id:        noCidrOverlapping,
			condition: v.noCidrsOverlapping,
		},
		{
			id:        networkPrefixValid,
			condition: v.networkPrefixValid,
		},
		{
			id:        IsNtpServerConfigured,
			condition: v.isNtpServerConfigured,
		},
		{
			id:        NetworksSameAddressFamilies,
			condition: v.isNetworksSameAddressFamilies,
		},
		{
			id:        PlatformRequirementsSatisfied,
			condition: v.platformRequirementsSatisfied,
		},
	}
	return ret
}

func newConditions(v *clusterValidator) []condition {
	return []condition{
		{
			id: VipDhcpAllocationSet,
			fn: v.isVipDhcpAllocationSet,
		},
		{
			id: AllHostsPreparedSuccessfully,
			fn: v.areAllHostsPreparedSuccessfully,
		},
		{
			id: UnPreparingtHostsExist,
			fn: v.isUnPreparingHostsExist,
		},
		{
			id: ClusterPreparationSucceeded,
			fn: v.isClusterPreparationSucceeded,
		},
		{
			id: ClusterPreparationFailed,
			fn: v.isClusterPreparationFailed,
		},
		{
			id: FailedPreparingtHostsExist,
			fn: v.isFailedPreparingHostExist,
		},
	}
}
