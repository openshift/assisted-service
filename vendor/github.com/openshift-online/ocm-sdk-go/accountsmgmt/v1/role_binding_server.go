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

// RoleBindingServer represents the interface the manages the 'role_binding' resource.
type RoleBindingServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the role binding.
	Delete(ctx context.Context, request *RoleBindingDeleteServerRequest, response *RoleBindingDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the role binding.
	Get(ctx context.Context, request *RoleBindingGetServerRequest, response *RoleBindingGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the account.
	Update(ctx context.Context, request *RoleBindingUpdateServerRequest, response *RoleBindingUpdateServerResponse) error
}

// RoleBindingDeleteServerRequest is the request for the 'delete' method.
type RoleBindingDeleteServerRequest struct {
}

// RoleBindingDeleteServerResponse is the response for the 'delete' method.
type RoleBindingDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *RoleBindingDeleteServerResponse) Status(value int) *RoleBindingDeleteServerResponse {
	r.status = value
	return r
}

// RoleBindingGetServerRequest is the request for the 'get' method.
type RoleBindingGetServerRequest struct {
}

// RoleBindingGetServerResponse is the response for the 'get' method.
type RoleBindingGetServerResponse struct {
	status int
	err    *errors.Error
	body   *RoleBinding
}

// Body sets the value of the 'body' parameter.
//
//
func (r *RoleBindingGetServerResponse) Body(value *RoleBinding) *RoleBindingGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RoleBindingGetServerResponse) Status(value int) *RoleBindingGetServerResponse {
	r.status = value
	return r
}

// RoleBindingUpdateServerRequest is the request for the 'update' method.
type RoleBindingUpdateServerRequest struct {
	body *RoleBinding
}

// Body returns the value of the 'body' parameter.
//
//
func (r *RoleBindingUpdateServerRequest) Body() *RoleBinding {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *RoleBindingUpdateServerRequest) GetBody() (value *RoleBinding, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// RoleBindingUpdateServerResponse is the response for the 'update' method.
type RoleBindingUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *RoleBinding
}

// Body sets the value of the 'body' parameter.
//
//
func (r *RoleBindingUpdateServerResponse) Body(value *RoleBinding) *RoleBindingUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RoleBindingUpdateServerResponse) Status(value int) *RoleBindingUpdateServerResponse {
	r.status = value
	return r
}

// dispatchRoleBinding navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchRoleBinding(w http.ResponseWriter, r *http.Request, server RoleBindingServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptRoleBindingDeleteRequest(w, r, server)
			return
		case "GET":
			adaptRoleBindingGetRequest(w, r, server)
			return
		case "PATCH":
			adaptRoleBindingUpdateRequest(w, r, server)
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

// adaptRoleBindingDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleBindingDeleteRequest(w http.ResponseWriter, r *http.Request, server RoleBindingServer) {
	request := &RoleBindingDeleteServerRequest{}
	err := readRoleBindingDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleBindingDeleteServerResponse{}
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
	err = writeRoleBindingDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRoleBindingGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleBindingGetRequest(w http.ResponseWriter, r *http.Request, server RoleBindingServer) {
	request := &RoleBindingGetServerRequest{}
	err := readRoleBindingGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleBindingGetServerResponse{}
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
	err = writeRoleBindingGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRoleBindingUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleBindingUpdateRequest(w http.ResponseWriter, r *http.Request, server RoleBindingServer) {
	request := &RoleBindingUpdateServerRequest{}
	err := readRoleBindingUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleBindingUpdateServerResponse{}
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
	err = writeRoleBindingUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
