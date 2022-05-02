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

// OrganizationsServer represents the interface the manages the 'organizations' resource.
type OrganizationsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new organization.
	Add(ctx context.Context, request *OrganizationsAddServerRequest, response *OrganizationsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of organizations.
	List(ctx context.Context, request *OrganizationsListServerRequest, response *OrganizationsListServerResponse) error

	// Organization returns the target 'organization' server for the given identifier.
	//
	// Reference to the service that manages a specific organization.
	Organization(id string) OrganizationServer
}

// OrganizationsAddServerRequest is the request for the 'add' method.
type OrganizationsAddServerRequest struct {
	body *Organization
}

// Body returns the value of the 'body' parameter.
//
// Organization data.
func (r *OrganizationsAddServerRequest) Body() *Organization {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Organization data.
func (r *OrganizationsAddServerRequest) GetBody() (value *Organization, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// OrganizationsAddServerResponse is the response for the 'add' method.
type OrganizationsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Organization
}

// Body sets the value of the 'body' parameter.
//
// Organization data.
func (r *OrganizationsAddServerResponse) Body(value *Organization) *OrganizationsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *OrganizationsAddServerResponse) Status(value int) *OrganizationsAddServerResponse {
	r.status = value
	return r
}

// OrganizationsListServerRequest is the request for the 'list' method.
type OrganizationsListServerRequest struct {
	fetchlabelsLabels *bool
	fields            *string
	page              *int
	search            *string
	size              *int
}

// FetchlabelsLabels returns the value of the 'fetchlabels_labels' parameter.
//
// If true, includes the labels on an organization in the output. Could slow request response time.
func (r *OrganizationsListServerRequest) FetchlabelsLabels() bool {
	if r != nil && r.fetchlabelsLabels != nil {
		return *r.fetchlabelsLabels
	}
	return false
}

// GetFetchlabelsLabels returns the value of the 'fetchlabels_labels' parameter and
// a flag indicating if the parameter has a value.
//
// If true, includes the labels on an organization in the output. Could slow request response time.
func (r *OrganizationsListServerRequest) GetFetchlabelsLabels() (value bool, ok bool) {
	ok = r != nil && r.fetchlabelsLabels != nil
	if ok {
		value = *r.fetchlabelsLabels
	}
	return
}

// Fields returns the value of the 'fields' parameter.
//
// Projection
// This field contains a comma-separated list of fields you'd like to get in
// a result. No new fields can be added, only existing ones can be filtered.
// To specify a field 'id' of a structure 'plan' use 'plan.id'.
// To specify all fields of a structure 'labels' use 'labels.*'.
//
func (r *OrganizationsListServerRequest) Fields() string {
	if r != nil && r.fields != nil {
		return *r.fields
	}
	return ""
}

// GetFields returns the value of the 'fields' parameter and
// a flag indicating if the parameter has a value.
//
// Projection
// This field contains a comma-separated list of fields you'd like to get in
// a result. No new fields can be added, only existing ones can be filtered.
// To specify a field 'id' of a structure 'plan' use 'plan.id'.
// To specify all fields of a structure 'labels' use 'labels.*'.
//
func (r *OrganizationsListServerRequest) GetFields() (value string, ok bool) {
	ok = r != nil && r.fields != nil
	if ok {
		value = *r.fields
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *OrganizationsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *OrganizationsListServerRequest) GetPage() (value int, ok bool) {
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
// of an SQL statement, but using the names of the attributes of the organization
// instead of the names of the columns of a table. For example, in order to
// retrieve organizations with name starting with my:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *OrganizationsListServerRequest) Search() string {
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
// of an SQL statement, but using the names of the attributes of the organization
// instead of the names of the columns of a table. For example, in order to
// retrieve organizations with name starting with my:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *OrganizationsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *OrganizationsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *OrganizationsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// OrganizationsListServerResponse is the response for the 'list' method.
type OrganizationsListServerResponse struct {
	status int
	err    *errors.Error
	items  *OrganizationList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of organizations.
func (r *OrganizationsListServerResponse) Items(value *OrganizationList) *OrganizationsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *OrganizationsListServerResponse) Page(value int) *OrganizationsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *OrganizationsListServerResponse) Size(value int) *OrganizationsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *OrganizationsListServerResponse) Total(value int) *OrganizationsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *OrganizationsListServerResponse) Status(value int) *OrganizationsListServerResponse {
	r.status = value
	return r
}

// dispatchOrganizations navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchOrganizations(w http.ResponseWriter, r *http.Request, server OrganizationsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptOrganizationsAddRequest(w, r, server)
			return
		case "GET":
			adaptOrganizationsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Organization(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchOrganization(w, r, target, segments[1:])
	}
}

// adaptOrganizationsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptOrganizationsAddRequest(w http.ResponseWriter, r *http.Request, server OrganizationsServer) {
	request := &OrganizationsAddServerRequest{}
	err := readOrganizationsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &OrganizationsAddServerResponse{}
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
	err = writeOrganizationsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptOrganizationsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptOrganizationsListRequest(w http.ResponseWriter, r *http.Request, server OrganizationsServer) {
	request := &OrganizationsListServerRequest{}
	err := readOrganizationsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &OrganizationsListServerResponse{}
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
	err = writeOrganizationsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
