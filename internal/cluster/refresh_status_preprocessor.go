package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorcommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
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
	log          logrus.FieldLogger
	validations  []validation
	conditions   []condition
	operatorsAPI operators.API
}

func newRefreshPreprocessor(log logrus.FieldLogger, hostAPI host.API, operatorsAPI operators.API) *refreshPreprocessor {
	v := clusterValidator{
		log:     log,
		hostAPI: hostAPI,
	}

	return &refreshPreprocessor{
		log:          log,
		validations:  newValidations(&v),
		conditions:   newConditions(&v),
		operatorsAPI: operatorsAPI,
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
			return nil, nil, errors.Wrap(err, fmt.Sprintf("Unable to deserialize ignored cluster validations for cluster %s", string(*c.cluster.ID)))
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
	err = r.recalculateOperatorDependencies(c)
	if err != nil {
		err = errors.Wrapf(err, "failed to recalculate operator dependencies for cluster '%s'", c.clusterId)
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
func (r *refreshPreprocessor) recalculateOperatorDependencies(c *clusterPreprocessContext) error {
	previousOperators := c.cluster.MonitoredOperators
	currentOperators, err := r.operatorsAPI.ResolveDependencies(c.cluster, c.cluster.MonitoredOperators)
	if err != nil {
		return errors.Wrapf(
			err,
			"failed to resolve operator dependencies for cluster '%s'",
			c.clusterId,
		)
	}
	var addedOperators, updatedOperators, deletedOperators []*models.MonitoredOperator
	for _, currentOperator := range currentOperators {
		if currentOperator.ClusterID == "" {
			currentOperator.ClusterID = c.clusterId
		}
		previousOperator := operatorcommon.GetOperator(previousOperators, currentOperator.Name)
		if previousOperator != nil {
			if !reflect.DeepEqual(currentOperator, previousOperator) {
				updatedOperators = append(updatedOperators, currentOperator)
			}
		} else {
			addedOperators = append(addedOperators, currentOperator)
		}
	}
	for _, previousOperator := range previousOperators {
		if !operatorcommon.HasOperator(currentOperators, previousOperator.Name) {
			deletedOperators = append(deletedOperators, previousOperator)
		}
	}
	for _, addedOperator := range addedOperators {
		err = c.db.Save(addedOperator).Error
		if err != nil {
			return errors.Wrapf(
				err,
				"failed to add operator '%s' to cluster '%s'",
				addedOperator.Name, *c.cluster.ID,
			)
		}
	}
	for _, updatedOperator := range updatedOperators {
		err = c.db.Save(updatedOperator).Error
		if err != nil {
			return errors.Wrapf(
				err,
				"failed to update operator '%s' for cluster '%s'",
				updatedOperator.Name, *c.cluster.ID,
			)
		}
	}
	for _, deletedOperator := range deletedOperators {
		err = c.db.Delete(deletedOperator).Error
		if err != nil {
			return errors.Wrapf(
				err,
				"failed to delete operator '%s' from cluster '%s'",
				deletedOperator.Name,
				*c.cluster.ID,
			)
		}
	}
	c.cluster.MonitoredOperators = currentOperators
	return nil
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
