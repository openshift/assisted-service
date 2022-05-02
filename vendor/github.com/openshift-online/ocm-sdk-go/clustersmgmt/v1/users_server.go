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

// UsersServer represents the interface the manages the 'users' resource.
type UsersServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new user to the group.
	Add(ctx context.Context, request *UsersAddServerRequest, response *UsersAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of users.
	List(ctx context.Context, request *UsersListServerRequest, response *UsersListServerResponse) error

	// User returns the target 'user' server for the given identifier.
	//
	// Reference to the service that manages an specific user.
	User(id string) UserServer
}

// UsersAddServerRequest is the request for the 'add' method.
type UsersAddServerRequest struct {
	body *User
}

// Body returns the value of the 'body' parameter.
//
// Description of the user.
func (r *UsersAddServerRequest) Body() *User {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the user.
func (r *UsersAddServerRequest) GetBody() (value *User, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// UsersAddServerResponse is the response for the 'add' method.
type UsersAddServerResponse struct {
	status int
	err    *errors.Error
	body   *User
}

// Body sets the value of the 'body' parameter.
//
// Description of the user.
func (r *UsersAddServerResponse) Body(value *User) *UsersAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *UsersAddServerResponse) Status(value int) *UsersAddServerResponse {
	r.status = value
	return r
}

// UsersListServerRequest is the request for the 'list' method.
type UsersListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UsersListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UsersListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *UsersListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *UsersListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// UsersListServerResponse is the response for the 'list' method.
type UsersListServerResponse struct {
	status int
	err    *errors.Error
	items  *UserList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of users.
func (r *UsersListServerResponse) Items(value *UserList) *UsersListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UsersListServerResponse) Page(value int) *UsersListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *UsersListServerResponse) Size(value int) *UsersListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *UsersListServerResponse) Total(value int) *UsersListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *UsersListServerResponse) Status(value int) *UsersListServerResponse {
	r.status = value
	return r
}

// dispatchUsers navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchUsers(w http.ResponseWriter, r *http.Request, server UsersServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptUsersAddRequest(w, r, server)
			return
		case "GET":
			adaptUsersListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.User(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchUser(w, r, target, segments[1:])
	}
}

// adaptUsersAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUsersAddRequest(w http.ResponseWriter, r *http.Request, server UsersServer) {
	request := &UsersAddServerRequest{}
	err := readUsersAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UsersAddServerResponse{}
	response.status = 201
	err = server.Add(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeUsersAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptUsersListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUsersListRequest(w http.ResponseWriter, r *http.Request, server UsersServer) {
	request := &UsersListServerRequest{}
	err := readUsersListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UsersListServerResponse{}
	response.status = 200
	err = server.List(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeUsersListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
