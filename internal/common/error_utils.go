package common

import (
	"net/http"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
)

func GenerateError(id int32, err error) *models.Error {
	return &models.Error{
		Code:   swag.String(string(id)),
		Href:   swag.String(""),
		ID:     swag.Int32(id),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
	}
}

func GenerateInternalFromError(err error) *models.Error {
	return &models.Error{
		Code:   swag.String(string(http.StatusInternalServerError)),
		Href:   swag.String(""),
		ID:     swag.Int32(http.StatusInternalServerError),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
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

func GenerateErrorResponder(err error) middleware.Responder {
	switch errValue := err.(type) {
	case *ApiErrorResponse:
		return errValue
	default:
		return NewApiError(http.StatusInternalServerError, err)
	}
}

func GenerateErrorResponderWithDefault(err error, defaultCode int32) middleware.Responder {
	switch errValue := err.(type) {
	case *ApiErrorResponse:
		return errValue
	default:
		return NewApiError(defaultCode, err)
	}
}
