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

// MachinePoolsServer represents the interface the manages the 'machine_pools' resource.
type MachinePoolsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new machine pool to the cluster.
	Add(ctx context.Context, request *MachinePoolsAddServerRequest, response *MachinePoolsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of machine pools.
	List(ctx context.Context, request *MachinePoolsListServerRequest, response *MachinePoolsListServerResponse) error

	// MachinePool returns the target 'machine_pool' server for the given identifier.
	//
	// Reference to the service that manages a specific machine pool.
	MachinePool(id string) MachinePoolServer
}

// MachinePoolsAddServerRequest is the request for the 'add' method.
type MachinePoolsAddServerRequest struct {
	body *MachinePool
}

// Body returns the value of the 'body' parameter.
//
// Description of the machine pool
func (r *MachinePoolsAddServerRequest) Body() *MachinePool {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the machine pool
func (r *MachinePoolsAddServerRequest) GetBody() (value *MachinePool, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// MachinePoolsAddServerResponse is the response for the 'add' method.
type MachinePoolsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *MachinePool
}

// Body sets the value of the 'body' parameter.
//
// Description of the machine pool
func (r *MachinePoolsAddServerResponse) Body(value *MachinePool) *MachinePoolsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *MachinePoolsAddServerResponse) Status(value int) *MachinePoolsAddServerResponse {
	r.status = value
	return r
}

// MachinePoolsListServerRequest is the request for the 'list' method.
type MachinePoolsListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *MachinePoolsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *MachinePoolsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *MachinePoolsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *MachinePoolsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// MachinePoolsListServerResponse is the response for the 'list' method.
type MachinePoolsListServerResponse struct {
	status int
	err    *errors.Error
	items  *MachinePoolList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of machine pools.
func (r *MachinePoolsListServerResponse) Items(value *MachinePoolList) *MachinePoolsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *MachinePoolsListServerResponse) Page(value int) *MachinePoolsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *MachinePoolsListServerResponse) Size(value int) *MachinePoolsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *MachinePoolsListServerResponse) Total(value int) *MachinePoolsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *MachinePoolsListServerResponse) Status(value int) *MachinePoolsListServerResponse {
	r.status = value
	return r
}

// dispatchMachinePools navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchMachinePools(w http.ResponseWriter, r *http.Request, server MachinePoolsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptMachinePoolsAddRequest(w, r, server)
			return
		case "GET":
			adaptMachinePoolsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.MachinePool(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchMachinePool(w, r, target, segments[1:])
	}
}

// adaptMachinePoolsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptMachinePoolsAddRequest(w http.ResponseWriter, r *http.Request, server MachinePoolsServer) {
	request := &MachinePoolsAddServerRequest{}
	err := readMachinePoolsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &MachinePoolsAddServerResponse{}
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
	err = writeMachinePoolsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptMachinePoolsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptMachinePoolsListRequest(w http.ResponseWriter, r *http.Request, server MachinePoolsServer) {
	request := &MachinePoolsListServerRequest{}
	err := readMachinePoolsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &MachinePoolsListServerResponse{}
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
	err = writeMachinePoolsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
