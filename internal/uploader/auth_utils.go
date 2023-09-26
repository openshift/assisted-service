package uploader

import (
	"strings"

	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	openshiftTokenKey = "cloud.openshift.com"
)

func getPullSecret(clusterPullSecret string, k8sclient k8sclient.K8SClient, key string) (*validations.PullSecretCreds, error) {
	data, exists := getOCMPullSecret(k8sclient)
	if !exists {
		data = clusterPullSecret
	}
	if data == "" {
		return nil, errors.Errorf("failed to find pull secret for %s", key)
	}
	pullSecret, err := validations.ParsePullSecret(data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse pull secret")
	}
	if auth, ok := pullSecret[key]; ok {
		return &auth, nil
	}
	return nil, errors.Errorf("pull secret doesn't contain authentication information for %s", key)
}

func getOCMPullSecret(k8sclient k8sclient.K8SClient) (string, bool) {
	if k8sclient != nil {
		secret, err := k8sclient.GetSecret("openshift-config", "pull-secret")
		if err != nil {
			err = client.IgnoreNotFound(err)
			// A "not found" error (err == nil) should return false
			// indicating that the secret does not exist
			return "", err != nil
		}
		if pullSecret, ok := secret.Data[corev1.DockerConfigJsonKey]; ok {
			return string(pullSecret), true
		}
		return "", true
	}
	return "", false
}

func getEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// Checks if cloud.openshift.com is present in the OCM pull secret to see if the user
// is still opted-in to sending data
func isOCMPullSecretOptIn(k8sclient k8sclient.K8SClient) bool {
	if data, exists := getOCMPullSecret(k8sclient); exists {
		if pullSecret, err := validations.ParsePullSecret(data); err == nil {
			if _, ok := pullSecret[openshiftTokenKey]; ok {
				return ok
			}
		}
		return false
	}
	// Indicates the pull secret doesn't exist, so we'll assume they're opted-in
	return true
}
