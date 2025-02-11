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

// LimitedSupportReasonServer represents the interface the manages the 'limited_support_reason' resource.
type LimitedSupportReasonServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the reason.
	Delete(ctx context.Context, request *LimitedSupportReasonDeleteServerRequest, response *LimitedSupportReasonDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the reason.
	Get(ctx context.Context, request *LimitedSupportReasonGetServerRequest, response *LimitedSupportReasonGetServerResponse) error
}

// LimitedSupportReasonDeleteServerRequest is the request for the 'delete' method.
type LimitedSupportReasonDeleteServerRequest struct {
}

// LimitedSupportReasonDeleteServerResponse is the response for the 'delete' method.
type LimitedSupportReasonDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *LimitedSupportReasonDeleteServerResponse) Status(value int) *LimitedSupportReasonDeleteServerResponse {
	r.status = value
	return r
}

// LimitedSupportReasonGetServerRequest is the request for the 'get' method.
type LimitedSupportReasonGetServerRequest struct {
}

// LimitedSupportReasonGetServerResponse is the response for the 'get' method.
type LimitedSupportReasonGetServerResponse struct {
	status int
	err    *errors.Error
	body   *LimitedSupportReason
}

// Body sets the value of the 'body' parameter.
//
//
func (r *LimitedSupportReasonGetServerResponse) Body(value *LimitedSupportReason) *LimitedSupportReasonGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LimitedSupportReasonGetServerResponse) Status(value int) *LimitedSupportReasonGetServerResponse {
	r.status = value
	return r
}

// dispatchLimitedSupportReason navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchLimitedSupportReason(w http.ResponseWriter, r *http.Request, server LimitedSupportReasonServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptLimitedSupportReasonDeleteRequest(w, r, server)
			return
		case "GET":
			adaptLimitedSupportReasonGetRequest(w, r, server)
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

// adaptLimitedSupportReasonDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLimitedSupportReasonDeleteRequest(w http.ResponseWriter, r *http.Request, server LimitedSupportReasonServer) {
	request := &LimitedSupportReasonDeleteServerRequest{}
	err := readLimitedSupportReasonDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LimitedSupportReasonDeleteServerResponse{}
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
	err = writeLimitedSupportReasonDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptLimitedSupportReasonGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLimitedSupportReasonGetRequest(w http.ResponseWriter, r *http.Request, server LimitedSupportReasonServer) {
	request := &LimitedSupportReasonGetServerRequest{}
	err := readLimitedSupportReasonGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LimitedSupportReasonGetServerResponse{}
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
	err = writeLimitedSupportReasonGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
