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

// ProvisionShardsServer represents the interface the manages the 'provision_shards' resource.
type ProvisionShardsServer interface {

	// List handles a request for the 'list' method.
	//
	//
	List(ctx context.Context, request *ProvisionShardsListServerRequest, response *ProvisionShardsListServerResponse) error

	// ProvisionShard returns the target 'provision_shard' server for the given identifier.
	//
	// Reference to the resource that manages a specific provision shard.
	ProvisionShard(id string) ProvisionShardServer
}

// ProvisionShardsListServerRequest is the request for the 'list' method.
type ProvisionShardsListServerRequest struct {
	page *int
	size *int
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ProvisionShardsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ProvisionShardsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ProvisionShardsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *ProvisionShardsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// ProvisionShardsListServerResponse is the response for the 'list' method.
type ProvisionShardsListServerResponse struct {
	status int
	err    *errors.Error
	items  *ProvisionShardList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved a list of provision shards.
func (r *ProvisionShardsListServerResponse) Items(value *ProvisionShardList) *ProvisionShardsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ProvisionShardsListServerResponse) Page(value int) *ProvisionShardsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ProvisionShardsListServerResponse) Size(value int) *ProvisionShardsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection.
func (r *ProvisionShardsListServerResponse) Total(value int) *ProvisionShardsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *ProvisionShardsListServerResponse) Status(value int) *ProvisionShardsListServerResponse {
	r.status = value
	return r
}

// dispatchProvisionShards navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchProvisionShards(w http.ResponseWriter, r *http.Request, server ProvisionShardsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptProvisionShardsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.ProvisionShard(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchProvisionShard(w, r, target, segments[1:])
	}
}

// adaptProvisionShardsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptProvisionShardsListRequest(w http.ResponseWriter, r *http.Request, server ProvisionShardsServer) {
	request := &ProvisionShardsListServerRequest{}
	err := readProvisionShardsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ProvisionShardsListServerResponse{}
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
	err = writeProvisionShardsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
