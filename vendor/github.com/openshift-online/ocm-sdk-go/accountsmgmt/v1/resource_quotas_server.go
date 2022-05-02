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

// ResourceQuotasServer represents the interface the manages the 'resource_quotas' resource.
type ResourceQuotasServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new resource quota.
	Add(ctx context.Context, request *ResourceQuotasAddServerRequest, response *ResourceQuotasAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of resource quotas.
	List(ctx context.Context, request *ResourceQuotasListServerRequest, response *ResourceQuotasListServerResponse) error

	// ResourceQuota returns the target 'resource_quota' server for the given identifier.
	//
	// Reference to the service that manages an specific resource quota.
	ResourceQuota(id string) ResourceQuotaServer
}

// ResourceQuotasAddServerRequest is the request for the 'add' method.
type ResourceQuotasAddServerRequest struct {
	body *ResourceQuota
}

// Body returns the value of the 'body' parameter.
//
// Resource quota data.
func (r *ResourceQuotasAddServerRequest) Body() *ResourceQuota {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Resource quota data.
func (r *ResourceQuotasAddServerRequest) GetBody() (value *ResourceQuota, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// ResourceQuotasAddServerResponse is the response for the 'add' method.
type ResourceQuotasAddServerResponse struct {
	status int
	err    *errors.Error
	body   *ResourceQuota
}

// Body sets the value of the 'body' parameter.
//
// Resource quota data.
func (r *ResourceQuotasAddServerResponse) Body(value *ResourceQuota) *ResourceQuotasAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ResourceQuotasAddServerResponse) Status(value int) *ResourceQuotasAddServerResponse {
	r.status = value
	return r
}

// ResourceQuotasListServerRequest is the request for the 'list' method.
type ResourceQuotasListServerRequest struct {
	page   *int
	search *string
	size   *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ResourceQuotasListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ResourceQuotasListServerRequest) GetPage() (value int, ok bool) {
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
// of an SQL statement, but using the names of the attributes of the account
// instead of the names of the columns of a table. For example, in order to
// retrieve resource quota with resource_type cluster.aws:
//
// [source,sql]
// ----
// resource_type = 'cluster.aws'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *ResourceQuotasListServerRequest) Search() string {
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
// of an SQL statement, but using the names of the attributes of the account
// instead of the names of the columns of a table. For example, in order to
// retrieve resource quota with resource_type cluster.aws:
//
// [source,sql]
// ----
// resource_type = 'cluster.aws'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *ResourceQuotasListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ResourceQuotasListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *ResourceQuotasListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// ResourceQuotasListServerResponse is the response for the 'list' method.
type ResourceQuotasListServerResponse struct {
	status int
	err    *errors.Error
	items  *ResourceQuotaList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of resource quotas.
func (r *ResourceQuotasListServerResponse) Items(value *ResourceQuotaList) *ResourceQuotasListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ResourceQuotasListServerResponse) Page(value int) *ResourceQuotasListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ResourceQuotasListServerResponse) Size(value int) *ResourceQuotasListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *ResourceQuotasListServerResponse) Total(value int) *ResourceQuotasListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *ResourceQuotasListServerResponse) Status(value int) *ResourceQuotasListServerResponse {
	r.status = value
	return r
}

// dispatchResourceQuotas navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchResourceQuotas(w http.ResponseWriter, r *http.Request, server ResourceQuotasServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptResourceQuotasAddRequest(w, r, server)
			return
		case "GET":
			adaptResourceQuotasListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.ResourceQuota(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchResourceQuota(w, r, target, segments[1:])
	}
}

// adaptResourceQuotasAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourceQuotasAddRequest(w http.ResponseWriter, r *http.Request, server ResourceQuotasServer) {
	request := &ResourceQuotasAddServerRequest{}
	err := readResourceQuotasAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourceQuotasAddServerResponse{}
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
	err = writeResourceQuotasAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptResourceQuotasListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourceQuotasListRequest(w http.ResponseWriter, r *http.Request, server ResourceQuotasServer) {
	request := &ResourceQuotasListServerRequest{}
	err := readResourceQuotasListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourceQuotasListServerResponse{}
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
	err = writeResourceQuotasListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
