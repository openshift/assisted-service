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

// FlavoursServer represents the interface the manages the 'flavours' resource.
type FlavoursServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new cluster flavour.
	Add(ctx context.Context, request *FlavoursAddServerRequest, response *FlavoursAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	//
	List(ctx context.Context, request *FlavoursListServerRequest, response *FlavoursListServerResponse) error

	// Flavour returns the target 'flavour' server for the given identifier.
	//
	// Reference to the resource that manages a specific flavour.
	Flavour(id string) FlavourServer
}

// FlavoursAddServerRequest is the request for the 'add' method.
type FlavoursAddServerRequest struct {
	body *Flavour
}

// Body returns the value of the 'body' parameter.
//
// Details of the cluster flavour.
func (r *FlavoursAddServerRequest) Body() *Flavour {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Details of the cluster flavour.
func (r *FlavoursAddServerRequest) GetBody() (value *Flavour, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// FlavoursAddServerResponse is the response for the 'add' method.
type FlavoursAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Flavour
}

// Body sets the value of the 'body' parameter.
//
// Details of the cluster flavour.
func (r *FlavoursAddServerResponse) Body(value *Flavour) *FlavoursAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *FlavoursAddServerResponse) Status(value int) *FlavoursAddServerResponse {
	r.status = value
	return r
}

// FlavoursListServerRequest is the request for the 'list' method.
type FlavoursListServerRequest struct {
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
// a SQL statement, but using the names of the attributes of the flavour instead of
// the names of the columns of a table. For example, in order to sort the flavours
// descending by name the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *FlavoursListServerRequest) Order() string {
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
// a SQL statement, but using the names of the attributes of the flavour instead of
// the names of the columns of a table. For example, in order to sort the flavours
// descending by name the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *FlavoursListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *FlavoursListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *FlavoursListServerRequest) GetPage() (value int, ok bool) {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause of an
// SQL statement, but using the names of the attributes of the flavour instead of
// the names of the columns of a table. For example, in order to retrieve all the
// flavours with a name starting with `my`the value should be:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the flavours
// that the user has permission to see will be returned.
func (r *FlavoursListServerRequest) Search() string {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause of an
// SQL statement, but using the names of the attributes of the flavour instead of
// the names of the columns of a table. For example, in order to retrieve all the
// flavours with a name starting with `my`the value should be:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the flavours
// that the user has permission to see will be returned.
func (r *FlavoursListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *FlavoursListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *FlavoursListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// FlavoursListServerResponse is the response for the 'list' method.
type FlavoursListServerResponse struct {
	status int
	err    *errors.Error
	items  *FlavourList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of flavours.
func (r *FlavoursListServerResponse) Items(value *FlavourList) *FlavoursListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *FlavoursListServerResponse) Page(value int) *FlavoursListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *FlavoursListServerResponse) Size(value int) *FlavoursListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *FlavoursListServerResponse) Total(value int) *FlavoursListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *FlavoursListServerResponse) Status(value int) *FlavoursListServerResponse {
	r.status = value
	return r
}

// dispatchFlavours navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchFlavours(w http.ResponseWriter, r *http.Request, server FlavoursServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptFlavoursAddRequest(w, r, server)
			return
		case "GET":
			adaptFlavoursListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Flavour(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchFlavour(w, r, target, segments[1:])
	}
}

// adaptFlavoursAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptFlavoursAddRequest(w http.ResponseWriter, r *http.Request, server FlavoursServer) {
	request := &FlavoursAddServerRequest{}
	err := readFlavoursAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &FlavoursAddServerResponse{}
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
	err = writeFlavoursAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptFlavoursListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptFlavoursListRequest(w http.ResponseWriter, r *http.Request, server FlavoursServer) {
	request := &FlavoursListServerRequest{}
	err := readFlavoursListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &FlavoursListServerResponse{}
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
	err = writeFlavoursListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
