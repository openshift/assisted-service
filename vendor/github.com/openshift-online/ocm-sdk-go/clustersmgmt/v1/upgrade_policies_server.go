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

// UpgradePoliciesServer represents the interface the manages the 'upgrade_policies' resource.
type UpgradePoliciesServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new upgrade policy to the cluster.
	Add(ctx context.Context, request *UpgradePoliciesAddServerRequest, response *UpgradePoliciesAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of upgrade policies.
	List(ctx context.Context, request *UpgradePoliciesListServerRequest, response *UpgradePoliciesListServerResponse) error

	// UpgradePolicy returns the target 'upgrade_policy' server for the given identifier.
	//
	// Reference to the service that manages an specific upgrade policy.
	UpgradePolicy(id string) UpgradePolicyServer
}

// UpgradePoliciesAddServerRequest is the request for the 'add' method.
type UpgradePoliciesAddServerRequest struct {
	body *UpgradePolicy
}

// Body returns the value of the 'body' parameter.
//
// Description of the upgrade policy.
func (r *UpgradePoliciesAddServerRequest) Body() *UpgradePolicy {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the upgrade policy.
func (r *UpgradePoliciesAddServerRequest) GetBody() (value *UpgradePolicy, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// UpgradePoliciesAddServerResponse is the response for the 'add' method.
type UpgradePoliciesAddServerResponse struct {
	status int
	err    *errors.Error
	body   *UpgradePolicy
}

// Body sets the value of the 'body' parameter.
//
// Description of the upgrade policy.
func (r *UpgradePoliciesAddServerResponse) Body(value *UpgradePolicy) *UpgradePoliciesAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *UpgradePoliciesAddServerResponse) Status(value int) *UpgradePoliciesAddServerResponse {
	r.status = value
	return r
}

// UpgradePoliciesListServerRequest is the request for the 'list' method.
type UpgradePoliciesListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UpgradePoliciesListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UpgradePoliciesListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *UpgradePoliciesListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items contained in the returned page.
func (r *UpgradePoliciesListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// UpgradePoliciesListServerResponse is the response for the 'list' method.
type UpgradePoliciesListServerResponse struct {
	status int
	err    *errors.Error
	items  *UpgradePolicyList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of upgrade policy.
func (r *UpgradePoliciesListServerResponse) Items(value *UpgradePolicyList) *UpgradePoliciesListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *UpgradePoliciesListServerResponse) Page(value int) *UpgradePoliciesListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items contained in the returned page.
func (r *UpgradePoliciesListServerResponse) Size(value int) *UpgradePoliciesListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *UpgradePoliciesListServerResponse) Total(value int) *UpgradePoliciesListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *UpgradePoliciesListServerResponse) Status(value int) *UpgradePoliciesListServerResponse {
	r.status = value
	return r
}

// dispatchUpgradePolicies navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchUpgradePolicies(w http.ResponseWriter, r *http.Request, server UpgradePoliciesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptUpgradePoliciesAddRequest(w, r, server)
			return
		case "GET":
			adaptUpgradePoliciesListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.UpgradePolicy(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchUpgradePolicy(w, r, target, segments[1:])
	}
}

// adaptUpgradePoliciesAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUpgradePoliciesAddRequest(w http.ResponseWriter, r *http.Request, server UpgradePoliciesServer) {
	request := &UpgradePoliciesAddServerRequest{}
	err := readUpgradePoliciesAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UpgradePoliciesAddServerResponse{}
	response.status = 201
	err = server.Add(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeUpgradePoliciesAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptUpgradePoliciesListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptUpgradePoliciesListRequest(w http.ResponseWriter, r *http.Request, server UpgradePoliciesServer) {
	request := &UpgradePoliciesListServerRequest{}
	err := readUpgradePoliciesListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &UpgradePoliciesListServerResponse{}
	response.status = 200
	err = server.List(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeUpgradePoliciesListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
