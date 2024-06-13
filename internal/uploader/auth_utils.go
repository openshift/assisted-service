package uploader

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	openshiftNamespace  = "openshift-config"
	openshiftSecretName = "pull-secret"
	openshiftTokenKey   = "cloud.openshift.com"
)

type Identity struct {
	Username    string
	EmailDomain string
}

type APIAuth struct {
	AuthRaw string
}

type PullSecret struct {
	Identity Identity
	APIAuth  APIAuth
}

func getPullSecret(clusterPullSecret string, k8sclient k8sclient.K8SClient) (PullSecret, error) {
	ret := PullSecret{}

	managementCreds, managementErr := getManagementCreds(k8sclient)
	workloadCreds, workloadErr := getCloudOpenshiftCreds(clusterPullSecret)

	if managementErr != nil && workloadErr != nil {
		return ret, errors.Join(
			fmt.Errorf("failed to get management creds: %w", managementErr),
			fmt.Errorf("failed to get workload creds: %w", workloadErr),
		)
	}

	identityCreds := selectPullSecretForDataProcessing(managementCreds, workloadCreds)
	authCreds := selectPullSecretForAuthent(managementCreds, workloadCreds)

	ret.Identity.Username = identityCreds.Username
	ret.Identity.EmailDomain = getEmailDomain(identityCreds.Email)
	ret.APIAuth.AuthRaw = authCreds.AuthRaw

	return ret, nil
}

// Checks if cloud.openshift.com is present in the OCM pull secret to see if the user
// is still opted-in to sending data
func isOCMPullSecretOptIn(k8sclient k8sclient.K8SClient) (bool, error) {
	creds, err := getManagementCreds(k8sclient)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Indicates the pull secret doesn't exist, so we'll assume they're opted-in
			return true, nil
		}

		return false, fmt.Errorf("failed to get management creds: %w", err)
	}

	if creds == nil {
		return false, nil
	}

	return true, nil
}

func getManagementCreds(k8sclient k8sclient.K8SClient) (*validations.PullSecretCreds, error) {
	if k8sclient == nil {
		return nil, errors.New("nil kube client") // Should not happen
	}

	secret, err := k8sclient.GetSecret(openshiftNamespace, openshiftSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	pullSecret, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return nil, errors.New("missing .dockerconfigjson field")
	}

	ret, err := getCloudOpenshiftCreds(string(pullSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud openshift creds: %w", err)
	}

	return ret, nil
}

func getCloudOpenshiftCreds(pullSecretData string) (*validations.PullSecretCreds, error) {
	pullSecret, err := validations.ParsePullSecret(pullSecretData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pull secret: %w", err)
	}

	auth, ok := pullSecret[openshiftTokenKey]
	if !ok {
		return nil, fmt.Errorf("pull secret doesn't contain authentication information for %s", openshiftTokenKey)
	}

	return &auth, nil
}

func selectPullSecretForDataProcessing(managementCreds, workloadCreds *validations.PullSecretCreds) *validations.PullSecretCreds {
	if workloadCreds != nil {
		return workloadCreds
	}

	return managementCreds
}

func selectPullSecretForAuthent(managementCreds, workloadCreds *validations.PullSecretCreds) *validations.PullSecretCreds {
	if managementCreds != nil {
		return managementCreds
	}

	return workloadCreds
}

func getEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) < 2 {
		return ""
	}

	return parts[1]
}
