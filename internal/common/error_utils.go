package common

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	models "github.com/openshift/assisted-service/models/v1"
)

type NotFound string

func (f NotFound) Error() string {
	return fmt.Sprintf("object %s was not found", string(f))
}

func GenerateError(id int32, err error) *models.Error {
	return &models.Error{
		Code:   swag.String(strconv.Itoa(int(id))),
		Href:   swag.String(""),
		ID:     swag.Int32(id),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
	}
}

func GenerateInternalFromError(err error) *models.Error {
	return &models.Error{
		Code:   swag.String(strconv.Itoa(http.StatusInternalServerError)),
		Href:   swag.String(""),
		ID:     swag.Int32(http.StatusInternalServerError),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
	}
}

func GenerateInfraError(id int32, err error) *models.InfraError {
	return &models.InfraError{
		Code:    swag.Int32(id),
		Message: swag.String(err.Error()),
	}
}

type ApiErrorResponse struct {
	statusCode int32
	err        error
}

func (a *ApiErrorResponse) Error() string {
	return a.err.Error()
}

func (a *ApiErrorResponse) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {
	rw.WriteHeader(int(a.statusCode))
	if err := producer.Produce(rw, GenerateError(a.statusCode, a.err)); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

func (a *ApiErrorResponse) StatusCode() int32 {
	return a.statusCode
}

func NewApiError(statusCode int32, err error) *ApiErrorResponse {
	return &ApiErrorResponse{
		statusCode: statusCode,
		err:        err,
	}
}

type InfraErrorResponse struct {
	*ApiErrorResponse
}

func (i *InfraErrorResponse) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {
	rw.WriteHeader(int(i.statusCode))
	if err := producer.Produce(rw, GenerateInfraError(i.statusCode, i.err)); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

func NewInfraError(statusCode int32, err error) *InfraErrorResponse {
	return &InfraErrorResponse{
		ApiErrorResponse: &ApiErrorResponse{
			statusCode: statusCode,
			err:        err,
		},
	}
}

func IsKnownError(err error) bool {
	switch err.(type) {
	case *ApiErrorResponse:
		return true
	case *InfraErrorResponse:
		return true
	default:
		return false
	}
}

func GenerateErrorResponder(err error) middleware.Responder {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewApiError(http.StatusNotFound, err)
	}

	switch errValue := err.(type) {
	case *ApiErrorResponse:
		return errValue
	case *InfraErrorResponse:
		return errValue
	case NotFound:
		return NewApiError(http.StatusNotFound, err)
	default:
		return NewApiError(http.StatusInternalServerError, err)
	}
}

func GenerateErrorResponderWithDefault(err error, defaultCode int32) middleware.Responder {
	switch errValue := err.(type) {
	case *ApiErrorResponse:
		return errValue
	case *InfraErrorResponse:
		return errValue
	default:
		return NewApiError(defaultCode, err)
	}
}

func ApiErrorWithDefaultInfraError(err error, defaultCode int32) error {
	if IsKnownError(err) {
		return err
	}
	return NewInfraError(defaultCode, err)
}
