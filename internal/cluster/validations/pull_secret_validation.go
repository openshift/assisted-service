package validations

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/pkg/errors"
)

const (
	dockerHubRegistry   = "docker.io"
	dockerHubLegacyAuth = "https://index.docker.io/v1/"
	stageRegistry       = "registry.stage.redhat.io"
)

// PullSecretValidator is used run validations on a provided pull secret
// it verifies the format of the pull secrete and access to required image registries
//
//go:generate mockgen -source=pull_secret_validation.go -package=validations -destination=mock_pull_secret_validation.go
type PullSecretValidator interface {
	ValidatePullSecret(secret string, username string, releaseImageURL string) error
}

func ParsePublicRegistries(publicRegistries map[string]bool, publicRegistriesLiteral string) {
	if publicRegistriesLiteral == "" {
		return
	}

	for _, registry := range strings.Split(publicRegistriesLiteral, ",") {
		publicRegistries[registry] = true
	}
}

type registryPullSecretValidator struct {
	publicRegistries   map[string]bool
	registriesWithAuth map[string]bool
	authHandler        auth.Authenticator
}

type imagePullSecret struct {
	Auths map[string]map[string]interface{} `json:"auths"`
}

type PullSecretCreds struct {
	Username string
	Password string
	Registry string
	AuthRaw  string
	Email    string
}

// PullSecretError distinguishes secret validation errors produced by this package from other types of errors
type PullSecretError struct {
	Msg   string
	Cause error
}

func (e *PullSecretError) Error() string {
	return e.Msg
}

func (e *PullSecretError) Unwrap() error {
	return e.Cause
}

// ParsePullSecret validates the format of a pull secret and converts the secret string into individual credentail entries
func ParsePullSecret(secret string) (map[string]PullSecretCreds, error) {
	result := make(map[string]PullSecretCreds)
	var s imagePullSecret

	err := json.Unmarshal([]byte(strings.TrimSpace(secret)), &s)
	if err != nil {
		return nil, &PullSecretError{Msg: "pull secret must be a well-formed JSON", Cause: err}
	}

	if len(s.Auths) == 0 {
		return nil, &PullSecretError{Msg: "pull secret must contain 'auths' JSON-object field"}
	}

	for d, a := range s.Auths {

		_, authPresent := a["auth"]
		_, credsStorePresent := a["credsStore"]
		if !authPresent && !credsStorePresent {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: %q JSON-object requires either 'auth' or 'credsStore' field", d)}
		}

		var authRaw string
		if auth, ok := a["auth"].(string); authPresent && !ok {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: 'auth' field of %q is %v but should be a string", d, a["auth"])}
		} else {
			authRaw = auth
		}
		data, err := base64.StdEncoding.DecodeString(authRaw)
		if err != nil {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: 'auth' field of %q is not base64-encoded", d)}
		}

		res := bytes.SplitN(data, []byte(":"), 2)
		if len(res) != 2 {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: 'auth' for %s is not in 'user:password' format", d)}
		}

		var email string
		if emailString, ok := a["email"].(string); ok {
			email = emailString
		}

		result[d] = PullSecretCreds{
			Password: string(res[1]),
			Username: string(res[0]),
			AuthRaw:  authRaw,
			Registry: d,
			Email:    email,
		}

	}
	return result, nil
}

func AddRHRegPullSecret(secret, rhCred string) (string, error) {
	if rhCred == "" {
		return "", errors.Errorf("invalid pull secret")
	}
	var s imagePullSecret
	err := json.Unmarshal([]byte(strings.TrimSpace(secret)), &s)
	if err != nil {
		return secret, errors.Errorf("invalid pull secret: %v", err)
	}
	s.Auths[stageRegistry] = make(map[string]interface{})
	s.Auths[stageRegistry]["auth"] = base64.StdEncoding.EncodeToString([]byte(rhCred))
	ps, err := json.Marshal(s)
	if err != nil {
		return secret, err
	}
	return string(ps), nil
}

func NewPullSecretValidator(publicRegistries map[string]bool, authHandler auth.Authenticator, images ...string) (PullSecretValidator, error) {
	registriesWithAuth := map[string]bool{}
	for _, image := range images {
		registryWithAuth, err := getRegistryAuthStatus(publicRegistries, image)
		if err != nil {
			return nil, err
		}

		if registryWithAuth != nil {
			registriesWithAuth[*registryWithAuth] = true
		}
	}

	return &registryPullSecretValidator{
		publicRegistries:   publicRegistries,
		registriesWithAuth: registriesWithAuth,
		authHandler:        authHandler,
	}, nil
}

func validateRegistryWithAuth(registry string, credentials map[string]PullSecretCreds) error {
	// Both "docker.io" and "https://index.docker.io/v1/" are acceptable for DockerHub login
	if registry == dockerHubRegistry {
		if _, ok := credentials[dockerHubLegacyAuth]; ok {
			return nil
		}
	}

	// We add auth for stage registry automatically
	if registry == stageRegistry {
		return nil
	}

	if _, ok := credentials[registry]; !ok {
		return &PullSecretError{Msg: fmt.Sprintf("pull secret must contain auth for %q", registry)}
	}

	return nil
}

// ValidatePullSecret validates that a pull secret is well formed and contains all required data
func (v *registryPullSecretValidator) ValidatePullSecret(secret string, username string, releaseImageURL string) error {
	creds, err := ParsePullSecret(secret)
	if err != nil {
		return err
	}

	// only check for cloud creds if we're authenticating against Red Hat SSO
	if v.authHandler.AuthType() == auth.TypeRHSSO {
		r, ok := creds["cloud.openshift.com"]
		if !ok {
			return &PullSecretError{Msg: "pull secret must contain auth for \"cloud.openshift.com\""}
		}

		var user interface{}
		user, err = v.authHandler.AuthAgentAuth(r.AuthRaw)
		if err != nil {
			return &PullSecretError{Msg: "failed to authenticate the pull secret token"}
		}

		if (user.(*ocm.AuthPayload)).Username != username {
			return &PullSecretError{Msg: "pull secret token does not match current user"}
		}
	}

	for registry := range v.registriesWithAuth {
		if err = validateRegistryWithAuth(registry, creds); err != nil {
			return err
		}
	}

	registryWithAuth, err := getRegistryAuthStatus(v.publicRegistries, releaseImageURL)
	if err != nil {
		return err
	}

	if registryWithAuth != nil {
		if err := validateRegistryWithAuth(*registryWithAuth, creds); err != nil {
			return err
		}
	}

	return nil
}

// getRegistryAuthStatus takes a release image reference and a set of ignorarble registries,
// and returns the image's registry if it requires authentication and it is not ignorable
func getRegistryAuthStatus(ignorableImages map[string]bool, image string) (*string, error) {
	if image == "" {
		return nil, nil
	}

	registry, err := ParseRegistry(image)
	if err != nil {
		return nil, errors.Wrapf(err, "error occurred while trying to parse the registry out of '%s'", image)
	}

	if (registry == dockerHubRegistry && ignorableImages[dockerHubLegacyAuth]) || ignorableImages[registry] {
		return nil, nil
	}

	return &registry, nil
}
