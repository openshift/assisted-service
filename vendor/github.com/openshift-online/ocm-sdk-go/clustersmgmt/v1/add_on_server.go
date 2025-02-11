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

// AddOnServer represents the interface the manages the 'add_on' resource.
type AddOnServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the add-on.
	Delete(ctx context.Context, request *AddOnDeleteServerRequest, response *AddOnDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the add-on.
	Get(ctx context.Context, request *AddOnGetServerRequest, response *AddOnGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the add-on.
	Update(ctx context.Context, request *AddOnUpdateServerRequest, response *AddOnUpdateServerResponse) error
}

// AddOnDeleteServerRequest is the request for the 'delete' method.
type AddOnDeleteServerRequest struct {
}

// AddOnDeleteServerResponse is the response for the 'delete' method.
type AddOnDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *AddOnDeleteServerResponse) Status(value int) *AddOnDeleteServerResponse {
	r.status = value
	return r
}

// AddOnGetServerRequest is the request for the 'get' method.
type AddOnGetServerRequest struct {
}

// AddOnGetServerResponse is the response for the 'get' method.
type AddOnGetServerResponse struct {
	status int
	err    *errors.Error
	body   *AddOn
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AddOnGetServerResponse) Body(value *AddOn) *AddOnGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AddOnGetServerResponse) Status(value int) *AddOnGetServerResponse {
	r.status = value
	return r
}

// AddOnUpdateServerRequest is the request for the 'update' method.
type AddOnUpdateServerRequest struct {
	body *AddOn
}

// Body returns the value of the 'body' parameter.
//
//
func (r *AddOnUpdateServerRequest) Body() *AddOn {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *AddOnUpdateServerRequest) GetBody() (value *AddOn, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// AddOnUpdateServerResponse is the response for the 'update' method.
type AddOnUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *AddOn
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AddOnUpdateServerResponse) Body(value *AddOn) *AddOnUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AddOnUpdateServerResponse) Status(value int) *AddOnUpdateServerResponse {
	r.status = value
	return r
}

// dispatchAddOn navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAddOn(w http.ResponseWriter, r *http.Request, server AddOnServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptAddOnDeleteRequest(w, r, server)
			return
		case "GET":
			adaptAddOnGetRequest(w, r, server)
			return
		case "PATCH":
			adaptAddOnUpdateRequest(w, r, server)
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

// adaptAddOnDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnDeleteRequest(w http.ResponseWriter, r *http.Request, server AddOnServer) {
	request := &AddOnDeleteServerRequest{}
	err := readAddOnDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnDeleteServerResponse{}
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
	err = writeAddOnDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAddOnGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnGetRequest(w http.ResponseWriter, r *http.Request, server AddOnServer) {
	request := &AddOnGetServerRequest{}
	err := readAddOnGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnGetServerResponse{}
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
	err = writeAddOnGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAddOnUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnUpdateRequest(w http.ResponseWriter, r *http.Request, server AddOnServer) {
	request := &AddOnUpdateServerRequest{}
	err := readAddOnUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnUpdateServerResponse{}
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
	err = writeAddOnUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
