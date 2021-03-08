package cluster

import (
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
)

func AgentToken(c *common.Cluster, authType auth.AuthType) (string, error) {
	creds, err := validations.ParsePullSecret(c.PullSecret)
	if err != nil {
		return "", err
	}
	pullSecretToken := ""
	if authType == auth.TypeRHSSO {
		r, ok := creds["cloud.openshift.com"]
		if !ok {
			return "", errors.Errorf("Pull secret does not contain auth for cloud.openshift.com")
		}
		pullSecretToken = r.AuthRaw
	}

	return pullSecretToken, nil
}
