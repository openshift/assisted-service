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

// FlavourServer represents the interface the manages the 'flavour' resource.
type FlavourServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the cluster flavour.
	Get(ctx context.Context, request *FlavourGetServerRequest, response *FlavourGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the flavour.
	//
	// Attributes that can be updated are:
	//
	// - `aws.infra_volume`
	// - `aws.infra_instance_type`
	// - `gcp.infra_instance_type`
	Update(ctx context.Context, request *FlavourUpdateServerRequest, response *FlavourUpdateServerResponse) error
}

// FlavourGetServerRequest is the request for the 'get' method.
type FlavourGetServerRequest struct {
}

// FlavourGetServerResponse is the response for the 'get' method.
type FlavourGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Flavour
}

// Body sets the value of the 'body' parameter.
//
//
func (r *FlavourGetServerResponse) Body(value *Flavour) *FlavourGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *FlavourGetServerResponse) Status(value int) *FlavourGetServerResponse {
	r.status = value
	return r
}

// FlavourUpdateServerRequest is the request for the 'update' method.
type FlavourUpdateServerRequest struct {
	body *Flavour
}

// Body returns the value of the 'body' parameter.
//
//
func (r *FlavourUpdateServerRequest) Body() *Flavour {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *FlavourUpdateServerRequest) GetBody() (value *Flavour, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// FlavourUpdateServerResponse is the response for the 'update' method.
type FlavourUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Flavour
}

// Body sets the value of the 'body' parameter.
//
//
func (r *FlavourUpdateServerResponse) Body(value *Flavour) *FlavourUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *FlavourUpdateServerResponse) Status(value int) *FlavourUpdateServerResponse {
	r.status = value
	return r
}

// dispatchFlavour navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchFlavour(w http.ResponseWriter, r *http.Request, server FlavourServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptFlavourGetRequest(w, r, server)
			return
		case "PATCH":
			adaptFlavourUpdateRequest(w, r, server)
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

// adaptFlavourGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptFlavourGetRequest(w http.ResponseWriter, r *http.Request, server FlavourServer) {
	request := &FlavourGetServerRequest{}
	err := readFlavourGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &FlavourGetServerResponse{}
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
	err = writeFlavourGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptFlavourUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptFlavourUpdateRequest(w http.ResponseWriter, r *http.Request, server FlavourServer) {
	request := &FlavourUpdateServerRequest{}
	err := readFlavourUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &FlavourUpdateServerResponse{}
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
	err = writeFlavourUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
