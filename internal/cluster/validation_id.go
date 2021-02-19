package cluster

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type validationID models.ClusterValidationID

const (
	isClusterCidrDefined                = validationID(models.ClusterValidationIDClusterCidrDefined)
	isServiceCidrDefined                = validationID(models.ClusterValidationIDServiceCidrDefined)
	noCidrOverlapping                   = validationID(models.ClusterValidationIDNoCidrsOverlapping)
	networkPrefixValid                  = validationID(models.ClusterValidationIDNetworkPrefixValid)
	IsMachineCidrDefined                = validationID(models.ClusterValidationIDMachineCidrDefined)
	isMachineCidrEqualsToCalculatedCidr = validationID(models.ClusterValidationIDMachineCidrEqualsToCalculatedCidr)
	isApiVipDefined                     = validationID(models.ClusterValidationIDAPIVipDefined)
	isApiVipValid                       = validationID(models.ClusterValidationIDAPIVipValid)
	isIngressVipDefined                 = validationID(models.ClusterValidationIDIngressVipDefined)
	isIngressVipValid                   = validationID(models.ClusterValidationIDIngressVipValid)
	AllHostsAreReadyToInstall           = validationID(models.ClusterValidationIDAllHostsAreReadyToInstall)
	SufficientMastersCount              = validationID(models.ClusterValidationIDSufficientMastersCount)
	IsDNSDomainDefined                  = validationID(models.ClusterValidationIDDNSDomainDefined)
	IsPullSecretSet                     = validationID(models.ClusterValidationIDPullSecretSet)
	IsNtpServerConfigured               = validationID(models.ClusterValidationIDNtpServerConfigured)
	IsOcsRequirementsSatisfied          = validationID(models.ClusterValidationIDOcsRequirementsSatisfied)
	IsLsoRequirementsSatisfied          = validationID(models.ClusterValidationIDLsoRequirementsSatisfied)
	IsCnvRequirementsSatisfied          = validationID(models.ClusterValidationIDCnvRequirementsSatisfied)
)

func (v validationID) category() (string, error) {
	switch v {
	case IsMachineCidrDefined, isMachineCidrEqualsToCalculatedCidr, isApiVipDefined, isApiVipValid, isIngressVipDefined, isIngressVipValid,
		isClusterCidrDefined, isServiceCidrDefined, noCidrOverlapping, networkPrefixValid, IsDNSDomainDefined, IsNtpServerConfigured:
		return "network", nil
	case AllHostsAreReadyToInstall, SufficientMastersCount:
		return "hosts-data", nil
	case IsPullSecretSet:
		return "configuration", nil
	case IsOcsRequirementsSatisfied, IsLsoRequirementsSatisfied, IsCnvRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected cluster validation id %s", string(v)))
}

func (v validationID) String() string {
	return string(v)
}
