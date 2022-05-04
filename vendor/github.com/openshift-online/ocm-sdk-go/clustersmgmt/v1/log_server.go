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

// LogServer represents the interface the manages the 'log' resource.
type LogServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the log.
	Get(ctx context.Context, request *LogGetServerRequest, response *LogGetServerResponse) error
}

// LogGetServerRequest is the request for the 'get' method.
type LogGetServerRequest struct {
	offset *int
	tail   *int
}

// Offset returns the value of the 'offset' parameter.
//
// Line offset to start logs from. if 0 retreive entire log.
// If offset > #lines return an empty log.
func (r *LogGetServerRequest) Offset() int {
	if r != nil && r.offset != nil {
		return *r.offset
	}
	return 0
}

// GetOffset returns the value of the 'offset' parameter and
// a flag indicating if the parameter has a value.
//
// Line offset to start logs from. if 0 retreive entire log.
// If offset > #lines return an empty log.
func (r *LogGetServerRequest) GetOffset() (value int, ok bool) {
	ok = r != nil && r.offset != nil
	if ok {
		value = *r.offset
	}
	return
}

// Tail returns the value of the 'tail' parameter.
//
// Returns the number of tail lines from the end of the log.
// If there are no line breaks or the number of lines < tail
// return the entire log.
// Either 'tail' or 'offset' can be set. Not both.
func (r *LogGetServerRequest) Tail() int {
	if r != nil && r.tail != nil {
		return *r.tail
	}
	return 0
}

// GetTail returns the value of the 'tail' parameter and
// a flag indicating if the parameter has a value.
//
// Returns the number of tail lines from the end of the log.
// If there are no line breaks or the number of lines < tail
// return the entire log.
// Either 'tail' or 'offset' can be set. Not both.
func (r *LogGetServerRequest) GetTail() (value int, ok bool) {
	ok = r != nil && r.tail != nil
	if ok {
		value = *r.tail
	}
	return
}

// LogGetServerResponse is the response for the 'get' method.
type LogGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Log
}

// Body sets the value of the 'body' parameter.
//
// Retreived log.
func (r *LogGetServerResponse) Body(value *Log) *LogGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *LogGetServerResponse) Status(value int) *LogGetServerResponse {
	r.status = value
	return r
}

// dispatchLog navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchLog(w http.ResponseWriter, r *http.Request, server LogServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptLogGetRequest(w, r, server)
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

// adaptLogGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptLogGetRequest(w http.ResponseWriter, r *http.Request, server LogServer) {
	request := &LogGetServerRequest{}
	err := readLogGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &LogGetServerResponse{}
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
	err = writeLogGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
