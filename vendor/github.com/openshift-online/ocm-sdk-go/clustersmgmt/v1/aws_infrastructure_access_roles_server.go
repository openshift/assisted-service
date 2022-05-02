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

// AWSInfrastructureAccessRolesServer represents the interface the manages the 'AWS_infrastructure_access_roles' resource.
type AWSInfrastructureAccessRolesServer interface {

	// List handles a request for the 'list' method.
	//
	//
	List(ctx context.Context, request *AWSInfrastructureAccessRolesListServerRequest, response *AWSInfrastructureAccessRolesListServerResponse) error

	// AWSInfrastructureAccessRole returns the target 'AWS_infrastructure_access_role' server for the given identifier.
	//
	// Reference to the resource that manages a specific role.
	AWSInfrastructureAccessRole(id string) AWSInfrastructureAccessRoleServer
}

// AWSInfrastructureAccessRolesListServerRequest is the request for the 'list' method.
type AWSInfrastructureAccessRolesListServerRequest struct {
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
// a SQL statement, but using the names of the attributes of the role instead of
// the names of the columns of a table. For example, in order to sort the roles
// descending by dislay_name the value should be:
//
// [source,sql]
// ----
// display_name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *AWSInfrastructureAccessRolesListServerRequest) Order() string {
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
// a SQL statement, but using the names of the attributes of the role instead of
// the names of the columns of a table. For example, in order to sort the roles
// descending by dislay_name the value should be:
//
// [source,sql]
// ----
// display_name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *AWSInfrastructureAccessRolesListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRolesListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRolesListServerRequest) GetPage() (value int, ok bool) {
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
// SQL statement, but using the names of the attributes of the role instead of
// the names of the columns of a table. For example, in order to retrieve all the
// role with a name starting with `my`the value should be:
//
// [source,sql]
// ----
// display_name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the roles
// that the user has permission to see will be returned.
func (r *AWSInfrastructureAccessRolesListServerRequest) Search() string {
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
// SQL statement, but using the names of the attributes of the role instead of
// the names of the columns of a table. For example, in order to retrieve all the
// role with a name starting with `my`the value should be:
//
// [source,sql]
// ----
// display_name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the roles
// that the user has permission to see will be returned.
func (r *AWSInfrastructureAccessRolesListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRolesListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRolesListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// AWSInfrastructureAccessRolesListServerResponse is the response for the 'list' method.
type AWSInfrastructureAccessRolesListServerResponse struct {
	status int
	err    *errors.Error
	items  *AWSInfrastructureAccessRoleList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of roles.
func (r *AWSInfrastructureAccessRolesListServerResponse) Items(value *AWSInfrastructureAccessRoleList) *AWSInfrastructureAccessRolesListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *AWSInfrastructureAccessRolesListServerResponse) Page(value int) *AWSInfrastructureAccessRolesListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *AWSInfrastructureAccessRolesListServerResponse) Size(value int) *AWSInfrastructureAccessRolesListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *AWSInfrastructureAccessRolesListServerResponse) Total(value int) *AWSInfrastructureAccessRolesListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *AWSInfrastructureAccessRolesListServerResponse) Status(value int) *AWSInfrastructureAccessRolesListServerResponse {
	r.status = value
	return r
}

// dispatchAWSInfrastructureAccessRoles navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchAWSInfrastructureAccessRoles(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRolesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptAWSInfrastructureAccessRolesListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.AWSInfrastructureAccessRole(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAWSInfrastructureAccessRole(w, r, target, segments[1:])
	}
}

// adaptAWSInfrastructureAccessRolesListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptAWSInfrastructureAccessRolesListRequest(w http.ResponseWriter, r *http.Request, server AWSInfrastructureAccessRolesServer) {
	request := &AWSInfrastructureAccessRolesListServerRequest{}
	err := readAWSInfrastructureAccessRolesListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &AWSInfrastructureAccessRolesListServerResponse{}
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
	err = writeAWSInfrastructureAccessRolesListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
