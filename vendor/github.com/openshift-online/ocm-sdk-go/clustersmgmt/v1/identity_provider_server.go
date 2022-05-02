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

// IdentityProviderServer represents the interface the manages the 'identity_provider' resource.
type IdentityProviderServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the identity provider.
	Delete(ctx context.Context, request *IdentityProviderDeleteServerRequest, response *IdentityProviderDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the identity provider.
	Get(ctx context.Context, request *IdentityProviderGetServerRequest, response *IdentityProviderGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Update identity provider in the cluster.
	Update(ctx context.Context, request *IdentityProviderUpdateServerRequest, response *IdentityProviderUpdateServerResponse) error
}

// IdentityProviderDeleteServerRequest is the request for the 'delete' method.
type IdentityProviderDeleteServerRequest struct {
}

// IdentityProviderDeleteServerResponse is the response for the 'delete' method.
type IdentityProviderDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *IdentityProviderDeleteServerResponse) Status(value int) *IdentityProviderDeleteServerResponse {
	r.status = value
	return r
}

// IdentityProviderGetServerRequest is the request for the 'get' method.
type IdentityProviderGetServerRequest struct {
}

// IdentityProviderGetServerResponse is the response for the 'get' method.
type IdentityProviderGetServerResponse struct {
	status int
	err    *errors.Error
	body   *IdentityProvider
}

// Body sets the value of the 'body' parameter.
//
//
func (r *IdentityProviderGetServerResponse) Body(value *IdentityProvider) *IdentityProviderGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IdentityProviderGetServerResponse) Status(value int) *IdentityProviderGetServerResponse {
	r.status = value
	return r
}

// IdentityProviderUpdateServerRequest is the request for the 'update' method.
type IdentityProviderUpdateServerRequest struct {
	body *IdentityProvider
}

// Body returns the value of the 'body' parameter.
//
//
func (r *IdentityProviderUpdateServerRequest) Body() *IdentityProvider {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *IdentityProviderUpdateServerRequest) GetBody() (value *IdentityProvider, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// IdentityProviderUpdateServerResponse is the response for the 'update' method.
type IdentityProviderUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *IdentityProvider
}

// Body sets the value of the 'body' parameter.
//
//
func (r *IdentityProviderUpdateServerResponse) Body(value *IdentityProvider) *IdentityProviderUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *IdentityProviderUpdateServerResponse) Status(value int) *IdentityProviderUpdateServerResponse {
	r.status = value
	return r
}

// dispatchIdentityProvider navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchIdentityProvider(w http.ResponseWriter, r *http.Request, server IdentityProviderServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptIdentityProviderDeleteRequest(w, r, server)
			return
		case "GET":
			adaptIdentityProviderGetRequest(w, r, server)
			return
		case "PATCH":
			adaptIdentityProviderUpdateRequest(w, r, server)
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

// adaptIdentityProviderDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIdentityProviderDeleteRequest(w http.ResponseWriter, r *http.Request, server IdentityProviderServer) {
	request := &IdentityProviderDeleteServerRequest{}
	err := readIdentityProviderDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IdentityProviderDeleteServerResponse{}
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
	err = writeIdentityProviderDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIdentityProviderGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIdentityProviderGetRequest(w http.ResponseWriter, r *http.Request, server IdentityProviderServer) {
	request := &IdentityProviderGetServerRequest{}
	err := readIdentityProviderGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IdentityProviderGetServerResponse{}
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
	err = writeIdentityProviderGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptIdentityProviderUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptIdentityProviderUpdateRequest(w http.ResponseWriter, r *http.Request, server IdentityProviderServer) {
	request := &IdentityProviderUpdateServerRequest{}
	err := readIdentityProviderUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &IdentityProviderUpdateServerResponse{}
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
	err = writeIdentityProviderUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
