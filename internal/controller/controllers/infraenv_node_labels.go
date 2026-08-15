package controllers

import (
	"sort"
	"strings"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/sirupsen/logrus"
)

// PropagateInfraEnvNodeLabels copies designated labels from an InfraEnv into an Agent's
// spec.nodeLabels, following the same pattern as InfraEnv.spec.agentLabels → Agent metadata.
// The propagation is opt-in via the PropagateNodeLabelsAnnotation on InfraEnv, which contains
// a comma-separated list of label keys to propagate.
//
// Returns true if any change was made to agent.Spec.NodeLabels.
func PropagateInfraEnvNodeLabels(log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, agent *aiv1beta1.Agent) bool {
	propagateKeys := getPropagationKeys(infraEnv)
	if len(propagateKeys) == 0 {
		return removeAllInheritedNodeLabels(agent)
	}

	infraEnvLabels := infraEnv.GetLabels()
	if infraEnvLabels == nil {
		infraEnvLabels = make(map[string]string)
	}

	desiredInherited := make(map[string]string)
	for _, key := range propagateKeys {
		if value, exists := infraEnvLabels[key]; exists {
			desiredInherited[key] = value
		}
	}

	return reconcileAgentNodeLabels(log, agent, desiredInherited)
}

// getPropagationKeys reads the propagate-node-labels annotation from InfraEnv and returns
// the list of label keys that should be propagated to Agent.spec.nodeLabels.
func getPropagationKeys(infraEnv *aiv1beta1.InfraEnv) []string {
	annotations := infraEnv.GetAnnotations()
	if annotations == nil {
		return nil
	}

	value, exists := annotations[PropagateNodeLabelsAnnotation]
	if !exists || strings.TrimSpace(value) == "" {
		return nil
	}

	rawKeys := strings.Split(value, ",")
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		trimmed := strings.TrimSpace(k)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}

// reconcileAgentNodeLabels merges inherited labels into Agent.spec.nodeLabels, preserving
// user-set labels. Tracks inherited keys via the InheritedNodeLabelsAnnotation on the Agent.
// Returns true if the agent was modified.
func reconcileAgentNodeLabels(log logrus.FieldLogger, agent *aiv1beta1.Agent, desiredInherited map[string]string) bool {
	currentNodeLabels := agent.Spec.NodeLabels
	if currentNodeLabels == nil {
		currentNodeLabels = make(map[string]string)
	}

	previouslyInherited := getInheritedKeys(agent)
	modified := false
	actuallyInherited := make(map[string]string)

	// Remove previously inherited labels that are no longer in the desired set
	for _, key := range previouslyInherited {
		if _, stillDesired := desiredInherited[key]; !stillDesired {
			if _, exists := currentNodeLabels[key]; exists {
				delete(currentNodeLabels, key)
				modified = true
			}
		}
	}

	// Add or update inherited labels
	for key, value := range desiredInherited {
		existingValue, exists := currentNodeLabels[key]
		if !exists {
			currentNodeLabels[key] = value
			modified = true
			actuallyInherited[key] = value
		} else if existingValue != value {
			if isInheritedKey(previouslyInherited, key) {
				currentNodeLabels[key] = value
				modified = true
				actuallyInherited[key] = value
			} else {
				log.Warnf("InfraEnv label key %q conflicts with user-set nodeLabel on Agent %s/%s; keeping user value",
					key, agent.Namespace, agent.Name)
			}
		} else {
			// Value matches — track as inherited if it was previously inherited
			if isInheritedKey(previouslyInherited, key) {
				actuallyInherited[key] = value
			}
		}
	}

	if modified {
		agent.Spec.NodeLabels = currentNodeLabels
	}

	// Update the tracking annotation with only controller-owned keys
	newInheritedAnnotation := buildInheritedAnnotation(actuallyInherited)
	annotationModified := setInheritedAnnotation(agent, newInheritedAnnotation)

	return modified || annotationModified
}

// removeAllInheritedNodeLabels removes any previously inherited labels from the Agent when
// the propagation annotation is removed from InfraEnv.
func removeAllInheritedNodeLabels(agent *aiv1beta1.Agent) bool {
	previouslyInherited := getInheritedKeys(agent)
	if len(previouslyInherited) == 0 {
		return false
	}

	currentNodeLabels := agent.Spec.NodeLabels
	if currentNodeLabels == nil {
		return clearInheritedAnnotation(agent)
	}

	modified := false
	for _, key := range previouslyInherited {
		if _, exists := currentNodeLabels[key]; exists {
			delete(currentNodeLabels, key)
			modified = true
		}
	}

	if modified {
		agent.Spec.NodeLabels = currentNodeLabels
	}

	annotationModified := clearInheritedAnnotation(agent)
	return modified || annotationModified
}

// getInheritedKeys reads the InheritedNodeLabelsAnnotation from the Agent and returns
// the list of label keys that were inherited from InfraEnv.
func getInheritedKeys(agent *aiv1beta1.Agent) []string {
	annotations := agent.GetAnnotations()
	if annotations == nil {
		return nil
	}

	value, exists := annotations[InheritedNodeLabelsAnnotation]
	if !exists || strings.TrimSpace(value) == "" {
		return nil
	}

	rawKeys := strings.Split(value, ",")
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		trimmed := strings.TrimSpace(k)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}

func isInheritedKey(inheritedKeys []string, key string) bool {
	for _, k := range inheritedKeys {
		if k == key {
			return true
		}
	}
	return false
}

func buildInheritedAnnotation(desiredInherited map[string]string) string {
	if len(desiredInherited) == 0 {
		return ""
	}
	keys := make([]string, 0, len(desiredInherited))
	for k := range desiredInherited {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func setInheritedAnnotation(agent *aiv1beta1.Agent, value string) bool {
	annotations := agent.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	existing, exists := annotations[InheritedNodeLabelsAnnotation]
	if exists && existing == value {
		return false
	}

	if value == "" {
		if !exists {
			return false
		}
		delete(annotations, InheritedNodeLabelsAnnotation)
	} else {
		annotations[InheritedNodeLabelsAnnotation] = value
	}
	agent.SetAnnotations(annotations)
	return true
}

func clearInheritedAnnotation(agent *aiv1beta1.Agent) bool {
	return setInheritedAnnotation(agent, "")
}
