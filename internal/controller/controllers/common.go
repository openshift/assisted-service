package controllers

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"

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

// generatePassword generates a password of a given length out of the acceptable
// ASCII characters suitable for a password
// taken from https://github.com/CrunchyData/postgres-operator/blob/383dfa95991553352623f14d3d0d4c9193795855/internal/util/secrets.go#L75
func generatePassword(length int) (string, error) {
	password := make([]byte, length)

	// passwordCharLower is the lowest ASCII character to use for generating a
	// password, which is 40
	passwordCharLower := int64(40)
	// passwordCharUpper is the highest ASCII character to use for generating a
	// password, which is 126
	passwordCharUpper := int64(126)
	// passwordCharExclude is a map of characters that we choose to exclude from
	// the password to simplify usage in the shell. There is still enough entropy
	// that exclusion of these characters is OK.
	passwordCharExclude := "`\\"

	// passwordCharSelector is a "big int" that we need to select the random ASCII
	// character for the password. Since the random integer generator looks for
	// values from [0,X), we need to force this to be [40,126]
	passwordCharSelector := big.NewInt(passwordCharUpper - passwordCharLower)

	i := 0

	for i < length {
		val, err := rand.Int(rand.Reader, passwordCharSelector)
		// if there is an error generating the random integer, return
		if err != nil {
			return "", err
		}

		char := byte(passwordCharLower + val.Int64())

		// if the character is in the exclusion list, continue
		if idx := strings.IndexAny(string(char), passwordCharExclude); idx > -1 {
			continue
		}

		password[i] = char
		i++
	}

	return string(password), nil
}
