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

// RolesServer represents the interface the manages the 'roles' resource.
type RolesServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new role.
	Add(ctx context.Context, request *RolesAddServerRequest, response *RolesAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of roles.
	List(ctx context.Context, request *RolesListServerRequest, response *RolesListServerResponse) error

	// Role returns the target 'role' server for the given identifier.
	//
	// Reference to the service that manages a specific role.
	Role(id string) RoleServer
}

// RolesAddServerRequest is the request for the 'add' method.
type RolesAddServerRequest struct {
	body *Role
}

// Body returns the value of the 'body' parameter.
//
// Role data.
func (r *RolesAddServerRequest) Body() *Role {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Role data.
func (r *RolesAddServerRequest) GetBody() (value *Role, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// RolesAddServerResponse is the response for the 'add' method.
type RolesAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Role
}

// Body sets the value of the 'body' parameter.
//
// Role data.
func (r *RolesAddServerResponse) Body(value *Role) *RolesAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RolesAddServerResponse) Status(value int) *RolesAddServerResponse {
	r.status = value
	return r
}

// RolesListServerRequest is the request for the 'list' method.
type RolesListServerRequest struct {
	page   *int
	search *string
	size   *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RolesListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RolesListServerRequest) GetPage() (value int, ok bool) {
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
// of an SQL statement, but using the names of the attributes of the role
// instead of the names of the columns of a table. For example, in order to
// retrieve roles named starting with `Organization`:
//
// [source,sql]
// ----
// name like 'Organization%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *RolesListServerRequest) Search() string {
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
// of an SQL statement, but using the names of the attributes of the role
// instead of the names of the columns of a table. For example, in order to
// retrieve roles named starting with `Organization`:
//
// [source,sql]
// ----
// name like 'Organization%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *RolesListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RolesListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *RolesListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// RolesListServerResponse is the response for the 'list' method.
type RolesListServerResponse struct {
	status int
	err    *errors.Error
	items  *RoleList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of roles.
func (r *RolesListServerResponse) Items(value *RoleList) *RolesListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RolesListServerResponse) Page(value int) *RolesListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RolesListServerResponse) Size(value int) *RolesListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *RolesListServerResponse) Total(value int) *RolesListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *RolesListServerResponse) Status(value int) *RolesListServerResponse {
	r.status = value
	return r
}

// dispatchRoles navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchRoles(w http.ResponseWriter, r *http.Request, server RolesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptRolesAddRequest(w, r, server)
			return
		case "GET":
			adaptRolesListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Role(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchRole(w, r, target, segments[1:])
	}
}

// adaptRolesAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRolesAddRequest(w http.ResponseWriter, r *http.Request, server RolesServer) {
	request := &RolesAddServerRequest{}
	err := readRolesAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RolesAddServerResponse{}
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
	err = writeRolesAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRolesListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRolesListRequest(w http.ResponseWriter, r *http.Request, server RolesServer) {
	request := &RolesListServerRequest{}
	err := readRolesListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RolesListServerResponse{}
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
	err = writeRolesListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
