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

// SyncsetsServer represents the interface the manages the 'syncsets' resource.
type SyncsetsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new syncset to the cluster.
	Add(ctx context.Context, request *SyncsetsAddServerRequest, response *SyncsetsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of syncsets.
	List(ctx context.Context, request *SyncsetsListServerRequest, response *SyncsetsListServerResponse) error

	// Syncset returns the target 'syncset' server for the given identifier.
	//
	// Reference to the service that manages an specific syncset.
	Syncset(id string) SyncsetServer
}

// SyncsetsAddServerRequest is the request for the 'add' method.
type SyncsetsAddServerRequest struct {
	body *Syncset
}

// Body returns the value of the 'body' parameter.
//
// Description of the syncset.
func (r *SyncsetsAddServerRequest) Body() *Syncset {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the syncset.
func (r *SyncsetsAddServerRequest) GetBody() (value *Syncset, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// SyncsetsAddServerResponse is the response for the 'add' method.
type SyncsetsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Syncset
}

// Body sets the value of the 'body' parameter.
//
// Description of the syncset.
func (r *SyncsetsAddServerResponse) Body(value *Syncset) *SyncsetsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *SyncsetsAddServerResponse) Status(value int) *SyncsetsAddServerResponse {
	r.status = value
	return r
}

// SyncsetsListServerRequest is the request for the 'list' method.
type SyncsetsListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SyncsetsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SyncsetsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *SyncsetsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *SyncsetsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// SyncsetsListServerResponse is the response for the 'list' method.
type SyncsetsListServerResponse struct {
	status int
	err    *errors.Error
	items  *SyncsetList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of syncsets.
func (r *SyncsetsListServerResponse) Items(value *SyncsetList) *SyncsetsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SyncsetsListServerResponse) Page(value int) *SyncsetsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *SyncsetsListServerResponse) Size(value int) *SyncsetsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *SyncsetsListServerResponse) Total(value int) *SyncsetsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *SyncsetsListServerResponse) Status(value int) *SyncsetsListServerResponse {
	r.status = value
	return r
}

// dispatchSyncsets navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchSyncsets(w http.ResponseWriter, r *http.Request, server SyncsetsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptSyncsetsAddRequest(w, r, server)
			return
		case "GET":
			adaptSyncsetsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Syncset(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSyncset(w, r, target, segments[1:])
	}
}

// adaptSyncsetsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSyncsetsAddRequest(w http.ResponseWriter, r *http.Request, server SyncsetsServer) {
	request := &SyncsetsAddServerRequest{}
	err := readSyncsetsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SyncsetsAddServerResponse{}
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
	err = writeSyncsetsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptSyncsetsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSyncsetsListRequest(w http.ResponseWriter, r *http.Request, server SyncsetsServer) {
	request := &SyncsetsListServerRequest{}
	err := readSyncsetsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SyncsetsListServerResponse{}
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
	err = writeSyncsetsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
