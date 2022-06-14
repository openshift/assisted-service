// This file contains functions that simplify the execution of validations from multiple places of
// the service.

package common

// IsAgentCompatible checks if the given agent image is compatible with what the service expects.
func IsAgentCompatible(expectedImage, agentImage string) bool {
	return agentImage == expectedImage
}
