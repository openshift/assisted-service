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

// PullSecretsServer represents the interface the manages the 'pull_secrets' resource.
type PullSecretsServer interface {

	// Post handles a request for the 'post' method.
	//
	// Returns access token generated from registries in docker format.
	Post(ctx context.Context, request *PullSecretsPostServerRequest, response *PullSecretsPostServerResponse) error

	// PullSecret returns the target 'pull_secret' server for the given identifier.
	//
	// Reference to the service that manages a specific pull secret.
	PullSecret(id string) PullSecretServer
}

// PullSecretsPostServerRequest is the request for the 'post' method.
type PullSecretsPostServerRequest struct {
	request *PullSecretsRequest
}

// Request returns the value of the 'request' parameter.
//
//
func (r *PullSecretsPostServerRequest) Request() *PullSecretsRequest {
	if r == nil {
		return nil
	}
	return r.request
}

// GetRequest returns the value of the 'request' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *PullSecretsPostServerRequest) GetRequest() (value *PullSecretsRequest, ok bool) {
	ok = r != nil && r.request != nil
	if ok {
		value = r.request
	}
	return
}

// PullSecretsPostServerResponse is the response for the 'post' method.
type PullSecretsPostServerResponse struct {
	status int
	err    *errors.Error
	body   *AccessToken
}

// Body sets the value of the 'body' parameter.
//
//
func (r *PullSecretsPostServerResponse) Body(value *AccessToken) *PullSecretsPostServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *PullSecretsPostServerResponse) Status(value int) *PullSecretsPostServerResponse {
	r.status = value
	return r
}

// dispatchPullSecrets navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchPullSecrets(w http.ResponseWriter, r *http.Request, server PullSecretsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptPullSecretsPostRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.PullSecret(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchPullSecret(w, r, target, segments[1:])
	}
}

// adaptPullSecretsPostRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptPullSecretsPostRequest(w http.ResponseWriter, r *http.Request, server PullSecretsServer) {
	request := &PullSecretsPostServerRequest{}
	err := readPullSecretsPostRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &PullSecretsPostServerResponse{}
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
	err = writePullSecretsPostResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
