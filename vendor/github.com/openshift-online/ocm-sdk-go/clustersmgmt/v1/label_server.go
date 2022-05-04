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

// LabelServer represents the interface the manages the 'label' resource.
type LabelServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the label.
	Delete(ctx context.Context, request *LabelDeleteServerRequest, response *LabelDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the label.
	Get(ctx context.Context, request *LabelGetServerRequest, response *LabelGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Update the label.
	Update(ctx context.Context, request *LabelUpdateServerRequest, response *LabelUpdateServerResponse) error
}

// LabelDeleteServerRequest is the request for the 'delete' method.
type LabelDeleteServerRequest struct {
}

// LabelDeleteServerResponse is the response for the 'delete' method.
type LabelDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *LabelDeleteServerResponse) Status(value int) *LabelDeleteServerResponse {
	r.status = value
	return r
}

// LabelGetServerRequest is the request for the 'get' method.
type LabelGetServerRequest struct {
}

// LabelGetServerResponse is the response for the 'get' method.
type LabelGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
//
func (r *LabelGetServerResponse) Body(value *Label) *LabelGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LabelGetServerResponse) Status(value int) *LabelGetServerResponse {
	r.status = value
	return r
}

// LabelUpdateServerRequest is the request for the 'update' method.
type LabelUpdateServerRequest struct {
	body *Label
}

// Body returns the value of the 'body' parameter.
//
//
func (r *LabelUpdateServerRequest) Body() *Label {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *LabelUpdateServerRequest) GetBody() (value *Label, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// LabelUpdateServerResponse is the response for the 'update' method.
type LabelUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Label
}

// Body sets the value of the 'body' parameter.
//
//
func (r *LabelUpdateServerResponse) Body(value *Label) *LabelUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LabelUpdateServerResponse) Status(value int) *LabelUpdateServerResponse {
	r.status = value
	return r
}

// dispatchLabel navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchLabel(w http.ResponseWriter, r *http.Request, server LabelServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptLabelDeleteRequest(w, r, server)
			return
		case "GET":
			adaptLabelGetRequest(w, r, server)
			return
		case "PATCH":
			adaptLabelUpdateRequest(w, r, server)
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

// adaptLabelDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLabelDeleteRequest(w http.ResponseWriter, r *http.Request, server LabelServer) {
	request := &LabelDeleteServerRequest{}
	err := readLabelDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LabelDeleteServerResponse{}
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
	err = writeLabelDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptLabelGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLabelGetRequest(w http.ResponseWriter, r *http.Request, server LabelServer) {
	request := &LabelGetServerRequest{}
	err := readLabelGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LabelGetServerResponse{}
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
	err = writeLabelGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptLabelUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLabelUpdateRequest(w http.ResponseWriter, r *http.Request, server LabelServer) {
	request := &LabelUpdateServerRequest{}
	err := readLabelUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LabelUpdateServerResponse{}
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
	err = writeLabelUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
