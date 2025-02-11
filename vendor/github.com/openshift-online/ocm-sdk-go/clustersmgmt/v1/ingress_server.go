/*
Copyright (c) 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// IMPORTANT: This file has been generated automatically, refrain from modifying it manually as all
// your changes will be lost when the file is generated again.

package v1 // github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// IngressServer represents the interface the manages the 'ingress' resource.
type IngressServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the ingress.
	Delete(ctx context.Context, request *IngressDeleteServerRequest, response *IngressDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the ingress.
	Get(ctx context.Context, request *IngressGetServerRequest, response *IngressGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the ingress.
	Update(ctx context.Context, request *IngressUpdateServerRequest, response *IngressUpdateServerResponse) error
}

// IngressDeleteServerRequest is the request for the 'delete' method.
type IngressDeleteServerRequest struct {
}

// IngressDeleteServerResponse is the response for the 'delete' method.
type IngressDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *IngressDeleteServerResponse) Status(value int) *IngressDeleteServerResponse {
	r.status = value
	return r
}

// IngressGetServerRequest is the request for the 'get' method.
type IngressGetServerRequest struct {
}

// IngressGetServerResponse is the response for the 'get' method.
type IngressGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Ingress
}

// Body sets the value of the 'body' parameter.
//
//
func (r *IngressGetServerResponse) Body(value *Ingress) *IngressGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IngressGetServerResponse) Status(value int) *IngressGetServerResponse {
	r.status = value
	return r
}

// IngressUpdateServerRequest is the request for the 'update' method.
type IngressUpdateServerRequest struct {
	body *Ingress
}

// Body returns the value of the 'body' parameter.
//
//
func (r *IngressUpdateServerRequest) Body() *Ingress {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *IngressUpdateServerRequest) GetBody() (value *Ingress, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// IngressUpdateServerResponse is the response for the 'update' method.
type IngressUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Ingress
}

// Body sets the value of the 'body' parameter.
//
//
func (r *IngressUpdateServerResponse) Body(value *Ingress) *IngressUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IngressUpdateServerResponse) Status(value int) *IngressUpdateServerResponse {
	r.status = value
	return r
}

// dispatchIngress navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchIngress(w http.ResponseWriter, r *http.Request, server IngressServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptIngressDeleteRequest(w, r, server)
			return
		case "GET":
			adaptIngressGetRequest(w, r, server)
			return
		case "PATCH":
			adaptIngressUpdateRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptIngressDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressDeleteRequest(w http.ResponseWriter, r *http.Request, server IngressServer) {
	request := &IngressDeleteServerRequest{}
	err := readIngressDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressDeleteServerResponse{}
	response.status = 204
	err = server.Delete(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeIngressDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIngressGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressGetRequest(w http.ResponseWriter, r *http.Request, server IngressServer) {
	request := &IngressGetServerRequest{}
	err := readIngressGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressGetServerResponse{}
	response.status = 200
	err = server.Get(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeIngressGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIngressUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressUpdateRequest(w http.ResponseWriter, r *http.Request, server IngressServer) {
	request := &IngressUpdateServerRequest{}
	err := readIngressUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressUpdateServerResponse{}
	response.status = 200
	err = server.Update(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeIngressUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
