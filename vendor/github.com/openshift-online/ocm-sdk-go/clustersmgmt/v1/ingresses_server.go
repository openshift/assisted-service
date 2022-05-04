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

// IngressesServer represents the interface the manages the 'ingresses' resource.
type IngressesServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new ingress to the cluster.
	Add(ctx context.Context, request *IngressesAddServerRequest, response *IngressesAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of ingresses.
	List(ctx context.Context, request *IngressesListServerRequest, response *IngressesListServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates all ingresses
	Update(ctx context.Context, request *IngressesUpdateServerRequest, response *IngressesUpdateServerResponse) error

	// Ingress returns the target 'ingress' server for the given identifier.
	//
	// Reference to the service that manages a specific ingress.
	Ingress(id string) IngressServer
}

// IngressesAddServerRequest is the request for the 'add' method.
type IngressesAddServerRequest struct {
	body *Ingress
}

// Body returns the value of the 'body' parameter.
//
// Description of the ingress
func (r *IngressesAddServerRequest) Body() *Ingress {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the ingress
func (r *IngressesAddServerRequest) GetBody() (value *Ingress, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// IngressesAddServerResponse is the response for the 'add' method.
type IngressesAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Ingress
}

// Body sets the value of the 'body' parameter.
//
// Description of the ingress
func (r *IngressesAddServerResponse) Body(value *Ingress) *IngressesAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IngressesAddServerResponse) Status(value int) *IngressesAddServerResponse {
	r.status = value
	return r
}

// IngressesListServerRequest is the request for the 'list' method.
type IngressesListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IngressesListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IngressesListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *IngressesListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *IngressesListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// IngressesListServerResponse is the response for the 'list' method.
type IngressesListServerResponse struct {
	status int
	err    *errors.Error
	items  *IngressList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of ingresses.
func (r *IngressesListServerResponse) Items(value *IngressList) *IngressesListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *IngressesListServerResponse) Page(value int) *IngressesListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *IngressesListServerResponse) Size(value int) *IngressesListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *IngressesListServerResponse) Total(value int) *IngressesListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *IngressesListServerResponse) Status(value int) *IngressesListServerResponse {
	r.status = value
	return r
}

// IngressesUpdateServerRequest is the request for the 'update' method.
type IngressesUpdateServerRequest struct {
	body []*Ingress
}

// Body returns the value of the 'body' parameter.
//
//
func (r *IngressesUpdateServerRequest) Body() []*Ingress {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *IngressesUpdateServerRequest) GetBody() (value []*Ingress, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// IngressesUpdateServerResponse is the response for the 'update' method.
type IngressesUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   []*Ingress
}

// Body sets the value of the 'body' parameter.
//
//
func (r *IngressesUpdateServerResponse) Body(value []*Ingress) *IngressesUpdateServerResponse {
	if value == nil {
		r.body = nil
	} else {
		r.body = make([]*Ingress, len(value))
		for i, v := range value {
			r.body[i] = v
		}
	}
	return r
}

// Status sets the status code.
func (r *IngressesUpdateServerResponse) Status(value int) *IngressesUpdateServerResponse {
	r.status = value
	return r
}

// dispatchIngresses navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchIngresses(w http.ResponseWriter, r *http.Request, server IngressesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptIngressesAddRequest(w, r, server)
			return
		case "GET":
			adaptIngressesListRequest(w, r, server)
			return
		case "PATCH":
			adaptIngressesUpdateRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Ingress(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchIngress(w, r, target, segments[1:])
	}
}

// adaptIngressesAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressesAddRequest(w http.ResponseWriter, r *http.Request, server IngressesServer) {
	request := &IngressesAddServerRequest{}
	err := readIngressesAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressesAddServerResponse{}
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
	err = writeIngressesAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIngressesListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressesListRequest(w http.ResponseWriter, r *http.Request, server IngressesServer) {
	request := &IngressesListServerRequest{}
	err := readIngressesListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressesListServerResponse{}
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
	err = writeIngressesListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIngressesUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIngressesUpdateRequest(w http.ResponseWriter, r *http.Request, server IngressesServer) {
	request := &IngressesUpdateServerRequest{}
	err := readIngressesUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IngressesUpdateServerResponse{}
	response.status = 200
	err = server.Update(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeIngressesUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
