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

// EventsServer represents the interface the manages the 'events' resource.
type EventsServer interface {

	// Add handles a request for the 'add' method.
	//
	// Adds a new event to be tracked. When sending a new event request,
	// it gets tracked in Prometheus, Pendo, CloudWatch, or whichever
	// analytics client is configured as part of clusters service. This
	// allows for reporting on events that happen outside of a regular API
	// request, but are found to be useful for understanding customer
	// needs and possible blockers.
	Add(ctx context.Context, request *EventsAddServerRequest, response *EventsAddServerResponse) error
}

// EventsAddServerRequest is the request for the 'add' method.
type EventsAddServerRequest struct {
	body *Event
}

// Body returns the value of the 'body' parameter.
//
// Description of the event.
func (r *EventsAddServerRequest) Body() *Event {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Description of the event.
func (r *EventsAddServerRequest) GetBody() (value *Event, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// EventsAddServerResponse is the response for the 'add' method.
type EventsAddServerResponse struct {
	status int
	err    *errors.Error
	body   *Event
}

// Body sets the value of the 'body' parameter.
//
// Description of the event.
func (r *EventsAddServerResponse) Body(value *Event) *EventsAddServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *EventsAddServerResponse) Status(value int) *EventsAddServerResponse {
	r.status = value
	return r
}

// dispatchEvents navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchEvents(w http.ResponseWriter, r *http.Request, server EventsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptEventsAddRequest(w, r, server)
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

// adaptEventsAddRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptEventsAddRequest(w http.ResponseWriter, r *http.Request, server EventsServer) {
	request := &EventsAddServerRequest{}
	err := readEventsAddRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &EventsAddServerResponse{}
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
	err = writeEventsAddResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
