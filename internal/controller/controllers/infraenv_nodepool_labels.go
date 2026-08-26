package controllers

import (
	"context"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
)

var nodePoolListGVK = schema.GroupVersionKind{
	Group:   "hypershift.openshift.io",
	Version: "v1beta1",
	Kind:    "NodePoolList",
}

// reconcileNodePoolLabelPropagation propagates designated InfraEnv labels into
// associated NodePool.spec.nodeLabels when running in an HCP context.
// The InfraEnv must have a clusterRef pointing to a ClusterDeployment whose name
// matches the HostedCluster, and NodePools are found by spec.clusterName matching.
// If NodePool CRD is not installed, this is a no-op.
func (r *InfraEnvReconciler) reconcileNodePoolLabelPropagation(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) error {
	if infraEnv.Spec.ClusterRef == nil {
		return nil
	}

	propagateKeys := getPropagationKeys(infraEnv)

	hostedClusterName := infraEnv.Spec.ClusterRef.Name
	hostedClusterNamespace := infraEnv.Spec.ClusterRef.Namespace
	if hostedClusterNamespace == "" {
		hostedClusterNamespace = infraEnv.Namespace
	}

	nodePools, err := listNodePoolsForCluster(ctx, r.Client, hostedClusterNamespace, hostedClusterName)
	if err != nil {
		return err
	}
	if len(nodePools) == 0 {
		return nil
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

	var patchErrors []error
	for i := range nodePools {
		np := &nodePools[i]
		patch := client.MergeFrom(np.DeepCopy())

		modified, err := reconcileNodePoolNodeLabels(log, np, desiredInherited, propagateKeys)
		if err != nil {
			log.WithError(err).Errorf("failed to reconcile labels for NodePool %s/%s",
				np.GetNamespace(), np.GetName())
			patchErrors = append(patchErrors, err)
			continue
		}
		if modified {
			if err := r.Patch(ctx, np, patch); err != nil {
				log.WithError(err).Errorf("failed to patch NodePool %s/%s with propagated node labels",
					np.GetNamespace(), np.GetName())
				patchErrors = append(patchErrors, err)
				continue
			}
			log.Infof("propagated node labels to NodePool %s/%s", np.GetNamespace(), np.GetName())
		}
	}

	if len(patchErrors) > 0 {
		return errors.Errorf("failed to patch %d NodePool(s) during node label propagation", len(patchErrors))
	}
	return nil
}

// listNodePoolsForCluster returns all NodePool resources in the given namespace
// where spec.clusterName matches the hostedClusterName. Returns nil (not error)
// if the NodePool CRD is not installed on the cluster.
func listNodePoolsForCluster(ctx context.Context, c client.Client, namespace, hostedClusterName string) ([]unstructured.Unstructured, error) {
	npList := &unstructured.UnstructuredList{}
	npList.SetGroupVersionKind(nodePoolListGVK)

	if err := c.List(ctx, npList, client.InNamespace(namespace)); err != nil {
		if isNoKindMatchError(err) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to list NodePools in namespace %s", namespace)
	}

	var matched []unstructured.Unstructured
	for _, np := range npList.Items {
		clusterName, found, err := unstructured.NestedString(np.Object, "spec", "clusterName")
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read spec.clusterName from NodePool %s/%s", np.GetNamespace(), np.GetName())
		}
		if found && clusterName == hostedClusterName {
			matched = append(matched, np)
		}
	}
	return matched, nil
}

