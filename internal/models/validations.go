package models

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type HostValidationID models.HostValidationID

const (
	IsMediaConnected                               = HostValidationID(models.HostValidationIDMediaConnected)
	IsConnected                                    = HostValidationID(models.HostValidationIDConnected)
	HasInventory                                   = HostValidationID(models.HostValidationIDHasInventory)
	IsMachineCidrDefined                           = HostValidationID(models.HostValidationIDMachineCidrDefined)
	BelongsToMachineCidr                           = HostValidationID(models.HostValidationIDBelongsToMachineCidr)
	HasMinCPUCores                                 = HostValidationID(models.HostValidationIDHasMinCPUCores)
	HasMinValidDisks                               = HostValidationID(models.HostValidationIDHasMinValidDisks)
	HasMinMemory                                   = HostValidationID(models.HostValidationIDHasMinMemory)
	HasCPUCoresForRole                             = HostValidationID(models.HostValidationIDHasCPUCoresForRole)
	HasMemoryForRole                               = HostValidationID(models.HostValidationIDHasMemoryForRole)
	IsHostnameUnique                               = HostValidationID(models.HostValidationIDHostnameUnique)
	IsHostnameValid                                = HostValidationID(models.HostValidationIDHostnameValid)
	IsIgnitionDownloadable                         = HostValidationID(models.HostValidationIDIgnitionDownloadable)
	BelongsToMajorityGroup                         = HostValidationID(models.HostValidationIDBelongsToMajorityGroup)
	IsPlatformNetworkSettingsValid                 = HostValidationID(models.HostValidationIDValidPlatformNetworkSettings)
	IsNTPSynced                                    = HostValidationID(models.HostValidationIDNtpSynced)
	SucessfullOrUnknownContainerImagesAvailability = HostValidationID(models.HostValidationIDContainerImagesAvailable)
	AreLsoRequirementsSatisfied                    = HostValidationID(models.HostValidationIDLsoRequirementsSatisfied)
	AreOdfRequirementsSatisfied                    = HostValidationID(models.HostValidationIDOdfRequirementsSatisfied)
	AreCnvRequirementsSatisfied                    = HostValidationID(models.HostValidationIDCnvRequirementsSatisfied)
	SufficientOrUnknownInstallationDiskSpeed       = HostValidationID(models.HostValidationIDSufficientInstallationDiskSpeed)
	HasSufficientNetworkLatencyRequirementForRole  = HostValidationID(models.HostValidationIDSufficientNetworkLatencyRequirementForRole)
	HasSufficientPacketLossRequirementForRole      = HostValidationID(models.HostValidationIDSufficientPacketLossRequirementForRole)
	HasDefaultRoute                                = HostValidationID(models.HostValidationIDHasDefaultRoute)
	IsAPIDomainNameResolvedCorrectly               = HostValidationID(models.HostValidationIDAPIDomainNameResolvedCorrectly)
	IsAPIInternalDomainNameResolvedCorrectly       = HostValidationID(models.HostValidationIDAPIIntDomainNameResolvedCorrectly)
	IsAppsDomainNameResolvedCorrectly              = HostValidationID(models.HostValidationIDAppsDomainNameResolvedCorrectly)
	CompatibleWithClusterPlatform                  = HostValidationID(models.HostValidationIDCompatibleWithClusterPlatform)
	IsDNSWildcardNotConfigured                     = HostValidationID(models.HostValidationIDDNSWildcardNotConfigured)
	DiskEncryptionRequirementsSatisfied            = HostValidationID(models.HostValidationIDDiskEncryptionRequirementsSatisfied)
	NonOverlappingSubnets                          = HostValidationID(models.HostValidationIDNonOverlappingSubnets)
	VSphereHostUUIDEnabled                         = HostValidationID(models.HostValidationIDVsphereDiskUUIDEnabled)
)

func (v HostValidationID) Category() (string, error) {
	switch v {
	case IsConnected,
		IsMediaConnected,
		IsMachineCidrDefined,
		BelongsToMachineCidr,
		IsIgnitionDownloadable,
		BelongsToMajorityGroup,
		IsNTPSynced,
		SucessfullOrUnknownContainerImagesAvailability,
		HasSufficientNetworkLatencyRequirementForRole,
		HasSufficientPacketLossRequirementForRole,
		HasDefaultRoute,
		IsAPIDomainNameResolvedCorrectly,
		IsAPIInternalDomainNameResolvedCorrectly,
		IsPlatformNetworkSettingsValid,
		IsAppsDomainNameResolvedCorrectly,
		IsDNSWildcardNotConfigured,
		NonOverlappingSubnets:
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
		DiskEncryptionRequirementsSatisfied:
		return "hardware", nil
	case AreLsoRequirementsSatisfied,
		AreOdfRequirementsSatisfied,
		AreCnvRequirementsSatisfied:
		return "operators", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected validation id %s", string(v)))
}

func (v HostValidationID) String() string {
	return string(v)
}

type HostValidationStatus string

const (
	HostValidationSuccess               HostValidationStatus = "success"
	HostValidationSuccessSuppressOutput HostValidationStatus = "success-suppress-output"
	HostValidationFailure               HostValidationStatus = "failure"
	HostValidationPending               HostValidationStatus = "pending"
	HostValidationError                 HostValidationStatus = "error"
	HostValidationDisabled              HostValidationStatus = "disabled"
)

func (s HostValidationStatus) String() string {
	return string(s)
}

type HostValidationResult struct {
	ID      HostValidationID     `json:"id"`
	Status  HostValidationStatus `json:"status"`
	Message string               `json:"message"`
}

type HostValidationsStatus map[string]HostValidationResults

type HostValidationResults []HostValidationResult
