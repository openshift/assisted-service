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

// RegistryCredentialsServer represents the interface the manages the 'registry_credentials' resource.
type RegistryCredentialsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new registry credential.
	Add(ctx context.Context, request *RegistryCredentialsAddServerRequest, response *RegistryCredentialsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of accounts.
	List(ctx context.Context, request *RegistryCredentialsListServerRequest, response *RegistryCredentialsListServerResponse) error

	// RegistryCredential returns the target 'registry_credential' server for the given identifier.
	//
	// Reference to the service that manages an specific registry credential.
	RegistryCredential(id string) RegistryCredentialServer
}

// RegistryCredentialsAddServerRequest is the request for the 'add' method.
type RegistryCredentialsAddServerRequest struct {
	body *RegistryCredential
}

// Body returns the value of the 'body' parameter.
//
// Registry credential data.
func (r *RegistryCredentialsAddServerRequest) Body() *RegistryCredential {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Registry credential data.
func (r *RegistryCredentialsAddServerRequest) GetBody() (value *RegistryCredential, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// RegistryCredentialsAddServerResponse is the response for the 'add' method.
type RegistryCredentialsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *RegistryCredential
}

// Body sets the value of the 'body' parameter.
//
// Registry credential data.
func (r *RegistryCredentialsAddServerResponse) Body(value *RegistryCredential) *RegistryCredentialsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *RegistryCredentialsAddServerResponse) Status(value int) *RegistryCredentialsAddServerResponse {
	r.status = value
	return r
}

// RegistryCredentialsListServerRequest is the request for the 'list' method.
type RegistryCredentialsListServerRequest struct {
	order  *string
	page   *int
	search *string
	size   *int
}

// Order returns the value of the 'order' parameter.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement. For example, in order to sort the
// RegistryCredentials descending by username the value should be:
//
// [source,sql]
// ----
// username desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *RegistryCredentialsListServerRequest) Order() string {
	if r != nil && r.order != nil {
		return *r.order
	}
	return ""
}

// GetOrder returns the value of the 'order' parameter and
// a flag indicating if the parameter has a value.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement. For example, in order to sort the
// RegistryCredentials descending by username the value should be:
//
// [source,sql]
// ----
// username desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *RegistryCredentialsListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RegistryCredentialsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RegistryCredentialsListServerRequest) GetPage() (value int, ok bool) {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause of a
// SQL statement, but using the names of the attributes of the RegistryCredentials instead
// of the names of the columns of a table. For example, in order to retrieve all the
// RegistryCredentials for a user the value should be:
//
// [source,sql]
// ----
// username = 'abcxyz...'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// RegistryCredentials that the user has permission to see will be returned.
func (r *RegistryCredentialsListServerRequest) Search() string {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause of a
// SQL statement, but using the names of the attributes of the RegistryCredentials instead
// of the names of the columns of a table. For example, in order to retrieve all the
// RegistryCredentials for a user the value should be:
//
// [source,sql]
// ----
// username = 'abcxyz...'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// RegistryCredentials that the user has permission to see will be returned.
func (r *RegistryCredentialsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RegistryCredentialsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *RegistryCredentialsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// RegistryCredentialsListServerResponse is the response for the 'list' method.
type RegistryCredentialsListServerResponse struct {
	status int
	err    *errors.Error
	items  *RegistryCredentialList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of registry credentials.
func (r *RegistryCredentialsListServerResponse) Items(value *RegistryCredentialList) *RegistryCredentialsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *RegistryCredentialsListServerResponse) Page(value int) *RegistryCredentialsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *RegistryCredentialsListServerResponse) Size(value int) *RegistryCredentialsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *RegistryCredentialsListServerResponse) Total(value int) *RegistryCredentialsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *RegistryCredentialsListServerResponse) Status(value int) *RegistryCredentialsListServerResponse {
	r.status = value
	return r
}

// dispatchRegistryCredentials navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchRegistryCredentials(w http.ResponseWriter, r *http.Request, server RegistryCredentialsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptRegistryCredentialsAddRequest(w, r, server)
			return
		case "GET":
			adaptRegistryCredentialsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.RegistryCredential(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchRegistryCredential(w, r, target, segments[1:])
	}
}

// adaptRegistryCredentialsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRegistryCredentialsAddRequest(w http.ResponseWriter, r *http.Request, server RegistryCredentialsServer) {
	request := &RegistryCredentialsAddServerRequest{}
	err := readRegistryCredentialsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RegistryCredentialsAddServerResponse{}
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
	err = writeRegistryCredentialsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptRegistryCredentialsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptRegistryCredentialsListRequest(w http.ResponseWriter, r *http.Request, server RegistryCredentialsServer) {
	request := &RegistryCredentialsListServerRequest{}
	err := readRegistryCredentialsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &RegistryCredentialsListServerResponse{}
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
	err = writeRegistryCredentialsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
