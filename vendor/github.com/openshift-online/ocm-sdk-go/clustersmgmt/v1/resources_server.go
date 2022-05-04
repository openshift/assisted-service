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

// ResourcesServer represents the interface the manages the 'resources' resource.
type ResourcesServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves a list of resources for a cluster in error state
	Get(ctx context.Context, request *ResourcesGetServerRequest, response *ResourcesGetServerResponse) error

	// Live returns the target 'resource' resource.
	//
	// Retrieves a list of currently available resources for a cluster
	Live() ResourceServer
}

// ResourcesGetServerRequest is the request for the 'get' method.
type ResourcesGetServerRequest struct {
}

// ResourcesGetServerResponse is the response for the 'get' method.
type ResourcesGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Resource
}

// Body sets the value of the 'body' parameter.
//
// List of cluster resources
func (r *ResourcesGetServerResponse) Body(value *Resource) *ResourcesGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ResourcesGetServerResponse) Status(value int) *ResourcesGetServerResponse {
	r.status = value
	return r
}

// dispatchResources navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchResources(w http.ResponseWriter, r *http.Request, server ResourcesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptResourcesGetRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "live":
		target := server.Live()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchResource(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptResourcesGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptResourcesGetRequest(w http.ResponseWriter, r *http.Request, server ResourcesServer) {
	request := &ResourcesGetServerRequest{}
	err := readResourcesGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ResourcesGetServerResponse{}
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
	err = writeResourcesGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
