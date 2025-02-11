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

package v1 // github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// GenericLabelServer represents the interface the manages the 'generic_label' resource.
type GenericLabelServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the account label.
	Delete(ctx context.Context, request *GenericLabelDeleteServerRequest, response *GenericLabelDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the label.
	Get(ctx context.Context, request *GenericLabelGetServerRequest, response *GenericLabelGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the account label.
	Update(ctx context.Context, request *GenericLabelUpdateServerRequest, response *GenericLabelUpdateServerResponse) error
}

// GenericLabelDeleteServerRequest is the request for the 'delete' method.
type GenericLabelDeleteServerRequest struct {
}

// GenericLabelDeleteServerResponse is the response for the 'delete' method.
type GenericLabelDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *GenericLabelDeleteServerResponse) Status(value int) *GenericLabelDeleteServerResponse {
	r.status = value
	return r
}

// GenericLabelGetServerRequest is the request for the 'get' method.
type GenericLabelGetServerRequest struct {
}

// GenericLabelGetServerResponse is the response for the 'get' method.
type GenericLabelGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
//
func (r *GenericLabelGetServerResponse) Body(value *Label) *GenericLabelGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *GenericLabelGetServerResponse) Status(value int) *GenericLabelGetServerResponse {
	r.status = value
	return r
}

// GenericLabelUpdateServerRequest is the request for the 'update' method.
type GenericLabelUpdateServerRequest struct {
	body *Label
}

// Body returns the value of the 'body' parameter.
//
//
func (r *GenericLabelUpdateServerRequest) Body() *Label {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *GenericLabelUpdateServerRequest) GetBody() (value *Label, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// GenericLabelUpdateServerResponse is the response for the 'update' method.
type GenericLabelUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
//
func (r *GenericLabelUpdateServerResponse) Body(value *Label) *GenericLabelUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *GenericLabelUpdateServerResponse) Status(value int) *GenericLabelUpdateServerResponse {
	r.status = value
	return r
}

// dispatchGenericLabel navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchGenericLabel(w http.ResponseWriter, r *http.Request, server GenericLabelServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptGenericLabelDeleteRequest(w, r, server)
			return
		case "GET":
			adaptGenericLabelGetRequest(w, r, server)
			return
		case "PATCH":
			adaptGenericLabelUpdateRequest(w, r, server)
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

// adaptGenericLabelDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptGenericLabelDeleteRequest(w http.ResponseWriter, r *http.Request, server GenericLabelServer) {
	request := &GenericLabelDeleteServerRequest{}
	err := readGenericLabelDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &GenericLabelDeleteServerResponse{}
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
	err = writeGenericLabelDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptGenericLabelGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptGenericLabelGetRequest(w http.ResponseWriter, r *http.Request, server GenericLabelServer) {
	request := &GenericLabelGetServerRequest{}
	err := readGenericLabelGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &GenericLabelGetServerResponse{}
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
	err = writeGenericLabelGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptGenericLabelUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptGenericLabelUpdateRequest(w http.ResponseWriter, r *http.Request, server GenericLabelServer) {
	request := &GenericLabelUpdateServerRequest{}
	err := readGenericLabelUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &GenericLabelUpdateServerResponse{}
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
	err = writeGenericLabelUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
