package cluster

import (
	"context"
	"sort"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
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
	//if the cluster is not on discovery stages - skip the validations check
	if !funk.ContainsString(checkValidationsInStatuses, swag.StringValue(c.cluster.Status)) {
		return stateMachineInput, validationsOutput, nil
	}
	for _, v := range r.validations {
		st, message := v.condition(c)
		stateMachineInput[v.id.String()] = st == ValidationSuccess
		category, err := v.id.Category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], ValidationResult{
			ID:      v.id,
			Status:  st,
			Message: message,
		})
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
			logrus.WithError(err).Warn("id.category()")
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
	return stateMachineInput, validationsOutput, nil
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
			condition: v.sufficientMastersCount,
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
