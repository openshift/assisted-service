package kubernetes

import (
	"context"
	"errors"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NoopPodIntrospector returns no imagePullSecrets; useful when introspection is not applicable.
type NoopPodIntrospector struct{}

func (*NoopPodIntrospector) GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference {
	return nil
}

func NewNoopPodIntrospector() *NoopPodIntrospector {
	return &NoopPodIntrospector{}
}

// NewPodIntrospector creates a new PodIntrospector instance
func NewPodIntrospector(client client.Client) (*PodIntrospector, error) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("NAMESPACE")
	if podName == "" || podNamespace == "" {
		return nil, errors.New("POD_NAME or NAMESPACE environment variables not set, cannot introspect imagePullSecrets")
	}

	pi := &PodIntrospector{
		client: client,
		namespacedName: types.NamespacedName{
			Name:      podName,
			Namespace: podNamespace,
		},
	}

	return pi, nil
}

// PodIntrospectorInterface provides access to information about the current pod
type PodIntrospectorInterface interface {
	GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference
}

// PodIntrospector implements PodIntrospectorInterface
type PodIntrospector struct {
	client         client.Client
	namespacedName types.NamespacedName
}

// GetImagePullSecrets retrieves the imagePullSecrets from the current pod
func (p *PodIntrospector) GetImagePullSecrets(ctx context.Context) []corev1.LocalObjectReference {
	pod := &corev1.Pod{}

	err := p.client.Get(ctx, p.namespacedName, pod)
	if err != nil {
		return nil
	}

	return pod.Spec.ImagePullSecrets
}
