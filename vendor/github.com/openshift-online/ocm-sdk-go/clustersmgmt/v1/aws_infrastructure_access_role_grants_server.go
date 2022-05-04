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

// AWSInfrastructureAccessRoleGrantsServer represents the interface the manages the 'AWS_infrastructure_access_role_grants' resource.
type AWSInfrastructureAccessRoleGrantsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Create a new AWS infrastructure access role grant and add it to the collection of
	// AWS infrastructure access role grants on the cluster.
	Add(ctx context.Context, request *AWSInfrastructureAccessRoleGrantsAddServerRequest, response *AWSInfrastructureAccessRoleGrantsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of AWS infrastructure access role grants.
	List(ctx context.Context, request *AWSInfrastructureAccessRoleGrantsListServerRequest, response *AWSInfrastructureAccessRoleGrantsListServerResponse) error

	// AWSInfrastructureAccessRoleGrant returns the target 'AWS_infrastructure_access_role_grant' server for the given identifier.
	//
	// Returns a reference to the service that manages a specific AWS infrastructure access role grant.
	AWSInfrastructureAccessRoleGrant(id string) AWSInfrastructureAccessRoleGrantServer
}

// AWSInfrastructureAccessRoleGrantsAddServerRequest is the request for the 'add' method.
type AWSInfrastructureAccessRoleGrantsAddServerRequest struct {
	body *AWSInfrastructureAccessRoleGrant
}

// Body returns the value of the 'body' parameter.
//
// Description of the AWS infrastructure access role grant.
func (r *AWSInfrastructureAccessRoleGrantsAddServerRequest) Body() *AWSInfrastructureAccessRoleGrant {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the AWS infrastructure access role grant.
func (r *AWSInfrastructureAccessRoleGrantsAddServerRequest) GetBody() (value *AWSInfrastructureAccessRoleGrant, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// AWSInfrastructureAccessRoleGrantsAddServerResponse is the response for the 'add' method.
type AWSInfrastructureAccessRoleGrantsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *AWSInfrastructureAccessRoleGrant
}

// Body sets the value of the 'body' parameter.
//
// Description of the AWS infrastructure access role grant.
func (r *AWSInfrastructureAccessRoleGrantsAddServerResponse) Body(value *AWSInfrastructureAccessRoleGrant) *AWSInfrastructureAccessRoleGrantsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *AWSInfrastructureAccessRoleGrantsAddServerResponse) Status(value int) *AWSInfrastructureAccessRoleGrantsAddServerResponse {
	r.status = value
	return r
}

// AWSInfrastructureAccessRoleGrantsListServerRequest is the request for the 'list' method.
type AWSInfrastructureAccessRoleGrantsListServerRequest struct {
	order  *string
	page   *int
	search *string
	size   *int
}

// Order returns the value of the 'order' parameter.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement, but using the names of the attributes of the AWS infrastructure access role grant
// instead of the names of the columns of a table. For example, in order to sort the
// AWS infrastructure access role grants descending by user ARN the value should be:
//
// [source,sql]
// ----
// user_arn desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) Order() string {
	if r != nil && r.order != nil {
		return *r.order
	}
	return ""
}

// GetOrder returns the value of the 'order' parameter and
// a flag indicating if the parameter has a value.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement, but using the names of the attributes of the AWS infrastructure access role grant
// instead of the names of the columns of a table. For example, in order to sort the
// AWS infrastructure access role grants descending by user ARN the value should be:
//
// [source,sql]
// ----
// user_arn desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Search returns the value of the 'search' parameter.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause of an
// SQL statement, but using the names of the attributes of the AWS infrastructure access role grant
// instead of the names of the columns of a table. For example, in order to retrieve
// all the AWS infrastructure access role grants with a user ARN starting with `user` the value should be:
//
// [source,sql]
// ----
// user_arn like '%user'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the AWS
// infrastructure access role grants that the user has permission to see will be returned.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) Search() string {
	if r != nil && r.search != nil {
		return *r.search
	}
	return ""
}

// GetSearch returns the value of the 'search' parameter and
// a flag indicating if the parameter has a value.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause of an
// SQL statement, but using the names of the attributes of the AWS infrastructure access role grant
// instead of the names of the columns of a table. For example, in order to retrieve
// all the AWS infrastructure access role grants with a user ARN starting with `user` the value should be:
//
// [source,sql]
// ----
// user_arn like '%user'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the AWS
// infrastructure access role grants that the user has permission to see will be returned.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRoleGrantsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// AWSInfrastructureAccessRoleGrantsListServerResponse is the response for the 'list' method.
type AWSInfrastructureAccessRoleGrantsListServerResponse struct {
	status int
	err    *errors.Error
	items  *AWSInfrastructureAccessRoleGrantList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of AWS infrastructure access role grants.
func (r *AWSInfrastructureAccessRoleGrantsListServerResponse) Items(value *AWSInfrastructureAccessRoleGrantList) *AWSInfrastructureAccessRoleGrantsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRoleGrantsListServerResponse) Page(value int) *AWSInfrastructureAccessRoleGrantsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRoleGrantsListServerResponse) Size(value int) *AWSInfrastructureAccessRoleGrantsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *AWSInfrastructureAccessRoleGrantsListServerResponse) Total(value int) *AWSInfrastructureAccessRoleGrantsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *AWSInfrastructureAccessRoleGrantsListServerResponse) Status(value int) *AWSInfrastructureAccessRoleGrantsListServerResponse {
	r.status = value
	return r
}

// dispatchAWSInfrastructureAccessRoleGrants navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAWSInfrastructureAccessRoleGrants(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptAWSInfrastructureAccessRoleGrantsAddRequest(w, r, server)
			return
		case "GET":
			adaptAWSInfrastructureAccessRoleGrantsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.AWSInfrastructureAccessRoleGrant(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAWSInfrastructureAccessRoleGrant(w, r, target, segments[1:])
	}
}

// adaptAWSInfrastructureAccessRoleGrantsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAWSInfrastructureAccessRoleGrantsAddRequest(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantsServer) {
	request := &AWSInfrastructureAccessRoleGrantsAddServerRequest{}
	err := readAWSInfrastructureAccessRoleGrantsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AWSInfrastructureAccessRoleGrantsAddServerResponse{}
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
	err = writeAWSInfrastructureAccessRoleGrantsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptAWSInfrastructureAccessRoleGrantsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAWSInfrastructureAccessRoleGrantsListRequest(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRoleGrantsServer) {
	request := &AWSInfrastructureAccessRoleGrantsListServerRequest{}
	err := readAWSInfrastructureAccessRoleGrantsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AWSInfrastructureAccessRoleGrantsListServerResponse{}
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
	err = writeAWSInfrastructureAccessRoleGrantsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
