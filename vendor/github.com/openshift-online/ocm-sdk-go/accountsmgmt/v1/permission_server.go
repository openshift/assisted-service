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

// PermissionServer represents the interface the manages the 'permission' resource.
type PermissionServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the permission.
	Delete(ctx context.Context, request *PermissionDeleteServerRequest, response *PermissionDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the permission.
	Get(ctx context.Context, request *PermissionGetServerRequest, response *PermissionGetServerResponse) error
}

// PermissionDeleteServerRequest is the request for the 'delete' method.
type PermissionDeleteServerRequest struct {
}

// PermissionDeleteServerResponse is the response for the 'delete' method.
type PermissionDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *PermissionDeleteServerResponse) Status(value int) *PermissionDeleteServerResponse {
	r.status = value
	return r
}

// PermissionGetServerRequest is the request for the 'get' method.
type PermissionGetServerRequest struct {
}

// PermissionGetServerResponse is the response for the 'get' method.
type PermissionGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Permission
}

// Body sets the value of the 'body' parameter.
//
//
func (r *PermissionGetServerResponse) Body(value *Permission) *PermissionGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *PermissionGetServerResponse) Status(value int) *PermissionGetServerResponse {
	r.status = value
	return r
}

// dispatchPermission navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchPermission(w http.ResponseWriter, r *http.Request, server PermissionServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptPermissionDeleteRequest(w, r, server)
			return
		case "GET":
			adaptPermissionGetRequest(w, r, server)
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

// adaptPermissionDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptPermissionDeleteRequest(w http.ResponseWriter, r *http.Request, server PermissionServer) {
	request := &PermissionDeleteServerRequest{}
	err := readPermissionDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &PermissionDeleteServerResponse{}
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
	err = writePermissionDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptPermissionGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptPermissionGetRequest(w http.ResponseWriter, r *http.Request, server PermissionServer) {
	request := &PermissionGetServerRequest{}
	err := readPermissionGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &PermissionGetServerResponse{}
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
	err = writePermissionGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
