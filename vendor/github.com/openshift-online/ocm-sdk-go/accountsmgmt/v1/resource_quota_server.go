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

// ResourceQuotaServer represents the interface the manages the 'resource_quota' resource.
type ResourceQuotaServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the resource quota.
	Delete(ctx context.Context, request *ResourceQuotaDeleteServerRequest, response *ResourceQuotaDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the resource quota.
	Get(ctx context.Context, request *ResourceQuotaGetServerRequest, response *ResourceQuotaGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the resource quota.
	Update(ctx context.Context, request *ResourceQuotaUpdateServerRequest, response *ResourceQuotaUpdateServerResponse) error
}

// ResourceQuotaDeleteServerRequest is the request for the 'delete' method.
type ResourceQuotaDeleteServerRequest struct {
}

// ResourceQuotaDeleteServerResponse is the response for the 'delete' method.
type ResourceQuotaDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *ResourceQuotaDeleteServerResponse) Status(value int) *ResourceQuotaDeleteServerResponse {
	r.status = value
	return r
}

// ResourceQuotaGetServerRequest is the request for the 'get' method.
type ResourceQuotaGetServerRequest struct {
}

// ResourceQuotaGetServerResponse is the response for the 'get' method.
type ResourceQuotaGetServerResponse struct {
	status int
	err    *errors.Error
	body   *ResourceQuota
}

// Body sets the value of the 'body' parameter.
//
//
func (r *ResourceQuotaGetServerResponse) Body(value *ResourceQuota) *ResourceQuotaGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ResourceQuotaGetServerResponse) Status(value int) *ResourceQuotaGetServerResponse {
	r.status = value
	return r
}

// ResourceQuotaUpdateServerRequest is the request for the 'update' method.
type ResourceQuotaUpdateServerRequest struct {
	body *ResourceQuota
}

// Body returns the value of the 'body' parameter.
//
//
func (r *ResourceQuotaUpdateServerRequest) Body() *ResourceQuota {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *ResourceQuotaUpdateServerRequest) GetBody() (value *ResourceQuota, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// ResourceQuotaUpdateServerResponse is the response for the 'update' method.
type ResourceQuotaUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *ResourceQuota
}

// Body sets the value of the 'body' parameter.
//
//
func (r *ResourceQuotaUpdateServerResponse) Body(value *ResourceQuota) *ResourceQuotaUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ResourceQuotaUpdateServerResponse) Status(value int) *ResourceQuotaUpdateServerResponse {
	r.status = value
	return r
}

// dispatchResourceQuota navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchResourceQuota(w http.ResponseWriter, r *http.Request, server ResourceQuotaServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptResourceQuotaDeleteRequest(w, r, server)
			return
		case "GET":
			adaptResourceQuotaGetRequest(w, r, server)
			return
		case "PATCH":
			adaptResourceQuotaUpdateRequest(w, r, server)
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

// adaptResourceQuotaDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourceQuotaDeleteRequest(w http.ResponseWriter, r *http.Request, server ResourceQuotaServer) {
	request := &ResourceQuotaDeleteServerRequest{}
	err := readResourceQuotaDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourceQuotaDeleteServerResponse{}
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
	err = writeResourceQuotaDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptResourceQuotaGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourceQuotaGetRequest(w http.ResponseWriter, r *http.Request, server ResourceQuotaServer) {
	request := &ResourceQuotaGetServerRequest{}
	err := readResourceQuotaGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourceQuotaGetServerResponse{}
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
	err = writeResourceQuotaGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptResourceQuotaUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourceQuotaUpdateRequest(w http.ResponseWriter, r *http.Request, server ResourceQuotaServer) {
	request := &ResourceQuotaUpdateServerRequest{}
	err := readResourceQuotaUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourceQuotaUpdateServerResponse{}
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
	err = writeResourceQuotaUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
