package validations

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type imagePullSecret struct {
	Auths map[string]map[string]interface{} `json:"auths"`
}

type PullSecretCreds struct {
	Username string
	Password string
	Registry string
}

func parsePullSecret(secret string) (map[string]PullSecretCreds, error) {
	result := make(map[string]PullSecretCreds)
	var s imagePullSecret
	err := json.Unmarshal([]byte(secret), &s)
	if err != nil {
		return nil, fmt.Errorf("invalid pull secret: %v", err)
	}
	if len(s.Auths) == 0 {
		return nil, fmt.Errorf("invalid pull secret: missing 'auths' JSON-object field")
	}

	for d, a := range s.Auths {
		_, authPresent := a["auth"]
		_, credsStorePresent := a["credsStore"]
		if !authPresent && !credsStorePresent {
			return nil, fmt.Errorf("invalid pull secret, '%q' JSON-object requires either 'auth' or 'credsStore' field", d)
		}
		data, err := base64.StdEncoding.DecodeString(a["auth"].(string))
		if err != nil {
			return nil, fmt.Errorf("invalid pull secret, 'auth' fiels of '%q' is not base64 decodable", d)
		}
		res := bytes.Split(data, []byte(":"))
		if len(res) != 2 {
			return nil, fmt.Errorf("auth for %s has invalid format", d)
		}
		result[d] = PullSecretCreds{
			Password: string(res[1]),
			Username: string(res[0]),
			Registry: d,
		}

	}
	return result, nil
}

/*
const (
	registryCredsToCheck string = "registry.redhat.io"
)
*/

func ValidatePullSecret(secret string) error {
	_, err := parsePullSecret(secret)
	if err != nil {
		return err
	}
	/*
		Actual credentials check is disabled for not until we solve how to do it in tests and subsystem
		r, ok := creds[registryCredsToCheck]
		if !ok {
			return fmt.Errorf("Pull secret does not contain auth for %s", registryCredsToCheck)
		}
		dc, err := docker.NewEnvClient()
		if err != nil {
			return err
		}
		auth := types.AuthConfig{
			ServerAddress: r.Registry,
			Username:      r.Username,
			Password:      r.Password,
		}
		_, err = dc.RegistryLogin(context.Background(), auth)
		if err != nil {
			return err
		}
	*/
	return nil
}
