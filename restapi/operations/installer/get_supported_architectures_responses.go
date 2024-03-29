// Code generated by go-swagger; DO NOT EDIT.

package installer

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/openshift/assisted-service/models"
)

// GetSupportedArchitecturesOKCode is the HTTP code returned for type GetSupportedArchitecturesOK
const GetSupportedArchitecturesOKCode int = 200

/*
GetSupportedArchitecturesOK Success.

swagger:response getSupportedArchitecturesOK
*/
type GetSupportedArchitecturesOK struct {

	/*
	  In: Body
	*/
	Payload *GetSupportedArchitecturesOKBody `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesOK creates GetSupportedArchitecturesOK with default headers values
func NewGetSupportedArchitecturesOK() *GetSupportedArchitecturesOK {

	return &GetSupportedArchitecturesOK{}
}

// WithPayload adds the payload to the get supported architectures o k response
func (o *GetSupportedArchitecturesOK) WithPayload(payload *GetSupportedArchitecturesOKBody) *GetSupportedArchitecturesOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures o k response
func (o *GetSupportedArchitecturesOK) SetPayload(payload *GetSupportedArchitecturesOKBody) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetSupportedArchitecturesBadRequestCode is the HTTP code returned for type GetSupportedArchitecturesBadRequest
const GetSupportedArchitecturesBadRequestCode int = 400

/*
GetSupportedArchitecturesBadRequest Error.

swagger:response getSupportedArchitecturesBadRequest
*/
type GetSupportedArchitecturesBadRequest struct {

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesBadRequest creates GetSupportedArchitecturesBadRequest with default headers values
func NewGetSupportedArchitecturesBadRequest() *GetSupportedArchitecturesBadRequest {

	return &GetSupportedArchitecturesBadRequest{}
}

// WithPayload adds the payload to the get supported architectures bad request response
func (o *GetSupportedArchitecturesBadRequest) WithPayload(payload *models.Error) *GetSupportedArchitecturesBadRequest {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures bad request response
func (o *GetSupportedArchitecturesBadRequest) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesBadRequest) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(400)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetSupportedArchitecturesUnauthorizedCode is the HTTP code returned for type GetSupportedArchitecturesUnauthorized
const GetSupportedArchitecturesUnauthorizedCode int = 401

/*
GetSupportedArchitecturesUnauthorized Unauthorized.

swagger:response getSupportedArchitecturesUnauthorized
*/
type GetSupportedArchitecturesUnauthorized struct {

	/*
	  In: Body
	*/
	Payload *models.InfraError `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesUnauthorized creates GetSupportedArchitecturesUnauthorized with default headers values
func NewGetSupportedArchitecturesUnauthorized() *GetSupportedArchitecturesUnauthorized {

	return &GetSupportedArchitecturesUnauthorized{}
}

// WithPayload adds the payload to the get supported architectures unauthorized response
func (o *GetSupportedArchitecturesUnauthorized) WithPayload(payload *models.InfraError) *GetSupportedArchitecturesUnauthorized {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures unauthorized response
func (o *GetSupportedArchitecturesUnauthorized) SetPayload(payload *models.InfraError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesUnauthorized) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(401)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetSupportedArchitecturesForbiddenCode is the HTTP code returned for type GetSupportedArchitecturesForbidden
const GetSupportedArchitecturesForbiddenCode int = 403

/*
GetSupportedArchitecturesForbidden Forbidden.

swagger:response getSupportedArchitecturesForbidden
*/
type GetSupportedArchitecturesForbidden struct {

	/*
	  In: Body
	*/
	Payload *models.InfraError `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesForbidden creates GetSupportedArchitecturesForbidden with default headers values
func NewGetSupportedArchitecturesForbidden() *GetSupportedArchitecturesForbidden {

	return &GetSupportedArchitecturesForbidden{}
}

// WithPayload adds the payload to the get supported architectures forbidden response
func (o *GetSupportedArchitecturesForbidden) WithPayload(payload *models.InfraError) *GetSupportedArchitecturesForbidden {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures forbidden response
func (o *GetSupportedArchitecturesForbidden) SetPayload(payload *models.InfraError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesForbidden) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(403)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetSupportedArchitecturesNotFoundCode is the HTTP code returned for type GetSupportedArchitecturesNotFound
const GetSupportedArchitecturesNotFoundCode int = 404

/*
GetSupportedArchitecturesNotFound Error.

swagger:response getSupportedArchitecturesNotFound
*/
type GetSupportedArchitecturesNotFound struct {

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesNotFound creates GetSupportedArchitecturesNotFound with default headers values
func NewGetSupportedArchitecturesNotFound() *GetSupportedArchitecturesNotFound {

	return &GetSupportedArchitecturesNotFound{}
}

// WithPayload adds the payload to the get supported architectures not found response
func (o *GetSupportedArchitecturesNotFound) WithPayload(payload *models.Error) *GetSupportedArchitecturesNotFound {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures not found response
func (o *GetSupportedArchitecturesNotFound) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesNotFound) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(404)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetSupportedArchitecturesServiceUnavailableCode is the HTTP code returned for type GetSupportedArchitecturesServiceUnavailable
const GetSupportedArchitecturesServiceUnavailableCode int = 503

/*
GetSupportedArchitecturesServiceUnavailable Unavailable.

swagger:response getSupportedArchitecturesServiceUnavailable
*/
type GetSupportedArchitecturesServiceUnavailable struct {

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetSupportedArchitecturesServiceUnavailable creates GetSupportedArchitecturesServiceUnavailable with default headers values
func NewGetSupportedArchitecturesServiceUnavailable() *GetSupportedArchitecturesServiceUnavailable {

	return &GetSupportedArchitecturesServiceUnavailable{}
}

// WithPayload adds the payload to the get supported architectures service unavailable response
func (o *GetSupportedArchitecturesServiceUnavailable) WithPayload(payload *models.Error) *GetSupportedArchitecturesServiceUnavailable {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get supported architectures service unavailable response
func (o *GetSupportedArchitecturesServiceUnavailable) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetSupportedArchitecturesServiceUnavailable) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(503)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
