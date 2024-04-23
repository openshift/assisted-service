package controllers

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PullSecretHandler interface {
	GetValidPullSecret(ctx context.Context, key types.NamespacedName) (string, error)
}

type pullSecretManager struct {
	c         client.Client
	r         client.Reader
	Installer bminventory.InstallerInternals
}

func NewPullSecretHandler(c client.Client, r client.Reader, Installer bminventory.InstallerInternals) PullSecretHandler {
	return &pullSecretManager{
		c:         c,
		r:         r,
		Installer: Installer,
	}
}

func (ps *pullSecretManager) GetValidPullSecret(ctx context.Context, key types.NamespacedName) (string, error) {
	if key.Name == "" || key.Namespace == "" {
		return "", newInputError("Missing reference to pull secret")
	}

	pullSecret, err := ps.getSecretData(ctx, key)
	if err != nil {
		return "", err
	}

	err = ps.Installer.ValidatePullSecret(pullSecret, "", "")
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("invalid pull secret data in secret %s %s", key.Name, pullSecret))
	}
	return pullSecret, nil
}

func (ps *pullSecretManager) getSecretData(ctx context.Context, key types.NamespacedName) (string, error) {
	secret, err := getSecret(ctx, ps.c, ps.r, key)
	if err != nil {
		return "", errors.Errorf("failed to find secret %s: %v", key.Name, err)
	}
	if err := ensureSecretIsLabelled(ctx, ps.c, secret, key); err != nil {
		return "", errors.Errorf("failed to label secret %s for backup: %v", key.Name, err)
	}

	data, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return "", errors.Errorf("secret %s did not contain key %s", key.Name, corev1.DockerConfigJsonKey)
	}

	return string(data), nil
}
