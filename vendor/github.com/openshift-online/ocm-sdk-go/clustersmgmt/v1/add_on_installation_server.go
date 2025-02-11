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

// AddOnInstallationServer represents the interface the manages the 'add_on_installation' resource.
type AddOnInstallationServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Delete an add-on installation and remove it from the collection of add-on installations on the cluster.
	Delete(ctx context.Context, request *AddOnInstallationDeleteServerRequest, response *AddOnInstallationDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the add-on installation.
	Get(ctx context.Context, request *AddOnInstallationGetServerRequest, response *AddOnInstallationGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the add-on installation.
	Update(ctx context.Context, request *AddOnInstallationUpdateServerRequest, response *AddOnInstallationUpdateServerResponse) error
}

// AddOnInstallationDeleteServerRequest is the request for the 'delete' method.
type AddOnInstallationDeleteServerRequest struct {
}

// AddOnInstallationDeleteServerResponse is the response for the 'delete' method.
type AddOnInstallationDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *AddOnInstallationDeleteServerResponse) Status(value int) *AddOnInstallationDeleteServerResponse {
	r.status = value
	return r
}

// AddOnInstallationGetServerRequest is the request for the 'get' method.
type AddOnInstallationGetServerRequest struct {
}

// AddOnInstallationGetServerResponse is the response for the 'get' method.
type AddOnInstallationGetServerResponse struct {
	status int
	err    *errors.Error
	body   *AddOnInstallation
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AddOnInstallationGetServerResponse) Body(value *AddOnInstallation) *AddOnInstallationGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AddOnInstallationGetServerResponse) Status(value int) *AddOnInstallationGetServerResponse {
	r.status = value
	return r
}

// AddOnInstallationUpdateServerRequest is the request for the 'update' method.
type AddOnInstallationUpdateServerRequest struct {
	body *AddOnInstallation
}

// Body returns the value of the 'body' parameter.
//
//
func (r *AddOnInstallationUpdateServerRequest) Body() *AddOnInstallation {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *AddOnInstallationUpdateServerRequest) GetBody() (value *AddOnInstallation, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// AddOnInstallationUpdateServerResponse is the response for the 'update' method.
type AddOnInstallationUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *AddOnInstallation
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AddOnInstallationUpdateServerResponse) Body(value *AddOnInstallation) *AddOnInstallationUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AddOnInstallationUpdateServerResponse) Status(value int) *AddOnInstallationUpdateServerResponse {
	r.status = value
	return r
}

// dispatchAddOnInstallation navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAddOnInstallation(w http.ResponseWriter, r *http.Request, server AddOnInstallationServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptAddOnInstallationDeleteRequest(w, r, server)
			return
		case "GET":
			adaptAddOnInstallationGetRequest(w, r, server)
			return
		case "PATCH":
			adaptAddOnInstallationUpdateRequest(w, r, server)
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

// adaptAddOnInstallationDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnInstallationDeleteRequest(w http.ResponseWriter, r *http.Request, server AddOnInstallationServer) {
	request := &AddOnInstallationDeleteServerRequest{}
	err := readAddOnInstallationDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnInstallationDeleteServerResponse{}
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
	err = writeAddOnInstallationDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAddOnInstallationGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnInstallationGetRequest(w http.ResponseWriter, r *http.Request, server AddOnInstallationServer) {
	request := &AddOnInstallationGetServerRequest{}
	err := readAddOnInstallationGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnInstallationGetServerResponse{}
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
	err = writeAddOnInstallationGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAddOnInstallationUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddOnInstallationUpdateRequest(w http.ResponseWriter, r *http.Request, server AddOnInstallationServer) {
	request := &AddOnInstallationUpdateServerRequest{}
	err := readAddOnInstallationUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddOnInstallationUpdateServerResponse{}
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
	err = writeAddOnInstallationUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
