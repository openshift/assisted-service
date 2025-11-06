package kubernetes

import (
	"context"
	"errors"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodIntrospector provides access to information about the current pod
type PodIntrospector interface {
	GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference
}

// NoopPodIntrospector returns no imagePullSecrets useful when introspection is not applicable.
type NoopPodIntrospector struct{}

func NewNoopPodIntrospector() *NoopPodIntrospector {
	return &NoopPodIntrospector{}
}

func (*NoopPodIntrospector) GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference {
	return nil
}

// KubePodIntrospector implements PodIntrospector
type KubePodIntrospector struct {
	client         client.Client
	namespacedName types.NamespacedName
}

// NewKubePodIntrospector creates a new KubePodIntrospector instance
func NewKubePodIntrospector(client client.Client) (*KubePodIntrospector, error) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("NAMESPACE")
	if podName == "" || podNamespace == "" {
		return nil, errors.New("POD_NAME or NAMESPACE environment variables not set, cannot introspect imagePullSecrets")
	}

	pi := &KubePodIntrospector{
		client: client,
		namespacedName: types.NamespacedName{
			Name:      podName,
			Namespace: podNamespace,
		},
	}

	return pi, nil
}

// GetImagePullSecrets retrieves the imagePullSecrets from the current pod
func (p *KubePodIntrospector) GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference {
	pod := &corev1.Pod{}

	err := p.client.Get(ctx, p.namespacedName, pod)
	if err != nil {
		return nil
	}

	return pod.Spec.ImagePullSecrets
}