// reconcileNodePoolNodeLabels merges inherited labels into a NodePool's spec.nodeLabels,
// preserving user-set labels. Tracks inherited keys via the inheritedNodeLabelsAnnotation.
// Returns true if the NodePool was modified and any error encountered.
func reconcileNodePoolNodeLabels(log logrus.FieldLogger, np *unstructured.Unstructured, desiredInherited map[string]string, propagateKeys []string) (bool, error) {
	if len(propagateKeys) == 0 {
		return removeAllInheritedNodePoolLabels(np)
	}

	currentNodeLabels, err := getNodePoolNodeLabels(np)
	if err != nil {
		return false, err
	}
	previouslyInherited := getNodePoolInheritedKeys(np)
	modified := false
	var actuallyInherited []string

	for _, key := range previouslyInherited {
		if _, stillDesired := desiredInherited[key]; !stillDesired {
			if _, exists := currentNodeLabels[key]; exists {
				delete(currentNodeLabels, key)
				modified = true
			}
		}
	}

	for key, value := range desiredInherited {
		existingValue, exists := currentNodeLabels[key]
		if !exists {
			currentNodeLabels[key] = value
			modified = true
			actuallyInherited = append(actuallyInherited, key)
		} else if existingValue != value {
			if slices.Contains(previouslyInherited, key) {
				currentNodeLabels[key] = value
				modified = true
				actuallyInherited = append(actuallyInherited, key)
			} else {
				log.Warnf("InfraEnv label key %q conflicts with user-set nodeLabel on NodePool %s/%s; keeping user value",
					key, np.GetNamespace(), np.GetName())
			}
		} else {
			if slices.Contains(previouslyInherited, key) {
				actuallyInherited = append(actuallyInherited, key)
			}
		}
	}

	if modified {
		if err := setNodePoolNodeLabels(np, currentNodeLabels); err != nil {
			return false, err
		}
	}

	newInheritedAnnotation := buildInheritedAnnotation(actuallyInherited)
	annotationModified := setNodePoolInheritedAnnotation(np, newInheritedAnnotation)

	return modified || annotationModified, nil
}

// removeAllInheritedNodePoolLabels removes any previously inherited labels from the NodePool
// when the propagation annotation is removed from InfraEnv.
func removeAllInheritedNodePoolLabels(np *unstructured.Unstructured) (bool, error) {
	previouslyInherited := getNodePoolInheritedKeys(np)
	if len(previouslyInherited) == 0 {
		return false, nil
	}

	currentNodeLabels, err := getNodePoolNodeLabels(np)
	if err != nil {
		return false, err
	}
	modified := false
	for _, key := range previouslyInherited {
		if _, exists := currentNodeLabels[key]; exists {
			delete(currentNodeLabels, key)
			modified = true
		}
	}

	if modified {
		if err := setNodePoolNodeLabels(np, currentNodeLabels); err != nil {
			return false, err
		}
	}

	annotationModified := setNodePoolInheritedAnnotation(np, "")
	return modified || annotationModified, nil
}

// --- NodePool unstructured helpers ---

func getNodePoolNodeLabels(np *unstructured.Unstructured) (map[string]string, error) {
	labels, found, err := unstructured.NestedStringMap(np.Object, "spec", "nodeLabels")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read spec.nodeLabels from NodePool %s/%s", np.GetNamespace(), np.GetName())
	}
	if !found || labels == nil {
		return make(map[string]string), nil
	}
	return labels, nil
}

func setNodePoolNodeLabels(np *unstructured.Unstructured, labels map[string]string) error {
	if len(labels) == 0 {
		unstructured.RemoveNestedField(np.Object, "spec", "nodeLabels")
		return nil
	}
	labelsInterface := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		labelsInterface[k] = v
	}
	return unstructured.SetNestedField(np.Object, labelsInterface, "spec", "nodeLabels")
}

func getNodePoolInheritedKeys(np *unstructured.Unstructured) []string {
	annotations := np.GetAnnotations()
	if annotations == nil {
		return nil
	}
	return parseCommaSeparatedKeys(annotations[inheritedNodeLabelsAnnotation])
}

func setNodePoolInheritedAnnotation(np *unstructured.Unstructured, value string) bool {
	annotations := np.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	existing, exists := annotations[inheritedNodeLabelsAnnotation]
	if exists && existing == value {
		return false
	}

	if value == "" {
		if !exists {
			return false
		}
		delete(annotations, inheritedNodeLabelsAnnotation)
	} else {
		annotations[inheritedNodeLabelsAnnotation] = value
	}
	np.SetAnnotations(annotations)
	return true
}

// isNoKindMatchError returns true if the error indicates the CRD is not installed.
func isNoKindMatchError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no matches for kind") ||
		strings.Contains(err.Error(), "the server could not find the requested resource")
}
