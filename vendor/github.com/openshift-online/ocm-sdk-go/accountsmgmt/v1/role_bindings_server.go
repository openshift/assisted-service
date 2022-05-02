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

// RoleBindingsServer represents the interface the manages the 'role_bindings' resource.
type RoleBindingsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new role binding.
	Add(ctx context.Context, request *RoleBindingsAddServerRequest, response *RoleBindingsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of role bindings.
	List(ctx context.Context, request *RoleBindingsListServerRequest, response *RoleBindingsListServerResponse) error

	// RoleBinding returns the target 'role_binding' server for the given identifier.
	//
	// Reference to the service that manages a specific role binding.
	RoleBinding(id string) RoleBindingServer
}

// RoleBindingsAddServerRequest is the request for the 'add' method.
type RoleBindingsAddServerRequest struct {
	body *RoleBinding
}

// Body returns the value of the 'body' parameter.
//
// Role binding data.
func (r *RoleBindingsAddServerRequest) Body() *RoleBinding {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Role binding data.
func (r *RoleBindingsAddServerRequest) GetBody() (value *RoleBinding, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// RoleBindingsAddServerResponse is the response for the 'add' method.
type RoleBindingsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *RoleBinding
}

// Body sets the value of the 'body' parameter.
//
// Role binding data.
func (r *RoleBindingsAddServerResponse) Body(value *RoleBinding) *RoleBindingsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RoleBindingsAddServerResponse) Status(value int) *RoleBindingsAddServerResponse {
	r.status = value
	return r
}

// RoleBindingsListServerRequest is the request for the 'list' method.
type RoleBindingsListServerRequest struct {
	page   *int
	search *string
	size   *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RoleBindingsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RoleBindingsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Search returns the value of the 'search' parameter.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause
// of an SQL statement, but using the names of the attributes of the role binding
// instead of the names of the columns of a table. For example, in order to
// retrieve role bindings with role_id AuthenticatedUser:
//
// [source,sql]
// ----
// role_id = 'AuthenticatedUser'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *RoleBindingsListServerRequest) Search() string {
	if r != nil && r.search != nil {
		return *r.search
	}
	return ""
}

// GetSearch returns the value of the 'search' parameter and
// a flag indicating if the parameter has a value.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause
// of an SQL statement, but using the names of the attributes of the role binding
// instead of the names of the columns of a table. For example, in order to
// retrieve role bindings with role_id AuthenticatedUser:
//
// [source,sql]
// ----
// role_id = 'AuthenticatedUser'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *RoleBindingsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RoleBindingsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *RoleBindingsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// RoleBindingsListServerResponse is the response for the 'list' method.
type RoleBindingsListServerResponse struct {
	status int
	err    *errors.Error
	items  *RoleBindingList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of role bindings.
func (r *RoleBindingsListServerResponse) Items(value *RoleBindingList) *RoleBindingsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RoleBindingsListServerResponse) Page(value int) *RoleBindingsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RoleBindingsListServerResponse) Size(value int) *RoleBindingsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *RoleBindingsListServerResponse) Total(value int) *RoleBindingsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *RoleBindingsListServerResponse) Status(value int) *RoleBindingsListServerResponse {
	r.status = value
	return r
}

// dispatchRoleBindings navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchRoleBindings(w http.ResponseWriter, r *http.Request, server RoleBindingsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptRoleBindingsAddRequest(w, r, server)
			return
		case "GET":
			adaptRoleBindingsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.RoleBinding(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchRoleBinding(w, r, target, segments[1:])
	}
}

// adaptRoleBindingsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleBindingsAddRequest(w http.ResponseWriter, r *http.Request, server RoleBindingsServer) {
	request := &RoleBindingsAddServerRequest{}
	err := readRoleBindingsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleBindingsAddServerResponse{}
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
	err = writeRoleBindingsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRoleBindingsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRoleBindingsListRequest(w http.ResponseWriter, r *http.Request, server RoleBindingsServer) {
	request := &RoleBindingsListServerRequest{}
	err := readRoleBindingsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RoleBindingsListServerResponse{}
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
	err = writeRoleBindingsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
