// This file contains functions that simplify the execution of validations from multiple places of
// the service.

package common

import (
	"github.com/go-openapi/swag"
)

// IsAgentCompatible checks if the given agent image is compatible with what the service expects.
func IsAgentCompatible(expectedImage, agentImage string) bool {
	return agentImage == expectedImage
}

var NonIgnorableHostValidations []string = []string{"connected", "has-inventory", "machine-cidr-defined", "hostname-unique", "hostname-valid"}
var NonIgnorableClusterValidations []string = []string{"api-vips-defined", "ingress-vips-defined", "all-hosts-are-ready-to-install", "sufficient-masters-count", "pull-secret-set", "cluster-preparation-succeeded"}

func ShouldIgnoreValidation(ignoredValidations []string, validationId string, nonIgnoribles []string) bool {
	if !MayIgnoreValidation(validationId, nonIgnoribles) {
		return false
	}
	if swag.ContainsStrings(ignoredValidations, "all") {
		return true
	}
	return swag.ContainsStrings(ignoredValidations, validationId)
}

func MayIgnoreValidation(validationID string, nonIgnorables []string) bool {
	if validationID == "all" {
		return true
	}
	return !swag.ContainsStrings(nonIgnorables, validationID)
}

func MayIgnoreValidations(validationIDs []string, nonIgnorables []string) (bool, []string) {
	result := true
	cantBeIgnored := []string{}
	for _, validation := range validationIDs {
		if validation == "all" {
			return true, []string{}
		}
		if !MayIgnoreValidation(validation, nonIgnorables) {
			cantBeIgnored = append(cantBeIgnored, validation)
			result = false
		}
	}
	return result, cantBeIgnored
}
