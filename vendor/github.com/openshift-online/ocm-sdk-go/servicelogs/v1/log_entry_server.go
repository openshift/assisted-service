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

// LogEntryServer represents the interface the manages the 'log_entry' resource.
type LogEntryServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the log entry.
	Delete(ctx context.Context, request *LogEntryDeleteServerRequest, response *LogEntryDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the log entry.
	Get(ctx context.Context, request *LogEntryGetServerRequest, response *LogEntryGetServerResponse) error
}

// LogEntryDeleteServerRequest is the request for the 'delete' method.
type LogEntryDeleteServerRequest struct {
}

// LogEntryDeleteServerResponse is the response for the 'delete' method.
type LogEntryDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *LogEntryDeleteServerResponse) Status(value int) *LogEntryDeleteServerResponse {
	r.status = value
	return r
}

// LogEntryGetServerRequest is the request for the 'get' method.
type LogEntryGetServerRequest struct {
}

// LogEntryGetServerResponse is the response for the 'get' method.
type LogEntryGetServerResponse struct {
	status int
	err    *errors.Error
	body   *LogEntry
}

// Body sets the value of the 'body' parameter.
//
//
func (r *LogEntryGetServerResponse) Body(value *LogEntry) *LogEntryGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LogEntryGetServerResponse) Status(value int) *LogEntryGetServerResponse {
	r.status = value
	return r
}

// dispatchLogEntry navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchLogEntry(w http.ResponseWriter, r *http.Request, server LogEntryServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptLogEntryDeleteRequest(w, r, server)
			return
		case "GET":
			adaptLogEntryGetRequest(w, r, server)
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

// adaptLogEntryDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLogEntryDeleteRequest(w http.ResponseWriter, r *http.Request, server LogEntryServer) {
	request := &LogEntryDeleteServerRequest{}
	err := readLogEntryDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LogEntryDeleteServerResponse{}
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
	err = writeLogEntryDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptLogEntryGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLogEntryGetRequest(w http.ResponseWriter, r *http.Request, server LogEntryServer) {
	request := &LogEntryGetServerRequest{}
	err := readLogEntryGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LogEntryGetServerResponse{}
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
	err = writeLogEntryGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
