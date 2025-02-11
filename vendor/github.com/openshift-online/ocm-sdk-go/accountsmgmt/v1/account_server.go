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

// AccountServer represents the interface the manages the 'account' resource.
type AccountServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the account.
	Get(ctx context.Context, request *AccountGetServerRequest, response *AccountGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the account.
	Update(ctx context.Context, request *AccountUpdateServerRequest, response *AccountUpdateServerResponse) error

	// Labels returns the target 'generic_labels' resource.
	//
	// Reference to the list of labels of a specific account.
	Labels() GenericLabelsServer
}

// AccountGetServerRequest is the request for the 'get' method.
type AccountGetServerRequest struct {
}

// AccountGetServerResponse is the response for the 'get' method.
type AccountGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Account
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AccountGetServerResponse) Body(value *Account) *AccountGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AccountGetServerResponse) Status(value int) *AccountGetServerResponse {
	r.status = value
	return r
}

// AccountUpdateServerRequest is the request for the 'update' method.
type AccountUpdateServerRequest struct {
	body *Account
}

// Body returns the value of the 'body' parameter.
//
//
func (r *AccountUpdateServerRequest) Body() *Account {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *AccountUpdateServerRequest) GetBody() (value *Account, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// AccountUpdateServerResponse is the response for the 'update' method.
type AccountUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Account
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AccountUpdateServerResponse) Body(value *Account) *AccountUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AccountUpdateServerResponse) Status(value int) *AccountUpdateServerResponse {
	r.status = value
	return r
}

// dispatchAccount navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAccount(w http.ResponseWriter, r *http.Request, server AccountServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptAccountGetRequest(w, r, server)
			return
		case "PATCH":
			adaptAccountUpdateRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "labels":
		target := server.Labels()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchGenericLabels(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptAccountGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAccountGetRequest(w http.ResponseWriter, r *http.Request, server AccountServer) {
	request := &AccountGetServerRequest{}
	err := readAccountGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AccountGetServerResponse{}
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
	err = writeAccountGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAccountUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAccountUpdateRequest(w http.ResponseWriter, r *http.Request, server AccountServer) {
	request := &AccountUpdateServerRequest{}
	err := readAccountUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AccountUpdateServerResponse{}
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
	err = writeAccountUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
