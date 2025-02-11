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

// UserServer represents the interface the manages the 'user' resource.
type UserServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the user.
	Delete(ctx context.Context, request *UserDeleteServerRequest, response *UserDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the user.
	Get(ctx context.Context, request *UserGetServerRequest, response *UserGetServerResponse) error
}

// UserDeleteServerRequest is the request for the 'delete' method.
type UserDeleteServerRequest struct {
}

// UserDeleteServerResponse is the response for the 'delete' method.
type UserDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *UserDeleteServerResponse) Status(value int) *UserDeleteServerResponse {
	r.status = value
	return r
}

// UserGetServerRequest is the request for the 'get' method.
type UserGetServerRequest struct {
}

// UserGetServerResponse is the response for the 'get' method.
type UserGetServerResponse struct {
	status int
	err    *errors.Error
	body   *User
}

// Body sets the value of the 'body' parameter.
//
//
func (r *UserGetServerResponse) Body(value *User) *UserGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *UserGetServerResponse) Status(value int) *UserGetServerResponse {
	r.status = value
	return r
}

// dispatchUser navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchUser(w http.ResponseWriter, r *http.Request, server UserServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptUserDeleteRequest(w, r, server)
			return
		case "GET":
			adaptUserGetRequest(w, r, server)
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

// adaptUserDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUserDeleteRequest(w http.ResponseWriter, r *http.Request, server UserServer) {
	request := &UserDeleteServerRequest{}
	err := readUserDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UserDeleteServerResponse{}
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
	err = writeUserDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptUserGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUserGetRequest(w http.ResponseWriter, r *http.Request, server UserServer) {
	request := &UserGetServerRequest{}
	err := readUserGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UserGetServerResponse{}
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
	err = writeUserGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
