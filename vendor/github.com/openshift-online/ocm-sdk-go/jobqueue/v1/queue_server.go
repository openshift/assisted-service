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

package v1 // github.com/openshift-online/ocm-sdk-go/jobqueue/v1

import (
	"context"
	"net/http"
	time "time"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// QueueServer represents the interface the manages the 'queue' resource.
type QueueServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of a job queue by ID.
	Get(ctx context.Context, request *QueueGetServerRequest, response *QueueGetServerResponse) error

	// Pop handles a request for the 'pop' method.
	//
	// POP new job from a job queue
	Pop(ctx context.Context, request *QueuePopServerRequest, response *QueuePopServerResponse) error

	// Push handles a request for the 'push' method.
	//
	// PUSH a new job into job queue
	Push(ctx context.Context, request *QueuePushServerRequest, response *QueuePushServerResponse) error

	// Jobs returns the target 'jobs' resource.
	//
	// jobs' operations (success, failure)
	Jobs() JobsServer
}

// QueueGetServerRequest is the request for the 'get' method.
type QueueGetServerRequest struct {
}

// QueueGetServerResponse is the response for the 'get' method.
type QueueGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Queue
}

// Body sets the value of the 'body' parameter.
//
//
func (r *QueueGetServerResponse) Body(value *Queue) *QueueGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *QueueGetServerResponse) Status(value int) *QueueGetServerResponse {
	r.status = value
	return r
}

// QueuePopServerRequest is the request for the 'pop' method.
type QueuePopServerRequest struct {
}

// QueuePopServerResponse is the response for the 'pop' method.
type QueuePopServerResponse struct {
	status      int
	err         *errors.Error
	href        *string
	id          *string
	abandonedAt *time.Time
	arguments   *string
	attempts    *int
	createdAt   *time.Time
	kind        *string
	receiptId   *string
	updatedAt   *time.Time
}

// HREF sets the value of the 'HREF' parameter.
//
//
func (r *QueuePopServerResponse) HREF(value string) *QueuePopServerResponse {
	r.href = &value
	return r
}

// ID sets the value of the 'ID' parameter.
//
//
func (r *QueuePopServerResponse) ID(value string) *QueuePopServerResponse {
	r.id = &value
	return r
}

// AbandonedAt sets the value of the 'abandoned_at' parameter.
//
//
func (r *QueuePopServerResponse) AbandonedAt(value time.Time) *QueuePopServerResponse {
	r.abandonedAt = &value
	return r
}

// Arguments sets the value of the 'arguments' parameter.
//
//
func (r *QueuePopServerResponse) Arguments(value string) *QueuePopServerResponse {
	r.arguments = &value
	return r
}

// Attempts sets the value of the 'attempts' parameter.
//
//
func (r *QueuePopServerResponse) Attempts(value int) *QueuePopServerResponse {
	r.attempts = &value
	return r
}

// CreatedAt sets the value of the 'created_at' parameter.
//
//
func (r *QueuePopServerResponse) CreatedAt(value time.Time) *QueuePopServerResponse {
	r.createdAt = &value
	return r
}

// Kind sets the value of the 'kind' parameter.
//
//
func (r *QueuePopServerResponse) Kind(value string) *QueuePopServerResponse {
	r.kind = &value
	return r
}

// ReceiptId sets the value of the 'receipt_id' parameter.
//
//
func (r *QueuePopServerResponse) ReceiptId(value string) *QueuePopServerResponse {
	r.receiptId = &value
	return r
}

// UpdatedAt sets the value of the 'updated_at' parameter.
//
//
func (r *QueuePopServerResponse) UpdatedAt(value time.Time) *QueuePopServerResponse {
	r.updatedAt = &value
	return r
}

// Status sets the status code.
func (r *QueuePopServerResponse) Status(value int) *QueuePopServerResponse {
	r.status = value
	return r
}

// QueuePushServerRequest is the request for the 'push' method.
type QueuePushServerRequest struct {
	abandonedAt *time.Time
	arguments   *string
	attempts    *int
	createdAt   *time.Time
}

// AbandonedAt returns the value of the 'abandoned_at' parameter.
//
//
func (r *QueuePushServerRequest) AbandonedAt() time.Time {
	if r != nil && r.abandonedAt != nil {
		return *r.abandonedAt
	}
	return time.Time{}
}

// GetAbandonedAt returns the value of the 'abandoned_at' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *QueuePushServerRequest) GetAbandonedAt() (value time.Time, ok bool) {
	ok = r != nil && r.abandonedAt != nil
	if ok {
		value = *r.abandonedAt
	}
	return
}

// Arguments returns the value of the 'arguments' parameter.
//
//
func (r *QueuePushServerRequest) Arguments() string {
	if r != nil && r.arguments != nil {
		return *r.arguments
	}
	return ""
}

