package cluster

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type validationResult struct {
	ID      validationID     `json:"id"`
	Status  validationStatus `json:"status"`
	Message string           `json:"message"`
}

type stringer interface {
	String() string
}

type refreshPreprocessor struct {
	log         logrus.FieldLogger
	validations []validation
	conditions  []condition
}

func newRefreshPreprocessor(log logrus.FieldLogger, hostAPI host.API, operatorsApi operators.API) *refreshPreprocessor {
	return &refreshPreprocessor{
		log:         log,
		validations: newValidations(log, hostAPI, operatorsApi),
		conditions:  newConditions(),
	}
}

func (r *refreshPreprocessor) preprocess(c *clusterPreprocessContext) (map[string]bool, map[string][]validationResult, error) {
	stateMachineInput := make(map[string]bool)
	validationsOutput := make(map[string][]validationResult)
	checkValidationsInStatuses := []string{
		models.ClusterStatusInsufficient, models.ClusterStatusReady, models.ClusterStatusPendingForInput,
	}
	//if the cluster is not on discovery stages - skip the validations check
	if !funk.ContainsString(checkValidationsInStatuses, swag.StringValue(c.cluster.Status)) {
		return stateMachineInput, validationsOutput, nil
	}
	for _, v := range r.validations {
		st := v.condition(c)
		stateMachineInput[v.id.String()] = st == ValidationSuccess
		message := v.formatter(c, st)
		category, err := v.id.category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], validationResult{
			ID:      v.id,
			Status:  st,
			Message: message,
		})
	}
	for _, condition := range r.conditions {
		stateMachineInput[condition.id.String()] = condition.fn(c)
	}
	return stateMachineInput, validationsOutput, nil
}

func newValidations(log logrus.FieldLogger, api host.API, operatorsManager operators.API) []validation {
	v := clusterValidator{
		log:              log,
		hostAPI:          api,
		operatorsManager: operatorsManager,
	}
	ret := []validation{
		{
			id:        IsMachineCidrDefined,
			condition: v.isMachineCidrDefined,
			formatter: v.printIsMachineCidrDefined,
		},
		{
			id:        isMachineCidrEqualsToCalculatedCidr,
			condition: v.isMachineCidrEqualsToCalculatedCidr,
			formatter: v.printIsMachineCidrEqualsToCalculatedCidr,
		},
		{
			id:        isApiVipDefined,
			condition: v.isApiVipDefined,
			formatter: v.printIsApiVipDefined,
		},
		{
			id:        isApiVipValid,
			condition: v.isApiVipValid,
			formatter: v.printIsApiVipValid,
		},
		{
			id:        isIngressVipDefined,
			condition: v.isIngressVipDefined,
			formatter: v.printIsIngressVipDefined,
		},
		{
			id:        isIngressVipValid,
			condition: v.isIngressVipValid,
			formatter: v.printIsIngressVipValid,
		},
		{
			id:        AllHostsAreReadyToInstall,
			condition: v.allHostsAreReadyToInstall,
			formatter: v.printAllHostsAreReadyToInstall,
		},
		{
			id:        SufficientMastersCount,
			condition: v.sufficientMastersCount,
			formatter: v.printSufficientMastersCount,
		},
		{
			id:        IsDNSDomainDefined,
			condition: v.isDNSDomainDefined,
			formatter: v.printIsDNSDomainDefined,
		},
		{
			id:        IsPullSecretSet,
			condition: v.isPullSecretSet,
			formatter: v.printIsPullSecretSet,
		},
		{
			id:        isClusterCidrDefined,
			condition: v.isClusterCidrDefined,
			formatter: v.printIsClusterCidrDefined,
		},
		{
			id:        isServiceCidrDefined,
			condition: v.isServiceCidr,
			formatter: v.printIsServiceCidrDefined,
		},
		{
			id:        noCidrOverlapping,
			condition: v.noCidrsOverlapping,
			formatter: v.printNoCidrsOverlapping,
		},
		{
			id:        networkPrefixValid,
			condition: v.networkPrefixValid,
			formatter: v.printNetworkPrefixValid,
		},
		{
			id:        IsNtpServerConfigured,
			condition: v.isNtpServerConfigured,
			formatter: v.printNtpServerConfigured,
		},
		{
			id:        IsOcsRequirementsSatisfied,
			condition: v.isOcsRequirementsSatisfied,
			formatter: v.printOcsRequirementsSatisfied,
		},
	}
	return ret
}

func newConditions() []condition {
	return []condition{
		{
			id: VipDhcpAllocationSet,
			fn: isVipDhcpAllocationSet,
		},
	}
}
