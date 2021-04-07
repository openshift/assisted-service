package controllers

import (
	"context"

	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getPullSecret(ctx context.Context, c client.Client, name, namespace string) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	if err := c.Get(ctx, key, secret); err != nil {
		return "", errors.Wrapf(err, "failed to get pull secret %s", key)
	}

	data, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return "", errors.Errorf("secret %s did not contain key %s", name, corev1.DockerConfigJsonKey)
	}

	return string(data), nil
}

func getInstallEnvByClusterDeployment(ctx context.Context, c client.Client, clusterDeployment *hivev1.ClusterDeployment) (*adiiov1alpha1.InstallEnv, error) {
	installEnvs := &adiiov1alpha1.InstallEnvList{}
	if err := c.List(ctx, installEnvs); err != nil {
		logrus.WithError(err).Errorf("failed to search for installEnv for clusterDeployment %s", clusterDeployment.Name)
		return nil, err
	}
	for _, installEnv := range installEnvs.Items {
		if installEnv.Spec.ClusterRef.Name == clusterDeployment.Name {
			return &installEnv, nil
		}
	}
	logrus.Infof("no installEnv for the clusterDeployment %s", clusterDeployment.Name)
	return nil, nil
}

func addAppLabel(appName string, meta *metav1.ObjectMeta) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels["app"] = appName
}
