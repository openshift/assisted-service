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

// TokenAuthorizationServer represents the interface the manages the 'token_authorization' resource.
type TokenAuthorizationServer interface {

	// Post handles a request for the 'post' method.
	//
	// Returns a specific account based on the given pull secret
	Post(ctx context.Context, request *TokenAuthorizationPostServerRequest, response *TokenAuthorizationPostServerResponse) error
}

// TokenAuthorizationPostServerRequest is the request for the 'post' method.
type TokenAuthorizationPostServerRequest struct {
	request *TokenAuthorizationRequest
}

// Request returns the value of the 'request' parameter.
//
//
func (r *TokenAuthorizationPostServerRequest) Request() *TokenAuthorizationRequest {
	if r == nil {
		return nil
	}
	return r.request
}

// GetRequest returns the value of the 'request' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *TokenAuthorizationPostServerRequest) GetRequest() (value *TokenAuthorizationRequest, ok bool) {
	ok = r != nil && r.request != nil
	if ok {
		value = r.request
	}
	return
}

// TokenAuthorizationPostServerResponse is the response for the 'post' method.
type TokenAuthorizationPostServerResponse struct {
	status   int
	err      *errors.Error
	response *TokenAuthorizationResponse
}

// Response sets the value of the 'response' parameter.
//
//
func (r *TokenAuthorizationPostServerResponse) Response(value *TokenAuthorizationResponse) *TokenAuthorizationPostServerResponse {
	r.response = value
	return r
}

// Status sets the status code.
func (r *TokenAuthorizationPostServerResponse) Status(value int) *TokenAuthorizationPostServerResponse {
	r.status = value
	return r
}

// dispatchTokenAuthorization navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchTokenAuthorization(w http.ResponseWriter, r *http.Request, server TokenAuthorizationServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptTokenAuthorizationPostRequest(w, r, server)
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

// adaptTokenAuthorizationPostRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptTokenAuthorizationPostRequest(w http.ResponseWriter, r *http.Request, server TokenAuthorizationServer) {
	request := &TokenAuthorizationPostServerRequest{}
	err := readTokenAuthorizationPostRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &TokenAuthorizationPostServerResponse{}
	response.status = 201
	err = server.Post(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeTokenAuthorizationPostResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
