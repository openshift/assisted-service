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

// RoleServer represents the interface the manages the 'role' resource.
type RoleServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the role.
	Delete(ctx context.Context, request *RoleDeleteServerRequest, response *RoleDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the role.
	Get(ctx context.Context, request *RoleGetServerRequest, response *RoleGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the role.
	Update(ctx context.Context, request *RoleUpdateServerRequest, response *RoleUpdateServerResponse) error
}

// RoleDeleteServerRequest is the request for the 'delete' method.
type RoleDeleteServerRequest struct {
}

// RoleDeleteServerResponse is the response for the 'delete' method.
type RoleDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *RoleDeleteServerResponse) Status(value int) *RoleDeleteServerResponse {
	r.status = value
	return r
}

// RoleGetServerRequest is the request for the 'get' method.
type RoleGetServerRequest struct {
}

// RoleGetServerResponse is the response for the 'get' method.
type RoleGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Role
}

// Body sets the value of the 'body' parameter.
//
//
func (r *RoleGetServerResponse) Body(value *Role) *RoleGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RoleGetServerResponse) Status(value int) *RoleGetServerResponse {
	r.status = value
	return r
}

// RoleUpdateServerRequest is the request for the 'update' method.
type RoleUpdateServerRequest struct {
	body *Role
}

// Body returns the value of the 'body' parameter.
//
//
func (r *RoleUpdateServerRequest) Body() *Role {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *RoleUpdateServerRequest) GetBody() (value *Role, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// RoleUpdateServerResponse is the response for the 'update' method.
type RoleUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Role
}

// Body sets the value of the 'body' parameter.
//
//
func (r *RoleUpdateServerResponse) Body(value *Role) *RoleUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RoleUpdateServerResponse) Status(value int) *RoleUpdateServerResponse {
	r.status = value
	return r
}

// dispatchRole navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchRole(w http.ResponseWriter, r *http.Request, server RoleServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptRoleDeleteRequest(w, r, server)
			return
		case "GET":
			adaptRoleGetRequest(w, r, server)
			return
		case "PATCH":
			adaptRoleUpdateRequest(w, r, server)
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

// adaptRoleDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleDeleteRequest(w http.ResponseWriter, r *http.Request, server RoleServer) {
	request := &RoleDeleteServerRequest{}
	err := readRoleDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleDeleteServerResponse{}
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
	err = writeRoleDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRoleGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleGetRequest(w http.ResponseWriter, r *http.Request, server RoleServer) {
	request := &RoleGetServerRequest{}
	err := readRoleGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleGetServerResponse{}
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
	err = writeRoleGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRoleUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleUpdateRequest(w http.ResponseWriter, r *http.Request, server RoleServer) {
	request := &RoleUpdateServerRequest{}
	err := readRoleUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleUpdateServerResponse{}
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
	err = writeRoleUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
