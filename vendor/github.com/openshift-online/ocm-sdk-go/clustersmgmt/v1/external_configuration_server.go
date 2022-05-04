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

// ExternalConfigurationServer represents the interface the manages the 'external_configuration' resource.
type ExternalConfigurationServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the external configuration.
	Get(ctx context.Context, request *ExternalConfigurationGetServerRequest, response *ExternalConfigurationGetServerResponse) error

	// Labels returns the target 'labels' resource.
	//
	// Reference to the resource that manages the collection of labels.
	Labels() LabelsServer

	// Syncsets returns the target 'syncsets' resource.
	//
	// Reference to the resource that manages the collection of syncsets.
	Syncsets() SyncsetsServer
}

// ExternalConfigurationGetServerRequest is the request for the 'get' method.
type ExternalConfigurationGetServerRequest struct {
}

// ExternalConfigurationGetServerResponse is the response for the 'get' method.
type ExternalConfigurationGetServerResponse struct {
	status int
	err    *errors.Error
	body   *ExternalConfiguration
}

// Body sets the value of the 'body' parameter.
//
// Retreived log.
func (r *ExternalConfigurationGetServerResponse) Body(value *ExternalConfiguration) *ExternalConfigurationGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ExternalConfigurationGetServerResponse) Status(value int) *ExternalConfigurationGetServerResponse {
	r.status = value
	return r
}

// dispatchExternalConfiguration navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchExternalConfiguration(w http.ResponseWriter, r *http.Request, server ExternalConfigurationServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptExternalConfigurationGetRequest(w, r, server)
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
		dispatchLabels(w, r, target, segments[1:])
	case "syncsets":
		target := server.Syncsets()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSyncsets(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptExternalConfigurationGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptExternalConfigurationGetRequest(w http.ResponseWriter, r *http.Request, server ExternalConfigurationServer) {
	request := &ExternalConfigurationGetServerRequest{}
	err := readExternalConfigurationGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ExternalConfigurationGetServerResponse{}
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
	err = writeExternalConfigurationGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
