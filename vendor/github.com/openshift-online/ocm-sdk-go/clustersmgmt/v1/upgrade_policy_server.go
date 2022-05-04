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

// UpgradePolicyServer represents the interface the manages the 'upgrade_policy' resource.
type UpgradePolicyServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the upgrade policy.
	Delete(ctx context.Context, request *UpgradePolicyDeleteServerRequest, response *UpgradePolicyDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the upgrade policy.
	Get(ctx context.Context, request *UpgradePolicyGetServerRequest, response *UpgradePolicyGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Update the upgrade policy.
	Update(ctx context.Context, request *UpgradePolicyUpdateServerRequest, response *UpgradePolicyUpdateServerResponse) error

	// State returns the target 'upgrade_policy_state' resource.
	//
	// Reference to the state of the upgrade policy.
	State() UpgradePolicyStateServer
}

// UpgradePolicyDeleteServerRequest is the request for the 'delete' method.
type UpgradePolicyDeleteServerRequest struct {
}

// UpgradePolicyDeleteServerResponse is the response for the 'delete' method.
type UpgradePolicyDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *UpgradePolicyDeleteServerResponse) Status(value int) *UpgradePolicyDeleteServerResponse {
	r.status = value
	return r
}

// UpgradePolicyGetServerRequest is the request for the 'get' method.
type UpgradePolicyGetServerRequest struct {
}

// UpgradePolicyGetServerResponse is the response for the 'get' method.
type UpgradePolicyGetServerResponse struct {
	status int
	err    *errors.Error
	body   *UpgradePolicy
}

// Body sets the value of the 'body' parameter.
//
//
func (r *UpgradePolicyGetServerResponse) Body(value *UpgradePolicy) *UpgradePolicyGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *UpgradePolicyGetServerResponse) Status(value int) *UpgradePolicyGetServerResponse {
	r.status = value
	return r
}

// UpgradePolicyUpdateServerRequest is the request for the 'update' method.
type UpgradePolicyUpdateServerRequest struct {
	body *UpgradePolicy
}

// Body returns the value of the 'body' parameter.
//
//
func (r *UpgradePolicyUpdateServerRequest) Body() *UpgradePolicy {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *UpgradePolicyUpdateServerRequest) GetBody() (value *UpgradePolicy, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// UpgradePolicyUpdateServerResponse is the response for the 'update' method.
type UpgradePolicyUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *UpgradePolicy
}

// Body sets the value of the 'body' parameter.
//
//
func (r *UpgradePolicyUpdateServerResponse) Body(value *UpgradePolicy) *UpgradePolicyUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *UpgradePolicyUpdateServerResponse) Status(value int) *UpgradePolicyUpdateServerResponse {
	r.status = value
	return r
}

// dispatchUpgradePolicy navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchUpgradePolicy(w http.ResponseWriter, r *http.Request, server UpgradePolicyServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptUpgradePolicyDeleteRequest(w, r, server)
			return
		case "GET":
			adaptUpgradePolicyGetRequest(w, r, server)
			return
		case "PATCH":
			adaptUpgradePolicyUpdateRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "state":
		target := server.State()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchUpgradePolicyState(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptUpgradePolicyDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUpgradePolicyDeleteRequest(w http.ResponseWriter, r *http.Request, server UpgradePolicyServer) {
	request := &UpgradePolicyDeleteServerRequest{}
	err := readUpgradePolicyDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UpgradePolicyDeleteServerResponse{}
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
	err = writeUpgradePolicyDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptUpgradePolicyGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUpgradePolicyGetRequest(w http.ResponseWriter, r *http.Request, server UpgradePolicyServer) {
	request := &UpgradePolicyGetServerRequest{}
	err := readUpgradePolicyGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UpgradePolicyGetServerResponse{}
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
	err = writeUpgradePolicyGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptUpgradePolicyUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUpgradePolicyUpdateRequest(w http.ResponseWriter, r *http.Request, server UpgradePolicyServer) {
	request := &UpgradePolicyUpdateServerRequest{}
	err := readUpgradePolicyUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UpgradePolicyUpdateServerResponse{}
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
	err = writeUpgradePolicyUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
