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

// SyncsetServer represents the interface the manages the 'syncset' resource.
type SyncsetServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the syncset.
	Delete(ctx context.Context, request *SyncsetDeleteServerRequest, response *SyncsetDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the syncset.
	Get(ctx context.Context, request *SyncsetGetServerRequest, response *SyncsetGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Update the syncset.
	Update(ctx context.Context, request *SyncsetUpdateServerRequest, response *SyncsetUpdateServerResponse) error
}

// SyncsetDeleteServerRequest is the request for the 'delete' method.
type SyncsetDeleteServerRequest struct {
}

// SyncsetDeleteServerResponse is the response for the 'delete' method.
type SyncsetDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *SyncsetDeleteServerResponse) Status(value int) *SyncsetDeleteServerResponse {
	r.status = value
	return r
}

// SyncsetGetServerRequest is the request for the 'get' method.
type SyncsetGetServerRequest struct {
}

// SyncsetGetServerResponse is the response for the 'get' method.
type SyncsetGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Syncset
}

// Body sets the value of the 'body' parameter.
//
//
func (r *SyncsetGetServerResponse) Body(value *Syncset) *SyncsetGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *SyncsetGetServerResponse) Status(value int) *SyncsetGetServerResponse {
	r.status = value
	return r
}

// SyncsetUpdateServerRequest is the request for the 'update' method.
type SyncsetUpdateServerRequest struct {
	body *Syncset
}

// Body returns the value of the 'body' parameter.
//
//
func (r *SyncsetUpdateServerRequest) Body() *Syncset {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *SyncsetUpdateServerRequest) GetBody() (value *Syncset, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// SyncsetUpdateServerResponse is the response for the 'update' method.
type SyncsetUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Syncset
}

// Body sets the value of the 'body' parameter.
//
//
func (r *SyncsetUpdateServerResponse) Body(value *Syncset) *SyncsetUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *SyncsetUpdateServerResponse) Status(value int) *SyncsetUpdateServerResponse {
	r.status = value
	return r
}

// dispatchSyncset navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchSyncset(w http.ResponseWriter, r *http.Request, server SyncsetServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptSyncsetDeleteRequest(w, r, server)
			return
		case "GET":
			adaptSyncsetGetRequest(w, r, server)
			return
		case "PATCH":
			adaptSyncsetUpdateRequest(w, r, server)
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

// adaptSyncsetDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSyncsetDeleteRequest(w http.ResponseWriter, r *http.Request, server SyncsetServer) {
	request := &SyncsetDeleteServerRequest{}
	err := readSyncsetDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SyncsetDeleteServerResponse{}
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
	err = writeSyncsetDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptSyncsetGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSyncsetGetRequest(w http.ResponseWriter, r *http.Request, server SyncsetServer) {
	request := &SyncsetGetServerRequest{}
	err := readSyncsetGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SyncsetGetServerResponse{}
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
	err = writeSyncsetGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptSyncsetUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSyncsetUpdateRequest(w http.ResponseWriter, r *http.Request, server SyncsetServer) {
	request := &SyncsetUpdateServerRequest{}
	err := readSyncsetUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SyncsetUpdateServerResponse{}
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
	err = writeSyncsetUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
