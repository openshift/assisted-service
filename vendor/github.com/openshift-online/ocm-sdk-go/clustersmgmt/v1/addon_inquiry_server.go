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

// AddonInquiryServer represents the interface the manages the 'addon_inquiry' resource.
type AddonInquiryServer interface {

	// Get handles a request for the 'get' method.
	//
	//
	Get(ctx context.Context, request *AddonInquiryGetServerRequest, response *AddonInquiryGetServerResponse) error
}

// AddonInquiryGetServerRequest is the request for the 'get' method.
type AddonInquiryGetServerRequest struct {
}

// AddonInquiryGetServerResponse is the response for the 'get' method.
type AddonInquiryGetServerResponse struct {
	status int
	err    *errors.Error
	body   *AddOn
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AddonInquiryGetServerResponse) Body(value *AddOn) *AddonInquiryGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AddonInquiryGetServerResponse) Status(value int) *AddonInquiryGetServerResponse {
	r.status = value
	return r
}

// dispatchAddonInquiry navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAddonInquiry(w http.ResponseWriter, r *http.Request, server AddonInquiryServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptAddonInquiryGetRequest(w, r, server)
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

// adaptAddonInquiryGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAddonInquiryGetRequest(w http.ResponseWriter, r *http.Request, server AddonInquiryServer) {
	request := &AddonInquiryGetServerRequest{}
	err := readAddonInquiryGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AddonInquiryGetServerResponse{}
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
	err = writeAddonInquiryGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
