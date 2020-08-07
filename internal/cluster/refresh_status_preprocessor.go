package cluster

import (
	"github.com/sirupsen/logrus"
)

type validationResult struct {
	ID      validationID     `json:"id"`
	Status  validationStatus `json:"status"`
	Message string           `json:"message"`
}

type refreshPreprocessor struct {
	log         logrus.FieldLogger
	validations []validation
}

func newRefreshPreprocessor(log logrus.FieldLogger) *refreshPreprocessor {
	return &refreshPreprocessor{
		log:         log,
		validations: newValidations(log),
	}
}

func (r *refreshPreprocessor) preprocess(c *clusterValidationContext) (map[validationID]bool, map[string][]validationResult, error) {
	stateMachineInput := make(map[validationID]bool)
	validationsOutput := make(map[string][]validationResult)
	for _, v := range r.validations {
		st := v.condition(c)
		stateMachineInput[v.id] = st == ValidationSuccess
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
	return stateMachineInput, validationsOutput, nil
}

func newValidations(log logrus.FieldLogger) []validation {
	v := clusterValidator{
		log: log,
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
			id:        NoPendingForInputHost,
			condition: v.noHostInPendingForInput,
			formatter: v.printNoHostInPendingForInput,
		},
		{
			id:        AllHostsAreReadyToInstall,
			condition: v.allHostsAreReadyToInstall,
			formatter: v.printAllHostsAreReadyToInstall,
		},
		{
			id:        HasExactlyThreeMasters,
			condition: v.hasExactlyThreeKnownMasters,
			formatter: v.printHasExactlyThreeKnownMasters,
		},
	}
	return ret
}
