package host

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type validationID models.HostValidationID

const (
	IsConnected                                    = validationID(models.HostValidationIDConnected)
	HasInventory                                   = validationID(models.HostValidationIDHasInventory)
	IsMachineCidrDefined                           = validationID(models.HostValidationIDMachineCidrDefined)
	BelongsToMachineCidr                           = validationID(models.HostValidationIDBelongsToMachineCidr)
	HasMinCPUCores                                 = validationID(models.HostValidationIDHasMinCPUCores)
	HasMinValidDisks                               = validationID(models.HostValidationIDHasMinValidDisks)
	HasMinMemory                                   = validationID(models.HostValidationIDHasMinMemory)
	HasCPUCoresForRole                             = validationID(models.HostValidationIDHasCPUCoresForRole)
	HasMemoryForRole                               = validationID(models.HostValidationIDHasMemoryForRole)
	IsHostnameUnique                               = validationID(models.HostValidationIDHostnameUnique)
	IsHostnameValid                                = validationID(models.HostValidationIDHostnameValid)
	IsAPIVipConnected                              = validationID(models.HostValidationIDAPIVipConnected)
	BelongsToMajorityGroup                         = validationID(models.HostValidationIDBelongsToMajorityGroup)
	IsPlatformValid                                = validationID(models.HostValidationIDValidPlatform)
	IsNTPSynced                                    = validationID(models.HostValidationIDNtpSynced)
	SucessfullOrUnknownContainerImagesAvailability = validationID(models.HostValidationIDContainerImagesAvailable)
	AreLsoRequirementsSatisfied                    = validationID(models.HostValidationIDLsoRequirementsSatisfied)
	AreOcsRequirementsSatisfied                    = validationID(models.HostValidationIDOcsRequirementsSatisfied)
	AreCnvRequirementsSatisfied                    = validationID(models.HostValidationIDCnvRequirementsSatisfied)
	SufficientOrUnknownInstallationDiskSpeed       = validationID(models.HostValidationIDSufficientInstallationDiskSpeed)
	HasSufficientNetworkLatencyRequirementForRole  = validationID(models.HostValidationIDSufficientNetworkLatencyRequirementForRole)
	HasSufficientPacketLossRequirementForRole      = validationID(models.HostValidationIDSufficientPacketLossRequirementForRole)
	HasDefaultRoute                                = validationID(models.HostValidationIDHasDefaultRoute)
	IsAPIDomainNameResolvedCorrectly               = validationID(models.HostValidationIDAPIDomainNameResolvedCorrectly)
	IsAPIInternalDomainNameResolvedCorrectly       = validationID(models.HostValidationIDAPIIntDomainNameResolvedCorrectly)
	IsAppsDomainNameResolvedCorrectly              = validationID(models.HostValidationIDAppsDomainNameResolvedCorrectly)
	CompatibleWithClusterPlatform                  = validationID(models.HostValidationIDCompatibleWithClusterPlatform)
	IsDNSWildcardNotConfigured                     = validationID(models.HostValidationIDDNSWildcardNotConfigured)
	DiskEncryptionRequirementsSatisfied            = validationID(models.HostValidationIDDiskEncryptionRequirementsSatisfied)
)

func (v validationID) category() (string, error) {
	switch v {
	case IsConnected,
		IsMachineCidrDefined,
		BelongsToMachineCidr,
		IsAPIVipConnected,
		BelongsToMajorityGroup,
		IsNTPSynced,
		SucessfullOrUnknownContainerImagesAvailability,
		HasSufficientNetworkLatencyRequirementForRole,
		HasSufficientPacketLossRequirementForRole,
		HasDefaultRoute,
		IsAPIDomainNameResolvedCorrectly,
		IsAPIInternalDomainNameResolvedCorrectly,
		IsAppsDomainNameResolvedCorrectly,
		IsDNSWildcardNotConfigured:
		return "network", nil
	case HasInventory,
		HasMinCPUCores,
		HasMinValidDisks,
		HasMinMemory,
		SufficientOrUnknownInstallationDiskSpeed,
		HasCPUCoresForRole,
		HasMemoryForRole,
		IsHostnameUnique,
		IsHostnameValid,
		IsPlatformValid,
		CompatibleWithClusterPlatform,
		DiskEncryptionRequirementsSatisfied:
		return "hardware", nil
	case AreLsoRequirementsSatisfied,
		AreOcsRequirementsSatisfied,
		AreCnvRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected validation id %s", string(v)))
}

func (v validationID) String() string {
	return string(v)
}
