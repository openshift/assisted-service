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

package v1 // github.com/openshift-online/ocm-sdk-go/servicelogs/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// ClusterLogsServer represents the interface the manages the 'cluster_logs' resource.
type ClusterLogsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Creates a new log entry.
	Add(ctx context.Context, request *ClusterLogsAddServerRequest, response *ClusterLogsAddServerResponse) error

	// List handles a request for the 'list' method.
	//
	// Retrieves the list of cluster logs.
	List(ctx context.Context, request *ClusterLogsListServerRequest, response *ClusterLogsListServerResponse) error

	// LogEntry returns the target 'log_entry' server for the given identifier.
	//
	// Reference to the service that manages a specific Log entry.
	LogEntry(id string) LogEntryServer
}

// ClusterLogsAddServerRequest is the request for the 'add' method.
type ClusterLogsAddServerRequest struct {
	body *LogEntry
}

// Body returns the value of the 'body' parameter.
//
// Log entry data.
func (r *ClusterLogsAddServerRequest) Body() *LogEntry {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Log entry data.
func (r *ClusterLogsAddServerRequest) GetBody() (value *LogEntry, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// ClusterLogsAddServerResponse is the response for the 'add' method.
type ClusterLogsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *LogEntry
}

// Body sets the value of the 'body' parameter.
//
// Log entry data.
func (r *ClusterLogsAddServerResponse) Body(value *LogEntry) *ClusterLogsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ClusterLogsAddServerResponse) Status(value int) *ClusterLogsAddServerResponse {
	r.status = value
	return r
}

// ClusterLogsListServerRequest is the request for the 'list' method.
type ClusterLogsListServerRequest struct {
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
// a SQL statement. For example, in order to sort the
// cluster logs descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *ClusterLogsListServerRequest) Order() string {
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
// a SQL statement. For example, in order to sort the
// cluster logs descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *ClusterLogsListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ClusterLogsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ClusterLogsListServerRequest) GetPage() (value int, ok bool) {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause
// of an SQL statement, but using the names of the attributes of the cluster logs
// instead of the names of the columns of a table. For example, in order to
// retrieve cluster logs with service_name starting with my:
//
// [source,sql]
// ----
// service_name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *ClusterLogsListServerRequest) Search() string {
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
// The syntax of this parameter is similar to the syntax of the _where_ clause
// of an SQL statement, but using the names of the attributes of the cluster logs
// instead of the names of the columns of a table. For example, in order to
// retrieve cluster logs with service_name starting with my:
//
// [source,sql]
// ----
// service_name like 'my%'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// items that the user has permission to see will be returned.
func (r *ClusterLogsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ClusterLogsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *ClusterLogsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// ClusterLogsListServerResponse is the response for the 'list' method.
type ClusterLogsListServerResponse struct {
	status int
	err    *errors.Error
	items  *LogEntryList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of Cluster logs.
func (r *ClusterLogsListServerResponse) Items(value *LogEntryList) *ClusterLogsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *ClusterLogsListServerResponse) Page(value int) *ClusterLogsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *ClusterLogsListServerResponse) Size(value int) *ClusterLogsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *ClusterLogsListServerResponse) Total(value int) *ClusterLogsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *ClusterLogsListServerResponse) Status(value int) *ClusterLogsListServerResponse {
	r.status = value
	return r
}

// dispatchClusterLogs navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchClusterLogs(w http.ResponseWriter, r *http.Request, server ClusterLogsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptClusterLogsAddRequest(w, r, server)
			return
		case "GET":
			adaptClusterLogsListRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	default:
		target := server.LogEntry(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchLogEntry(w, r, target, segments[1:])
	}
}

// adaptClusterLogsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterLogsAddRequest(w http.ResponseWriter, r *http.Request, server ClusterLogsServer) {
	request := &ClusterLogsAddServerRequest{}
	err := readClusterLogsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterLogsAddServerResponse{}
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
	err = writeClusterLogsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptClusterLogsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterLogsListRequest(w http.ResponseWriter, r *http.Request, server ClusterLogsServer) {
	request := &ClusterLogsListServerRequest{}
	err := readClusterLogsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterLogsListServerResponse{}
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
	err = writeClusterLogsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
