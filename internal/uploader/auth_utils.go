package uploader

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	corev1 "k8s.io/api/core/v1"
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

	if ret.APIAuth.AuthRaw == "" {
		return ret, errors.New("no credentials provided")
	}

	return ret, nil
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
	// If one of the two is nil, return the other
	if workloadCreds == nil {
		return managementCreds
	}

	if managementCreds == nil {
		return workloadCreds
	}

	// Otherwise, return the one with more information.
	// If both have the same number of information, priority to the workloadCreds
	workloadInfos := countIdentityInfo(workloadCreds)
	managementInfos := countIdentityInfo(managementCreds)

	if managementInfos > workloadInfos {
		return managementCreds
	}

	return workloadCreds
}

func countIdentityInfo(creds *validations.PullSecretCreds) int {
	ret := 0

	if creds.Username != "" {
		ret++
	}

	if creds.Email != "" {
		ret++
	}

	return ret
}

func selectPullSecretForAuthent(managementCreds, workloadCreds *validations.PullSecretCreds) *validations.PullSecretCreds {
	if managementCreds != nil && managementCreds.AuthRaw != "" {
		return managementCreds
	}

	if workloadCreds != nil && workloadCreds.AuthRaw != "" {
		return workloadCreds
	}

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
