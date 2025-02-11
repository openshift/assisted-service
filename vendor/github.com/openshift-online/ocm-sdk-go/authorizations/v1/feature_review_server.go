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

package v1 // github.com/openshift-online/ocm-sdk-go/authorizations/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// FeatureReviewServer represents the interface the manages the 'feature_review' resource.
type FeatureReviewServer interface {

	// Post handles a request for the 'post' method.
	//
	// Reviews a user's ability to toggle a feature
	Post(ctx context.Context, request *FeatureReviewPostServerRequest, response *FeatureReviewPostServerResponse) error
}

// FeatureReviewPostServerRequest is the request for the 'post' method.
type FeatureReviewPostServerRequest struct {
	request *FeatureReviewRequest
}

// Request returns the value of the 'request' parameter.
//
//
func (r *FeatureReviewPostServerRequest) Request() *FeatureReviewRequest {
	if r == nil {
		return nil
	}
	return r.request
}

// GetRequest returns the value of the 'request' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *FeatureReviewPostServerRequest) GetRequest() (value *FeatureReviewRequest, ok bool) {
	ok = r != nil && r.request != nil
	if ok {
		value = r.request
	}
	return
}

// FeatureReviewPostServerResponse is the response for the 'post' method.
type FeatureReviewPostServerResponse struct {
	status  int
	err     *errors.Error
	request *FeatureReviewResponse
}

// Request sets the value of the 'request' parameter.
//
//
func (r *FeatureReviewPostServerResponse) Request(value *FeatureReviewResponse) *FeatureReviewPostServerResponse {
	r.request = value
	return r
}

// Status sets the status code.
func (r *FeatureReviewPostServerResponse) Status(value int) *FeatureReviewPostServerResponse {
	r.status = value
	return r
}

// dispatchFeatureReview navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchFeatureReview(w http.ResponseWriter, r *http.Request, server FeatureReviewServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptFeatureReviewPostRequest(w, r, server)
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

// adaptFeatureReviewPostRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptFeatureReviewPostRequest(w http.ResponseWriter, r *http.Request, server FeatureReviewServer) {
	request := &FeatureReviewPostServerRequest{}
	err := readFeatureReviewPostRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &FeatureReviewPostServerResponse{}
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
	err = writeFeatureReviewPostResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
