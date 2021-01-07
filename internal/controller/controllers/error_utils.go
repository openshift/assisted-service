package controllers

import "github.com/openshift/assisted-service/internal/common"

type KubeAPIError struct {
	err           error
	IsClientError bool
}

func (e *KubeAPIError) Error() string {
	return e.err.Error()
}

func newKubeAPIError(err error, isClientError bool) *KubeAPIError {
	return &KubeAPIError{
		err:           err,
		IsClientError: isClientError,
	}
}

func isClientError(err error) bool {
	switch serr := err.(type) {
	case *KubeAPIError:
		return serr.IsClientError
	case *common.ApiErrorResponse:
		return int(serr.StatusCode()/100) == 4
	case *common.InfraErrorResponse:
		return int(serr.StatusCode()/100) == 4
	default:
		return false
	}
}
