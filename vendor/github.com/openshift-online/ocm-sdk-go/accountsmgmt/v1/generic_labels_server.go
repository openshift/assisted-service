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

// GenericLabelsServer represents the interface the manages the 'generic_labels' resource.
type GenericLabelsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Create a new account/organization/subscription label.
	Add(ctx context.Context, request *GenericLabelsAddServerRequest, response *GenericLabelsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of labels of the account/organization/subscription.
	//
	// IMPORTANT: This collection doesn't currently support paging or searching, so the returned
	// `page` will always be 1 and `size` and `total` will always be the total number of labels
	// of the account/organization/subscription.
	List(ctx context.Context, request *GenericLabelsListServerRequest, response *GenericLabelsListServerResponse) error

	// Labels returns the target 'generic_label' server for the given identifier.
	//
	// Reference to the labels of a specific account/organization/subscription.
	Labels(id string) GenericLabelServer
}

// GenericLabelsAddServerRequest is the request for the 'add' method.
type GenericLabelsAddServerRequest struct {
	body *Label
}

// Body returns the value of the 'body' parameter.
//
// Label
func (r *GenericLabelsAddServerRequest) Body() *Label {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Label
func (r *GenericLabelsAddServerRequest) GetBody() (value *Label, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// GenericLabelsAddServerResponse is the response for the 'add' method.
type GenericLabelsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
// Label
func (r *GenericLabelsAddServerResponse) Body(value *Label) *GenericLabelsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *GenericLabelsAddServerResponse) Status(value int) *GenericLabelsAddServerResponse {
	r.status = value
	return r
}

// GenericLabelsListServerRequest is the request for the 'list' method.
type GenericLabelsListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *GenericLabelsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *GenericLabelsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// labels of the account/organization/subscription.
func (r *GenericLabelsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// labels of the account/organization/subscription.
func (r *GenericLabelsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// GenericLabelsListServerResponse is the response for the 'list' method.
type GenericLabelsListServerResponse struct {
	status int
	err    *errors.Error
	items  *LabelList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of cloud providers.
func (r *GenericLabelsListServerResponse) Items(value *LabelList) *GenericLabelsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *GenericLabelsListServerResponse) Page(value int) *GenericLabelsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// labels of the account/organization/subscription.
func (r *GenericLabelsListServerResponse) Size(value int) *GenericLabelsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page. As this collection doesn't support paging or
// searching the result will always be the total number of labels of the account/organization/subscription.
func (r *GenericLabelsListServerResponse) Total(value int) *GenericLabelsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *GenericLabelsListServerResponse) Status(value int) *GenericLabelsListServerResponse {
	r.status = value
	return r
}

// dispatchGenericLabels navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchGenericLabels(w http.ResponseWriter, r *http.Request, server GenericLabelsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptGenericLabelsAddRequest(w, r, server)
			return
		case "GET":
			adaptGenericLabelsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Labels(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchGenericLabel(w, r, target, segments[1:])
	}
}

// adaptGenericLabelsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptGenericLabelsAddRequest(w http.ResponseWriter, r *http.Request, server GenericLabelsServer) {
	request := &GenericLabelsAddServerRequest{}
	err := readGenericLabelsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &GenericLabelsAddServerResponse{}
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
	err = writeGenericLabelsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptGenericLabelsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptGenericLabelsListRequest(w http.ResponseWriter, r *http.Request, server GenericLabelsServer) {
	request := &GenericLabelsListServerRequest{}
	err := readGenericLabelsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &GenericLabelsListServerResponse{}
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
	err = writeGenericLabelsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
