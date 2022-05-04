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

// IdentityProvidersServer represents the interface the manages the 'identity_providers' resource.
type IdentityProvidersServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new identity provider to the cluster.
	Add(ctx context.Context, request *IdentityProvidersAddServerRequest, response *IdentityProvidersAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of identity providers.
	List(ctx context.Context, request *IdentityProvidersListServerRequest, response *IdentityProvidersListServerResponse) error

	// IdentityProvider returns the target 'identity_provider' server for the given identifier.
	//
	// Reference to the service that manages an specific identity provider.
	IdentityProvider(id string) IdentityProviderServer
}

// IdentityProvidersAddServerRequest is the request for the 'add' method.
type IdentityProvidersAddServerRequest struct {
	body *IdentityProvider
}

// Body returns the value of the 'body' parameter.
//
// Description of the cluster.
func (r *IdentityProvidersAddServerRequest) Body() *IdentityProvider {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the cluster.
func (r *IdentityProvidersAddServerRequest) GetBody() (value *IdentityProvider, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// IdentityProvidersAddServerResponse is the response for the 'add' method.
type IdentityProvidersAddServerResponse struct {
	status int
	err    *errors.Error
	body   *IdentityProvider
}

// Body sets the value of the 'body' parameter.
//
// Description of the cluster.
func (r *IdentityProvidersAddServerResponse) Body(value *IdentityProvider) *IdentityProvidersAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IdentityProvidersAddServerResponse) Status(value int) *IdentityProvidersAddServerResponse {
	r.status = value
	return r
}

// IdentityProvidersListServerRequest is the request for the 'list' method.
type IdentityProvidersListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IdentityProvidersListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IdentityProvidersListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *IdentityProvidersListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *IdentityProvidersListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// IdentityProvidersListServerResponse is the response for the 'list' method.
type IdentityProvidersListServerResponse struct {
	status int
	err    *errors.Error
	items  *IdentityProviderList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of identity providers.
func (r *IdentityProvidersListServerResponse) Items(value *IdentityProviderList) *IdentityProvidersListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IdentityProvidersListServerResponse) Page(value int) *IdentityProvidersListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *IdentityProvidersListServerResponse) Size(value int) *IdentityProvidersListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *IdentityProvidersListServerResponse) Total(value int) *IdentityProvidersListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *IdentityProvidersListServerResponse) Status(value int) *IdentityProvidersListServerResponse {
	r.status = value
	return r
}

// dispatchIdentityProviders navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchIdentityProviders(w http.ResponseWriter, r *http.Request, server IdentityProvidersServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptIdentityProvidersAddRequest(w, r, server)
			return
		case "GET":
			adaptIdentityProvidersListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.IdentityProvider(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchIdentityProvider(w, r, target, segments[1:])
	}
}

// adaptIdentityProvidersAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIdentityProvidersAddRequest(w http.ResponseWriter, r *http.Request, server IdentityProvidersServer) {
	request := &IdentityProvidersAddServerRequest{}
	err := readIdentityProvidersAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IdentityProvidersAddServerResponse{}
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
	err = writeIdentityProvidersAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIdentityProvidersListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIdentityProvidersListRequest(w http.ResponseWriter, r *http.Request, server IdentityProvidersServer) {
	request := &IdentityProvidersListServerRequest{}
	err := readIdentityProvidersListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IdentityProvidersListServerResponse{}
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
	err = writeIdentityProvidersListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
