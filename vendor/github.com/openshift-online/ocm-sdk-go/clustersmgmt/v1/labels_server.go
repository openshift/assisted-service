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

// LabelsServer represents the interface the manages the 'labels' resource.
type LabelsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new label to the cluster.
	Add(ctx context.Context, request *LabelsAddServerRequest, response *LabelsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of labels.
	List(ctx context.Context, request *LabelsListServerRequest, response *LabelsListServerResponse) error

	// Label returns the target 'label' server for the given identifier.
	//
	// Reference to the service that manages an specific label.
	Label(id string) LabelServer
}

// LabelsAddServerRequest is the request for the 'add' method.
type LabelsAddServerRequest struct {
	body *Label
}

// Body returns the value of the 'body' parameter.
//
// Description of the label.
func (r *LabelsAddServerRequest) Body() *Label {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the label.
func (r *LabelsAddServerRequest) GetBody() (value *Label, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// LabelsAddServerResponse is the response for the 'add' method.
type LabelsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
// Description of the label.
func (r *LabelsAddServerResponse) Body(value *Label) *LabelsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LabelsAddServerResponse) Status(value int) *LabelsAddServerResponse {
	r.status = value
	return r
}

// LabelsListServerRequest is the request for the 'list' method.
type LabelsListServerRequest struct {
	page *int
	size *int
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

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *LabelsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
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
// Number of items contained in the returned page.
func (r *LabelsListServerResponse) Size(value int) *LabelsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
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
		case "POST":
			adaptLabelsAddRequest(w, r, server)
			return
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
		target := server.Label(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchLabel(w, r, target, segments[1:])
	}
}

// adaptLabelsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLabelsAddRequest(w http.ResponseWriter, r *http.Request, server LabelsServer) {
	request := &LabelsAddServerRequest{}
	err := readLabelsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LabelsAddServerResponse{}
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
	err = writeLabelsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
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
