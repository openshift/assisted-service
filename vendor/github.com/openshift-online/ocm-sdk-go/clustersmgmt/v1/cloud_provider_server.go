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

// CloudProviderServer represents the interface the manages the 'cloud_provider' resource.
type CloudProviderServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the cloud provider.
	Get(ctx context.Context, request *CloudProviderGetServerRequest, response *CloudProviderGetServerResponse) error

	// AvailableRegions returns the target 'available_regions' resource.
	//
	// Reference to the resource that manages the collection of available regions for
	// this cloud provider.
	AvailableRegions() AvailableRegionsServer

	// Regions returns the target 'cloud_regions' resource.
	//
	// Reference to the resource that manages the collection of regions for
	// this cloud provider.
	Regions() CloudRegionsServer
}

// CloudProviderGetServerRequest is the request for the 'get' method.
type CloudProviderGetServerRequest struct {
}

// CloudProviderGetServerResponse is the response for the 'get' method.
type CloudProviderGetServerResponse struct {
	status int
	err    *errors.Error
	body   *CloudProvider
}

// Body sets the value of the 'body' parameter.
//
//
func (r *CloudProviderGetServerResponse) Body(value *CloudProvider) *CloudProviderGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *CloudProviderGetServerResponse) Status(value int) *CloudProviderGetServerResponse {
	r.status = value
	return r
}

// dispatchCloudProvider navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchCloudProvider(w http.ResponseWriter, r *http.Request, server CloudProviderServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptCloudProviderGetRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "available_regions":
		target := server.AvailableRegions()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAvailableRegions(w, r, target, segments[1:])
	case "regions":
		target := server.Regions()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchCloudRegions(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptCloudProviderGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptCloudProviderGetRequest(w http.ResponseWriter, r *http.Request, server CloudProviderServer) {
	request := &CloudProviderGetServerRequest{}
	err := readCloudProviderGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &CloudProviderGetServerResponse{}
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
	err = writeCloudProviderGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
