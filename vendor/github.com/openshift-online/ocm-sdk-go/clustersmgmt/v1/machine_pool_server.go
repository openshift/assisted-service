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

// MachinePoolServer represents the interface the manages the 'machine_pool' resource.
type MachinePoolServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the machine pool.
	Delete(ctx context.Context, request *MachinePoolDeleteServerRequest, response *MachinePoolDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the machine pool.
	Get(ctx context.Context, request *MachinePoolGetServerRequest, response *MachinePoolGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the machine pool.
	Update(ctx context.Context, request *MachinePoolUpdateServerRequest, response *MachinePoolUpdateServerResponse) error
}

// MachinePoolDeleteServerRequest is the request for the 'delete' method.
type MachinePoolDeleteServerRequest struct {
}

// MachinePoolDeleteServerResponse is the response for the 'delete' method.
type MachinePoolDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *MachinePoolDeleteServerResponse) Status(value int) *MachinePoolDeleteServerResponse {
	r.status = value
	return r
}

// MachinePoolGetServerRequest is the request for the 'get' method.
type MachinePoolGetServerRequest struct {
}

// MachinePoolGetServerResponse is the response for the 'get' method.
type MachinePoolGetServerResponse struct {
	status int
	err    *errors.Error
	body   *MachinePool
}

// Body sets the value of the 'body' parameter.
//
//
func (r *MachinePoolGetServerResponse) Body(value *MachinePool) *MachinePoolGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *MachinePoolGetServerResponse) Status(value int) *MachinePoolGetServerResponse {
	r.status = value
	return r
}

// MachinePoolUpdateServerRequest is the request for the 'update' method.
type MachinePoolUpdateServerRequest struct {
	body *MachinePool
}

// Body returns the value of the 'body' parameter.
//
//
func (r *MachinePoolUpdateServerRequest) Body() *MachinePool {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *MachinePoolUpdateServerRequest) GetBody() (value *MachinePool, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// MachinePoolUpdateServerResponse is the response for the 'update' method.
type MachinePoolUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *MachinePool
}

// Body sets the value of the 'body' parameter.
//
//
func (r *MachinePoolUpdateServerResponse) Body(value *MachinePool) *MachinePoolUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *MachinePoolUpdateServerResponse) Status(value int) *MachinePoolUpdateServerResponse {
	r.status = value
	return r
}

// dispatchMachinePool navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchMachinePool(w http.ResponseWriter, r *http.Request, server MachinePoolServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptMachinePoolDeleteRequest(w, r, server)
			return
		case "GET":
			adaptMachinePoolGetRequest(w, r, server)
			return
		case "PATCH":
			adaptMachinePoolUpdateRequest(w, r, server)
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

// adaptMachinePoolDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptMachinePoolDeleteRequest(w http.ResponseWriter, r *http.Request, server MachinePoolServer) {
	request := &MachinePoolDeleteServerRequest{}
	err := readMachinePoolDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &MachinePoolDeleteServerResponse{}
	response.status = 204
	err = server.Delete(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeMachinePoolDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptMachinePoolGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptMachinePoolGetRequest(w http.ResponseWriter, r *http.Request, server MachinePoolServer) {
	request := &MachinePoolGetServerRequest{}
	err := readMachinePoolGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &MachinePoolGetServerResponse{}
	response.status = 200
	err = server.Get(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeMachinePoolGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptMachinePoolUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptMachinePoolUpdateRequest(w http.ResponseWriter, r *http.Request, server MachinePoolServer) {
	request := &MachinePoolUpdateServerRequest{}
	err := readMachinePoolUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &MachinePoolUpdateServerResponse{}
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
	err = writeMachinePoolUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
