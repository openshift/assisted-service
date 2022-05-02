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

// CloudProvidersServer represents the interface the manages the 'cloud_providers' resource.
type CloudProvidersServer interface {

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of cloud providers.
	List(ctx context.Context, request *CloudProvidersListServerRequest, response *CloudProvidersListServerResponse) error

	// CloudProvider returns the target 'cloud_provider' server for the given identifier.
	//
	// Returns a reference to the service that manages an specific cloud provider.
	CloudProvider(id string) CloudProviderServer
}

// CloudProvidersListServerRequest is the request for the 'list' method.
type CloudProvidersListServerRequest struct {
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
// a SQL statement, but using the names of the attributes of the cloud provider
// instead of the names of the columns of a table. For example, in order to sort the
// clusters descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *CloudProvidersListServerRequest) Order() string {
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
// a SQL statement, but using the names of the attributes of the cloud provider
// instead of the names of the columns of a table. For example, in order to sort the
// clusters descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *CloudProvidersListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *CloudProvidersListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *CloudProvidersListServerRequest) GetPage() (value int, ok bool) {
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
// SQL statement, but using the names of the attributes of the cloud provider
// instead of the names of the columns of a table. For example, in order to retrieve
// all the cloud providers with a name starting with `A` the value should be:
//
// [source,sql]
// ----
// name like 'A%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the clusters
// that the user has permission to see will be returned.
func (r *CloudProvidersListServerRequest) Search() string {
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
// SQL statement, but using the names of the attributes of the cloud provider
// instead of the names of the columns of a table. For example, in order to retrieve
// all the cloud providers with a name starting with `A` the value should be:
//
// [source,sql]
// ----
// name like 'A%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the clusters
// that the user has permission to see will be returned.
func (r *CloudProvidersListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *CloudProvidersListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *CloudProvidersListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// CloudProvidersListServerResponse is the response for the 'list' method.
type CloudProvidersListServerResponse struct {
	status int
	err    *errors.Error
	items  *CloudProviderList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of cloud providers.
func (r *CloudProvidersListServerResponse) Items(value *CloudProviderList) *CloudProvidersListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *CloudProvidersListServerResponse) Page(value int) *CloudProvidersListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *CloudProvidersListServerResponse) Size(value int) *CloudProvidersListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *CloudProvidersListServerResponse) Total(value int) *CloudProvidersListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *CloudProvidersListServerResponse) Status(value int) *CloudProvidersListServerResponse {
	r.status = value
	return r
}

// dispatchCloudProviders navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchCloudProviders(w http.ResponseWriter, r *http.Request, server CloudProvidersServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptCloudProvidersListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.CloudProvider(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchCloudProvider(w, r, target, segments[1:])
	}
}

// adaptCloudProvidersListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptCloudProvidersListRequest(w http.ResponseWriter, r *http.Request, server CloudProvidersServer) {
	request := &CloudProvidersListServerRequest{}
	err := readCloudProvidersListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &CloudProvidersListServerResponse{}
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
	err = writeCloudProvidersListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
