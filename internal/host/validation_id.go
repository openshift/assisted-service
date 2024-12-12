package host

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type validationID models.HostValidationID

const (
	IsMediaConnected                               = validationID(models.HostValidationIDMediaConnected)
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
	IsIgnitionDownloadable                         = validationID(models.HostValidationIDIgnitionDownloadable)
	BelongsToMajorityGroup                         = validationID(models.HostValidationIDBelongsToMajorityGroup)
	IsPlatformNetworkSettingsValid                 = validationID(models.HostValidationIDValidPlatformNetworkSettings)
	IsNTPSynced                                    = validationID(models.HostValidationIDNtpSynced)
	IsTimeSyncedBetweenHostAndService              = validationID(models.HostValidationIDTimeSyncedBetweenHostAndService)
	SucessfullOrUnknownContainerImagesAvailability = validationID(models.HostValidationIDContainerImagesAvailable)
	AreLsoRequirementsSatisfied                    = validationID(models.HostValidationIDLsoRequirementsSatisfied)
	AreOdfRequirementsSatisfied                    = validationID(models.HostValidationIDOdfRequirementsSatisfied)
	AreCnvRequirementsSatisfied                    = validationID(models.HostValidationIDCnvRequirementsSatisfied)
	AreLvmRequirementsSatisfied                    = validationID(models.HostValidationIDLvmRequirementsSatisfied)
	AreMceRequirementsSatisfied                    = validationID(models.HostValidationIDMceRequirementsSatisfied)
	AreMtvRequirementsSatisfied                    = validationID(models.HostValidationIDMtvRequirementsSatisfied)
	SufficientOrUnknownInstallationDiskSpeed       = validationID(models.HostValidationIDSufficientInstallationDiskSpeed)
	HasSufficientNetworkLatencyRequirementForRole  = validationID(models.HostValidationIDSufficientNetworkLatencyRequirementForRole)
	HasSufficientPacketLossRequirementForRole      = validationID(models.HostValidationIDSufficientPacketLossRequirementForRole)
	HasDefaultRoute                                = validationID(models.HostValidationIDHasDefaultRoute)
	IsAPIDomainNameResolvedCorrectly               = validationID(models.HostValidationIDAPIDomainNameResolvedCorrectly)
	IsAPIInternalDomainNameResolvedCorrectly       = validationID(models.HostValidationIDAPIIntDomainNameResolvedCorrectly)
	IsAppsDomainNameResolvedCorrectly              = validationID(models.HostValidationIDAppsDomainNameResolvedCorrectly)
	IsReleaseDomainNameResolvedCorrectly           = validationID(models.HostValidationIDReleaseDomainNameResolvedCorrectly)
	CompatibleWithClusterPlatform                  = validationID(models.HostValidationIDCompatibleWithClusterPlatform)
	IsDNSWildcardNotConfigured                     = validationID(models.HostValidationIDDNSWildcardNotConfigured)
	DiskEncryptionRequirementsSatisfied            = validationID(models.HostValidationIDDiskEncryptionRequirementsSatisfied)
	NonOverlappingSubnets                          = validationID(models.HostValidationIDNonOverlappingSubnets)
	VSphereHostUUIDEnabled                         = validationID(models.HostValidationIDVsphereDiskUUIDEnabled)
	CompatibleAgent                                = validationID(models.HostValidationIDCompatibleAgent)
	NoSkipInstallationDisk                         = validationID(models.HostValidationIDNoSkipInstallationDisk)
	NoSkipMissingDisk                              = validationID(models.HostValidationIDNoSkipMissingDisk)
	NoIPCollisionsInNetwork                        = validationID(models.HostValidationIDNoIPCollisionsInNetwork)
	NoIscsiNicBelongsToMachineCidr                 = validationID(models.HostValidationIDNoIscsiNicBelongsToMachineCidr)
	AreNodeFeatureDiscoveryRequirementsSatisfied   = validationID(models.HostValidationIDNodeFeatureDiscoveryRequirementsSatisfied)
	AreNvidiaGPURequirementsSatisfied              = validationID(models.HostValidationIDNvidiaGpuRequirementsSatisfied)
	ArePipelinesRequirementsSatisfied              = validationID(models.HostValidationIDPipelinesRequirementsSatisfied)
	AreServiceMeshRequirementsSatisfied            = validationID(models.HostValidationIDServicemeshRequirementsSatisfied)
	AreServerLessRequirementsSatisfied             = validationID(models.HostValidationIDServerlessRequirementsSatisfied)
	AreOpenShiftAIRequirementsSatisfied            = validationID(models.HostValidationIDOpenshiftAiRequirementsSatisfied)
	AreAuthorinoRequirementsSatisfied              = validationID(models.HostValidationIDAuthorinoRequirementsSatisfied)
	IsMtuValid                                     = validationID(models.HostValidationIDMtuValid)
)

func (v validationID) category() (string, error) {
	switch v {
	case IsConnected,
		IsMediaConnected,
		IsMachineCidrDefined,
		BelongsToMachineCidr,
		IsIgnitionDownloadable,
		BelongsToMajorityGroup,
		IsNTPSynced,
		IsTimeSyncedBetweenHostAndService,
		SucessfullOrUnknownContainerImagesAvailability,
		HasSufficientNetworkLatencyRequirementForRole,
		HasSufficientPacketLossRequirementForRole,
		HasDefaultRoute,
		IsAPIDomainNameResolvedCorrectly,
		IsAPIInternalDomainNameResolvedCorrectly,
		IsPlatformNetworkSettingsValid,
		IsAppsDomainNameResolvedCorrectly,
		IsDNSWildcardNotConfigured,
		NonOverlappingSubnets,
		IsReleaseDomainNameResolvedCorrectly,
		NoIPCollisionsInNetwork,
		IsMtuValid,
		NoIscsiNicBelongsToMachineCidr:
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
		CompatibleWithClusterPlatform,
		VSphereHostUUIDEnabled,
		DiskEncryptionRequirementsSatisfied,
		CompatibleAgent,
		NoSkipInstallationDisk,
		NoSkipMissingDisk:
		return "hardware", nil
	case AreLsoRequirementsSatisfied,
		AreOdfRequirementsSatisfied,
		AreCnvRequirementsSatisfied,
		AreLvmRequirementsSatisfied,
		AreMceRequirementsSatisfied,
		AreMtvRequirementsSatisfied,
		AreNodeFeatureDiscoveryRequirementsSatisfied,
		AreNvidiaGPURequirementsSatisfied,
		ArePipelinesRequirementsSatisfied,
		AreServiceMeshRequirementsSatisfied,
		AreServerLessRequirementsSatisfied,
		AreOpenShiftAIRequirementsSatisfied,
		AreAuthorinoRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected validation id %s", string(v)))
}

func (v validationID) String() string {
	return string(v)
}
