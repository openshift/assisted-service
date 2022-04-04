package auth

import (
	"net/http"

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

func toAction(request *http.Request) Action {
	if request == nil {
		return NoneAction
	}

	switch {
	case request.Method == http.MethodPost ||
		request.Method == http.MethodPut ||
		request.Method == http.MethodPatch:
		return UpdateAction
	case request.Method == http.MethodDelete:
		return DeleteAction
	case request.Method == http.MethodGet:
		return ReadAction
	default:
		return NoneAction
	}
}
