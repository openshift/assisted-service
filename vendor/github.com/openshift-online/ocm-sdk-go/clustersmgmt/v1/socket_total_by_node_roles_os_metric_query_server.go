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

// SocketTotalByNodeRolesOSMetricQueryServer represents the interface the manages the 'socket_total_by_node_roles_OS_metric_query' resource.
type SocketTotalByNodeRolesOSMetricQueryServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the metrics.
	Get(ctx context.Context, request *SocketTotalByNodeRolesOSMetricQueryGetServerRequest, response *SocketTotalByNodeRolesOSMetricQueryGetServerResponse) error
}

// SocketTotalByNodeRolesOSMetricQueryGetServerRequest is the request for the 'get' method.
type SocketTotalByNodeRolesOSMetricQueryGetServerRequest struct {
}

// SocketTotalByNodeRolesOSMetricQueryGetServerResponse is the response for the 'get' method.
type SocketTotalByNodeRolesOSMetricQueryGetServerResponse struct {
	status int
	err    *errors.Error
	body   *SocketTotalsNodeRoleOSMetricNode
}

// Body sets the value of the 'body' parameter.
//
//
func (r *SocketTotalByNodeRolesOSMetricQueryGetServerResponse) Body(value *SocketTotalsNodeRoleOSMetricNode) *SocketTotalByNodeRolesOSMetricQueryGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *SocketTotalByNodeRolesOSMetricQueryGetServerResponse) Status(value int) *SocketTotalByNodeRolesOSMetricQueryGetServerResponse {
	r.status = value
	return r
}

// dispatchSocketTotalByNodeRolesOSMetricQuery navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchSocketTotalByNodeRolesOSMetricQuery(w http.ResponseWriter, r *http.Request, server SocketTotalByNodeRolesOSMetricQueryServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptSocketTotalByNodeRolesOSMetricQueryGetRequest(w, r, server)
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

// adaptSocketTotalByNodeRolesOSMetricQueryGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSocketTotalByNodeRolesOSMetricQueryGetRequest(w http.ResponseWriter, r *http.Request, server SocketTotalByNodeRolesOSMetricQueryServer) {
	request := &SocketTotalByNodeRolesOSMetricQueryGetServerRequest{}
	err := readSocketTotalByNodeRolesOSMetricQueryGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SocketTotalByNodeRolesOSMetricQueryGetServerResponse{}
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
	err = writeSocketTotalByNodeRolesOSMetricQueryGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
