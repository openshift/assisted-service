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

// AWSInfrastructureAccessRoleGrantServer represents the interface the manages the 'AWS_infrastructure_access_role_grant' resource.
type AWSInfrastructureAccessRoleGrantServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the AWS infrastructure access role grant.
	Delete(ctx context.Context, request *AWSInfrastructureAccessRoleGrantDeleteServerRequest, response *AWSInfrastructureAccessRoleGrantDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the AWS infrastructure access role grant.
	Get(ctx context.Context, request *AWSInfrastructureAccessRoleGrantGetServerRequest, response *AWSInfrastructureAccessRoleGrantGetServerResponse) error
}

// AWSInfrastructureAccessRoleGrantDeleteServerRequest is the request for the 'delete' method.
type AWSInfrastructureAccessRoleGrantDeleteServerRequest struct {
}

// AWSInfrastructureAccessRoleGrantDeleteServerResponse is the response for the 'delete' method.
type AWSInfrastructureAccessRoleGrantDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *AWSInfrastructureAccessRoleGrantDeleteServerResponse) Status(value int) *AWSInfrastructureAccessRoleGrantDeleteServerResponse {
	r.status = value
	return r
}

// AWSInfrastructureAccessRoleGrantGetServerRequest is the request for the 'get' method.
type AWSInfrastructureAccessRoleGrantGetServerRequest struct {
}

// AWSInfrastructureAccessRoleGrantGetServerResponse is the response for the 'get' method.
type AWSInfrastructureAccessRoleGrantGetServerResponse struct {
	status int
	err    *errors.Error
	body   *AWSInfrastructureAccessRoleGrant
}

// Body sets the value of the 'body' parameter.
//
//
func (r *AWSInfrastructureAccessRoleGrantGetServerResponse) Body(value *AWSInfrastructureAccessRoleGrant) *AWSInfrastructureAccessRoleGrantGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AWSInfrastructureAccessRoleGrantGetServerResponse) Status(value int) *AWSInfrastructureAccessRoleGrantGetServerResponse {
	r.status = value
	return r
}

// dispatchAWSInfrastructureAccessRoleGrant navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAWSInfrastructureAccessRoleGrant(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptAWSInfrastructureAccessRoleGrantDeleteRequest(w, r, server)
			return
		case "GET":
			adaptAWSInfrastructureAccessRoleGrantGetRequest(w, r, server)
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

// adaptAWSInfrastructureAccessRoleGrantDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAWSInfrastructureAccessRoleGrantDeleteRequest(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantServer) {
	request := &AWSInfrastructureAccessRoleGrantDeleteServerRequest{}
	err := readAWSInfrastructureAccessRoleGrantDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AWSInfrastructureAccessRoleGrantDeleteServerResponse{}
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
	err = writeAWSInfrastructureAccessRoleGrantDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAWSInfrastructureAccessRoleGrantGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAWSInfrastructureAccessRoleGrantGetRequest(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantServer) {
	request := &AWSInfrastructureAccessRoleGrantGetServerRequest{}
	err := readAWSInfrastructureAccessRoleGrantGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AWSInfrastructureAccessRoleGrantGetServerResponse{}
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
	err = writeAWSInfrastructureAccessRoleGrantGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