// GetArguments returns the value of the 'arguments' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *QueuePushServerRequest) GetArguments() (value string, ok bool) {
	ok = r != nil && r.arguments != nil
	if ok {
		value = *r.arguments
	}
	return
}

// Attempts returns the value of the 'attempts' parameter.
//
//
func (r *QueuePushServerRequest) Attempts() int {
	if r != nil && r.attempts != nil {
		return *r.attempts
	}
	return 0
}

// GetAttempts returns the value of the 'attempts' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *QueuePushServerRequest) GetAttempts() (value int, ok bool) {
	ok = r != nil && r.attempts != nil
	if ok {
		value = *r.attempts
	}
	return
}

// CreatedAt returns the value of the 'created_at' parameter.
//
//
func (r *QueuePushServerRequest) CreatedAt() time.Time {
	if r != nil && r.createdAt != nil {
		return *r.createdAt
	}
	return time.Time{}
}

// GetCreatedAt returns the value of the 'created_at' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *QueuePushServerRequest) GetCreatedAt() (value time.Time, ok bool) {
	ok = r != nil && r.createdAt != nil
	if ok {
		value = *r.createdAt
	}
	return
}

// QueuePushServerResponse is the response for the 'push' method.
type QueuePushServerResponse struct {
	status      int
	err         *errors.Error
	href        *string
	id          *string
	abandonedAt *time.Time
	arguments   *string
	attempts    *int
	createdAt   *time.Time
	kind        *string
	receiptId   *string
	updatedAt   *time.Time
}

// HREF sets the value of the 'HREF' parameter.
//
//
func (r *QueuePushServerResponse) HREF(value string) *QueuePushServerResponse {
	r.href = &value
	return r
}

// ID sets the value of the 'ID' parameter.
//
//
func (r *QueuePushServerResponse) ID(value string) *QueuePushServerResponse {
	r.id = &value
	return r
}

// AbandonedAt sets the value of the 'abandoned_at' parameter.
//
//
func (r *QueuePushServerResponse) AbandonedAt(value time.Time) *QueuePushServerResponse {
	r.abandonedAt = &value
	return r
}

// Arguments sets the value of the 'arguments' parameter.
//
//
func (r *QueuePushServerResponse) Arguments(value string) *QueuePushServerResponse {
	r.arguments = &value
	return r
}

// Attempts sets the value of the 'attempts' parameter.
//
//
func (r *QueuePushServerResponse) Attempts(value int) *QueuePushServerResponse {
	r.attempts = &value
	return r
}

// CreatedAt sets the value of the 'created_at' parameter.
//
//
func (r *QueuePushServerResponse) CreatedAt(value time.Time) *QueuePushServerResponse {
	r.createdAt = &value
	return r
}

// Kind sets the value of the 'kind' parameter.
//
//
func (r *QueuePushServerResponse) Kind(value string) *QueuePushServerResponse {
	r.kind = &value
	return r
}

// ReceiptId sets the value of the 'receipt_id' parameter.
//
//
func (r *QueuePushServerResponse) ReceiptId(value string) *QueuePushServerResponse {
	r.receiptId = &value
	return r
}

// UpdatedAt sets the value of the 'updated_at' parameter.
//
//
func (r *QueuePushServerResponse) UpdatedAt(value time.Time) *QueuePushServerResponse {
	r.updatedAt = &value
	return r
}

// Status sets the status code.
func (r *QueuePushServerResponse) Status(value int) *QueuePushServerResponse {
	r.status = value
	return r
}

// dispatchQueue navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchQueue(w http.ResponseWriter, r *http.Request, server QueueServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptQueueGetRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "pop":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptQueuePopRequest(w, r, server)
		return
	case "push":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptQueuePushRequest(w, r, server)
		return
	case "jobs":
		target := server.Jobs()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchJobs(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptQueueGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptQueueGetRequest(w http.ResponseWriter, r *http.Request, server QueueServer) {
	request := &QueueGetServerRequest{}
	err := readQueueGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &QueueGetServerResponse{}
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
	err = writeQueueGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptQueuePopRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptQueuePopRequest(w http.ResponseWriter, r *http.Request, server QueueServer) {
	request := &QueuePopServerRequest{}
	err := readQueuePopRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &QueuePopServerResponse{}
	response.status = 200
	err = server.Pop(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeQueuePopResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptQueuePushRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptQueuePushRequest(w http.ResponseWriter, r *http.Request, server QueueServer) {
	request := &QueuePushServerRequest{}
	err := readQueuePushRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &QueuePushServerResponse{}
	response.status = 200
	err = server.Push(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeQueuePushResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
