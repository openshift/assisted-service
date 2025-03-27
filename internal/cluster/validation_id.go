package cluster

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type ValidationID models.ClusterValidationID

const (
	isClusterCidrDefined                           = ValidationID(models.ClusterValidationIDClusterCidrDefined)
	isServiceCidrDefined                           = ValidationID(models.ClusterValidationIDServiceCidrDefined)
	noCidrOverlapping                              = ValidationID(models.ClusterValidationIDNoCidrsOverlapping)
	networkPrefixValid                             = ValidationID(models.ClusterValidationIDNetworkPrefixValid)
	IsMachineCidrDefined                           = ValidationID(models.ClusterValidationIDMachineCidrDefined)
	IsMachineCidrEqualsToCalculatedCidr            = ValidationID(models.ClusterValidationIDMachineCidrEqualsToCalculatedCidr)
	NetworksSameAddressFamilies                    = ValidationID(models.ClusterValidationIDNetworksSameAddressFamilies)
	AreApiVipsDefined                              = ValidationID(models.ClusterValidationIDAPIVipsDefined)
	AreApiVipsValid                                = ValidationID(models.ClusterValidationIDAPIVipsValid)
	isNetworkTypeValid                             = ValidationID(models.ClusterValidationIDNetworkTypeValid)
	AreIngressVipsDefined                          = ValidationID(models.ClusterValidationIDIngressVipsDefined)
	AreIngressVipsValid                            = ValidationID(models.ClusterValidationIDIngressVipsValid)
	AllHostsAreReadyToInstall                      = ValidationID(models.ClusterValidationIDAllHostsAreReadyToInstall)
	SufficientMastersCount                         = ValidationID(models.ClusterValidationIDSufficientMastersCount)
	IsDNSDomainDefined                             = ValidationID(models.ClusterValidationIDDNSDomainDefined)
	IsPullSecretSet                                = ValidationID(models.ClusterValidationIDPullSecretSet)
	IsNtpServerConfigured                          = ValidationID(models.ClusterValidationIDNtpServerConfigured)
	IsOdfRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDOdfRequirementsSatisfied)
	IsLsoRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDLsoRequirementsSatisfied)
	IsCnvRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDCnvRequirementsSatisfied)
	IsLvmRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDLvmRequirementsSatisfied)
	IsMceRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDMceRequirementsSatisfied)
	IsMtvRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDMtvRequirementsSatisfied)
	IsOscRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDOscRequirementsSatisfied)
	PlatformRequirementsSatisfied                  = ValidationID(models.ClusterValidationIDPlatformRequirementsSatisfied)
	IsNodeFeatureDiscoveryRequirementsSatisfied    = ValidationID(models.ClusterValidationIDNodeFeatureDiscoveryRequirementsSatisfied)
	IsNvidiaGPURequirementsSatisfied               = ValidationID(models.ClusterValidationIDNvidiaGpuRequirementsSatisfied)
	IsPipelinesRequirementsSatisfied               = ValidationID(models.ClusterValidationIDPipelinesRequirementsSatisfied)
	IsServiceMeshRequirementsSatisfied             = ValidationID(models.ClusterValidationIDServicemeshRequirementsSatisfied)
	IsServerLessRequirementsSatisfied              = ValidationID(models.ClusterValidationIDServerlessRequirementsSatisfied)
	IsOpenShiftAIRequirementsSatisfied             = ValidationID(models.ClusterValidationIDOpenshiftAiRequirementsSatisfied)
	IsAuthorinoRequirementsSatisfied               = ValidationID(models.ClusterValidationIDAuthorinoRequirementsSatisfied)
	IsNmstateRequirementsSatisfied                 = ValidationID(models.ClusterValidationIDNmstateRequirementsSatisfied)
	IsAMDGPURequirementsSatisfied                  = ValidationID(models.ClusterValidationIDAmdGpuRequirementsSatisfied)
	IsKMMRequirementsSatisfied                     = ValidationID(models.ClusterValidationIDKmmRequirementsSatisfied)
	AreNodeHealthcheckRequirementsSatisfied        = ValidationID(models.ClusterValidationIDNodeHealthcheckRequirementsSatisfied)
	AreSelfNodeRemediationRequirementsSatisfied    = ValidationID(models.ClusterValidationIDSelfNodeRemediationRequirementsSatisfied)
	AreFenceAgentsRemediationRequirementsSatisfied = ValidationID(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied)
	AreNodeMaintenanceRequirementsSatisfied        = ValidationID(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied)
	AreKubeDeschedulerRequirementsSatisfied        = ValidationID(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied)
)

func (v ValidationID) Category() (string, error) {
	switch v {
	case IsMachineCidrDefined, IsMachineCidrEqualsToCalculatedCidr, AreApiVipsDefined, AreApiVipsValid, AreIngressVipsDefined,
		AreIngressVipsValid, isClusterCidrDefined, isServiceCidrDefined, noCidrOverlapping, networkPrefixValid,
		IsDNSDomainDefined, IsNtpServerConfigured, isNetworkTypeValid, NetworksSameAddressFamilies:
		return "network", nil
	case AllHostsAreReadyToInstall, SufficientMastersCount:
		return "hosts-data", nil
	case IsPullSecretSet, PlatformRequirementsSatisfied:
		return "configuration", nil
	case IsOdfRequirementsSatisfied,
		IsLsoRequirementsSatisfied,
		IsCnvRequirementsSatisfied,
		IsLvmRequirementsSatisfied,
		IsMceRequirementsSatisfied,
		IsMtvRequirementsSatisfied,
		IsOscRequirementsSatisfied,
		IsNodeFeatureDiscoveryRequirementsSatisfied,
		IsNvidiaGPURequirementsSatisfied,
		IsPipelinesRequirementsSatisfied,
		IsServiceMeshRequirementsSatisfied,
		IsServerLessRequirementsSatisfied,
		IsOpenShiftAIRequirementsSatisfied,
		IsAuthorinoRequirementsSatisfied,
		IsNmstateRequirementsSatisfied,
		IsAMDGPURequirementsSatisfied,
		IsKMMRequirementsSatisfied,
		AreNodeHealthcheckRequirementsSatisfied,
		AreSelfNodeRemediationRequirementsSatisfied,
		AreFenceAgentsRemediationRequirementsSatisfied,
		AreNodeMaintenanceRequirementsSatisfied,
		AreKubeDeschedulerRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected cluster validation id %s", string(v)))
}

func (v ValidationID) String() string {
	return string(v)
}
