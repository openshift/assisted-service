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

// CurrentAccountServer represents the interface the manages the 'current_account' resource.
type CurrentAccountServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the account.
	Get(ctx context.Context, request *CurrentAccountGetServerRequest, response *CurrentAccountGetServerResponse) error
}

// CurrentAccountGetServerRequest is the request for the 'get' method.
type CurrentAccountGetServerRequest struct {
}

// CurrentAccountGetServerResponse is the response for the 'get' method.
type CurrentAccountGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Account
}

// Body sets the value of the 'body' parameter.
//
//
func (r *CurrentAccountGetServerResponse) Body(value *Account) *CurrentAccountGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *CurrentAccountGetServerResponse) Status(value int) *CurrentAccountGetServerResponse {
	r.status = value
	return r
}

// dispatchCurrentAccount navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchCurrentAccount(w http.ResponseWriter, r *http.Request, server CurrentAccountServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptCurrentAccountGetRequest(w, r, server)
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

// adaptCurrentAccountGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptCurrentAccountGetRequest(w http.ResponseWriter, r *http.Request, server CurrentAccountServer) {
	request := &CurrentAccountGetServerRequest{}
	err := readCurrentAccountGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &CurrentAccountGetServerResponse{}
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
	err = writeCurrentAccountGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
