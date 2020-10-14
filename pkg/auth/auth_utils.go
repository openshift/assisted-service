package auth

import (
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
)

const (
	userAuthHeader  = "Authorization"
	agentAuthHeader = "X-Secret-Key"
)

func AuthHeaderWriter(token string, header string) runtime.ClientAuthInfoWriter {
	return runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
		return r.SetHeaderParam(header, token)
	})
}

func AgentAuthHeaderWriter(token string) runtime.ClientAuthInfoWriter {
	return AuthHeaderWriter(token, agentAuthHeader)
}

func UserAuthHeaderWriter(token string) runtime.ClientAuthInfoWriter {
	return AuthHeaderWriter(token, userAuthHeader)
}

func shouldStorePayloadInCache(err error) bool {
	if err == nil {
		return true
	}
	if serr, ok := err.(*common.ApiErrorResponse); ok {
		return serr.StatusCode() < 500
	}
	return false
}
