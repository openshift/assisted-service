package cluster

import (
	"fmt"
	"reflect"

	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
)

func AgentToken(resource interface{}, authType auth.AuthType) (token string, err error) {
	var (
		pullSecret string
		resId      string
	)
	switch res := resource.(type) {
	case *common.InfraEnv:
		resId = res.ID.String()
		pullSecret = res.PullSecret
	case *common.Cluster:
		resId = res.ID.String()
		pullSecret = res.PullSecret
	default:
		return "", fmt.Errorf("unsupported type, expected InfraEnv or Cluster got %s", reflect.TypeOf(resource))
	}

	switch authType {
	case auth.TypeRHSSO:
		token, err = cloudPullSecretToken(pullSecret)
	case auth.TypeLocal:
		token, err = gencrypto.LocalJWT(resId, gencrypto.InfraEnvKey)
	case auth.TypeNone:
		token = ""
	default:
		err = errors.Errorf("invalid authentication type %v", authType)
	}
	return
}

func cloudPullSecretToken(pullSecret string) (string, error) {
	creds, err := validations.ParsePullSecret(pullSecret)
	if err != nil {
		return "", err
	}
	r, ok := creds["cloud.openshift.com"]
	if !ok {
		return "", errors.Errorf("Pull secret does not contain auth for cloud.openshift.com")
	}
	return r.AuthRaw, nil
}
