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

// LabelsServer represents the interface the manages the 'labels' resource.
type LabelsServer interface {

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of labels.
	List(ctx context.Context, request *LabelsListServerRequest, response *LabelsListServerResponse) error
}

// LabelsListServerRequest is the request for the 'list' method.
type LabelsListServerRequest struct {
	page   *int
	search *string
	size   *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *LabelsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *LabelsListServerRequest) GetPage() (value int, ok bool) {
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
// retrieve labels with name starting with my:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *LabelsListServerRequest) Search() string {
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
// retrieve labels with name starting with my:
//
// [source,sql]
// ----
// name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *LabelsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *LabelsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *LabelsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// LabelsListServerResponse is the response for the 'list' method.
type LabelsListServerResponse struct {
	status int
	err    *errors.Error
	items  *LabelList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of labels.
func (r *LabelsListServerResponse) Items(value *LabelList) *LabelsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *LabelsListServerResponse) Page(value int) *LabelsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *LabelsListServerResponse) Size(value int) *LabelsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *LabelsListServerResponse) Total(value int) *LabelsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *LabelsListServerResponse) Status(value int) *LabelsListServerResponse {
	r.status = value
	return r
}

// dispatchLabels navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchLabels(w http.ResponseWriter, r *http.Request, server LabelsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptLabelsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptLabelsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLabelsListRequest(w http.ResponseWriter, r *http.Request, server LabelsServer) {
	request := &LabelsListServerRequest{}
	err := readLabelsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LabelsListServerResponse{}
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
	err = writeLabelsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
