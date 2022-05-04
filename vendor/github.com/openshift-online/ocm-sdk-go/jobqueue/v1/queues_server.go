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

package v1 // github.com/openshift-online/ocm-sdk-go/jobqueue/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// QueuesServer represents the interface the manages the 'queues' resource.
type QueuesServer interface {

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of job queues.
	List(ctx context.Context, request *QueuesListServerRequest, response *QueuesListServerResponse) error

	// Queue returns the target 'queue' server for the given identifier.
	//
	// Returns a reference to the service that manages a specific job queue.
	Queue(id string) QueueServer
}

// QueuesListServerRequest is the request for the 'list' method.
type QueuesListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
// Page may be a token to the next page
func (r *QueuesListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
// Page may be a token to the next page
func (r *QueuesListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *QueuesListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *QueuesListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// QueuesListServerResponse is the response for the 'list' method.
type QueuesListServerResponse struct {
	status int
	err    *errors.Error
	items  *QueueList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of job queues.
func (r *QueuesListServerResponse) Items(value *QueueList) *QueuesListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
// Page may be a token to the next page
func (r *QueuesListServerResponse) Page(value int) *QueuesListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *QueuesListServerResponse) Size(value int) *QueuesListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection regardless of the size of the page.
func (r *QueuesListServerResponse) Total(value int) *QueuesListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *QueuesListServerResponse) Status(value int) *QueuesListServerResponse {
	r.status = value
	return r
}

// dispatchQueues navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchQueues(w http.ResponseWriter, r *http.Request, server QueuesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptQueuesListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.Queue(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchQueue(w, r, target, segments[1:])
	}
}

// adaptQueuesListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptQueuesListRequest(w http.ResponseWriter, r *http.Request, server QueuesServer) {
	request := &QueuesListServerRequest{}
	err := readQueuesListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &QueuesListServerResponse{}
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
	err = writeQueuesListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
