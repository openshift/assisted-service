package controllers

import (
	"github.com/openshift/assisted-service/internal/common"
)

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

func IsHTTPError(err error, httpErrorCode int) bool {
	switch serr := err.(type) {
	case *common.ApiErrorResponse:
		return int(serr.StatusCode()) == httpErrorCode
	case *common.InfraErrorResponse:
		return int(serr.StatusCode()) == httpErrorCode
	default:
		return false
	}
}

func IsHTTP4XXError(err error) bool {
	switch serr := err.(type) {
	case *common.ApiErrorResponse:
		if serr.StatusCode() >= 400 && serr.StatusCode() < 500 {
			return true
		}
	case *common.InfraErrorResponse:
		if serr.StatusCode() >= 400 && serr.StatusCode() < 500 {
			return true
		}
	default:
		return false
	}
	return false
}
