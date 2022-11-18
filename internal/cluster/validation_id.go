package cluster

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type ValidationID models.ClusterValidationID

const (
	isClusterCidrDefined                = ValidationID(models.ClusterValidationIDClusterCidrDefined)
	isServiceCidrDefined                = ValidationID(models.ClusterValidationIDServiceCidrDefined)
	noCidrOverlapping                   = ValidationID(models.ClusterValidationIDNoCidrsOverlapping)
	networkPrefixValid                  = ValidationID(models.ClusterValidationIDNetworkPrefixValid)
	IsMachineCidrDefined                = ValidationID(models.ClusterValidationIDMachineCidrDefined)
	IsMachineCidrEqualsToCalculatedCidr = ValidationID(models.ClusterValidationIDMachineCidrEqualsToCalculatedCidr)
	NetworksSameAddressFamilies         = ValidationID(models.ClusterValidationIDNetworksSameAddressFamilies)
	IsApiVipDefined                     = ValidationID(models.ClusterValidationIDAPIVipDefined)
	IsApiVipValid                       = ValidationID(models.ClusterValidationIDAPIVipValid)
	isNetworkTypeValid                  = ValidationID(models.ClusterValidationIDNetworkTypeValid)
	IsIngressVipDefined                 = ValidationID(models.ClusterValidationIDIngressVipDefined)
	IsIngressVipValid                   = ValidationID(models.ClusterValidationIDIngressVipValid)
	AllHostsAreReadyToInstall           = ValidationID(models.ClusterValidationIDAllHostsAreReadyToInstall)
	SufficientMastersCount              = ValidationID(models.ClusterValidationIDSufficientMastersCount)
	IsDNSDomainDefined                  = ValidationID(models.ClusterValidationIDDNSDomainDefined)
	IsPullSecretSet                     = ValidationID(models.ClusterValidationIDPullSecretSet)
	IsNtpServerConfigured               = ValidationID(models.ClusterValidationIDNtpServerConfigured)
	IsOdfRequirementsSatisfied          = ValidationID(models.ClusterValidationIDOdfRequirementsSatisfied)
	IsLsoRequirementsSatisfied          = ValidationID(models.ClusterValidationIDLsoRequirementsSatisfied)
	IsCnvRequirementsSatisfied          = ValidationID(models.ClusterValidationIDCnvRequirementsSatisfied)
	IsLvmRequirementsSatisfied          = ValidationID(models.ClusterValidationIDLvmRequirementsSatisfied)
	IsMetalLBRequirementsSatisfied      = ValidationID(models.ClusterValidationIDMetallbRequirementsSatisfied)
)

func (v ValidationID) Category() (string, error) {
	switch v {
	case IsMachineCidrDefined, IsMachineCidrEqualsToCalculatedCidr, IsApiVipDefined, IsApiVipValid, IsIngressVipDefined,
		IsIngressVipValid, isClusterCidrDefined, isServiceCidrDefined, noCidrOverlapping, networkPrefixValid,
		IsDNSDomainDefined, IsNtpServerConfigured, isNetworkTypeValid, NetworksSameAddressFamilies:
		return "network", nil
	case AllHostsAreReadyToInstall, SufficientMastersCount:
		return "hosts-data", nil
	case IsPullSecretSet:
		return "configuration", nil
	case IsOdfRequirementsSatisfied, IsLsoRequirementsSatisfied, IsCnvRequirementsSatisfied, IsLvmRequirementsSatisfied, IsMetalLBRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected cluster validation id %s", string(v)))
}

func (v ValidationID) String() string {
	return string(v)
}
