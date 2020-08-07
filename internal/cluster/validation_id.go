package cluster

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"

	"github.com/openshift/assisted-service/models"
)

type validationID models.ClusterValidationID

const (
	IsMachineCidrDefined                = validationID(models.ClusterValidationIDMachineCidrDefined)
	isMachineCidrEqualsToCalculatedCidr = validationID(models.ClusterValidationIDMachineCidrEqualsToCalculatedCidr)
	isApiVipDefined                     = validationID(models.ClusterValidationIDAPIVipDefined)
	isApiVipValid                       = validationID(models.ClusterValidationIDAPIVipValid)
	isIngressVipDefined                 = validationID(models.ClusterValidationIDIngressVipDefined)
	isIngressVipValid                   = validationID(models.ClusterValidationIDIngressVipValid)
	NoPendingForInputHost               = validationID(models.ClusterValidationIDNoPendingForInputHost)
	AllHostsAreReadyToInstall           = validationID(models.ClusterValidationIDAllHostsAreReadyToInstall)
	HasExactlyThreeMasters              = validationID(models.ClusterValidationIDHasExactlyThreeMasters)
)

func (v validationID) category() (string, error) {
	switch v {
	case IsMachineCidrDefined, isMachineCidrEqualsToCalculatedCidr, isApiVipDefined, isApiVipValid, isIngressVipDefined, isIngressVipValid:
		return "network", nil
	case NoPendingForInputHost, AllHostsAreReadyToInstall, HasExactlyThreeMasters:
		return "hosts-data", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected cluster validation id %s", string(v)))
}

func (v validationID) String() string {
	return string(v)
}
