package cluster

import (
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
)

func AgentToken(infraEnv *common.InfraEnv, authType auth.AuthType) (token string, err error) {
	switch authType {
	case auth.TypeRHSSO:
		token, err = cloudPullSecretToken(infraEnv.PullSecret)
	case auth.TypeLocal:
		token, err = gencrypto.LocalJWT(infraEnv.ID.String(), gencrypto.InfraEnvKey)
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
