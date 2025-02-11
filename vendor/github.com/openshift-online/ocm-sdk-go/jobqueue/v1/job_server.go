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

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// JobServer represents the interface the manages the 'job' resource.
type JobServer interface {

	// Failure handles a request for the 'failure' method.
	//
	// Mark a job as Failed. This method returns '204 No Content'
	Failure(ctx context.Context, request *JobFailureServerRequest, response *JobFailureServerResponse) error

	// Success handles a request for the 'success' method.
	//
	// Mark a job as Successful. This method returns '204 No Content'
	Success(ctx context.Context, request *JobSuccessServerRequest, response *JobSuccessServerResponse) error
}

// JobFailureServerRequest is the request for the 'failure' method.
type JobFailureServerRequest struct {
	failureReason *string
	receiptId     *string
}

// FailureReason returns the value of the 'failure_reason' parameter.
//
//
func (r *JobFailureServerRequest) FailureReason() string {
	if r != nil && r.failureReason != nil {
		return *r.failureReason
	}
	return ""
}

// GetFailureReason returns the value of the 'failure_reason' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *JobFailureServerRequest) GetFailureReason() (value string, ok bool) {
	ok = r != nil && r.failureReason != nil
	if ok {
		value = *r.failureReason
	}
	return
}

// ReceiptId returns the value of the 'receipt_id' parameter.
//
// A unique ID of a pop'ed job
func (r *JobFailureServerRequest) ReceiptId() string {
	if r != nil && r.receiptId != nil {
		return *r.receiptId
	}
	return ""
}

// GetReceiptId returns the value of the 'receipt_id' parameter and
// a flag indicating if the parameter has a value.
//
// A unique ID of a pop'ed job
func (r *JobFailureServerRequest) GetReceiptId() (value string, ok bool) {
	ok = r != nil && r.receiptId != nil
	if ok {
		value = *r.receiptId
	}
	return
}

// JobFailureServerResponse is the response for the 'failure' method.
type JobFailureServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *JobFailureServerResponse) Status(value int) *JobFailureServerResponse {
	r.status = value
	return r
}

// JobSuccessServerRequest is the request for the 'success' method.
type JobSuccessServerRequest struct {
	receiptId *string
}

// ReceiptId returns the value of the 'receipt_id' parameter.
//
// A unique ID of a pop'ed job
func (r *JobSuccessServerRequest) ReceiptId() string {
	if r != nil && r.receiptId != nil {
		return *r.receiptId
	}
	return ""
}

// GetReceiptId returns the value of the 'receipt_id' parameter and
// a flag indicating if the parameter has a value.
//
// A unique ID of a pop'ed job
func (r *JobSuccessServerRequest) GetReceiptId() (value string, ok bool) {
	ok = r != nil && r.receiptId != nil
	if ok {
		value = *r.receiptId
	}
	return
}

// JobSuccessServerResponse is the response for the 'success' method.
type JobSuccessServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *JobSuccessServerResponse) Status(value int) *JobSuccessServerResponse {
	r.status = value
	return r
}

// dispatchJob navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchJob(w http.ResponseWriter, r *http.Request, server JobServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "failure":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptJobFailureRequest(w, r, server)
		return
	case "success":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptJobSuccessRequest(w, r, server)
		return
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptJobFailureRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptJobFailureRequest(w http.ResponseWriter, r *http.Request, server JobServer) {
	request := &JobFailureServerRequest{}
	err := readJobFailureRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &JobFailureServerResponse{}
	response.status = 200
	err = server.Failure(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeJobFailureResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptJobSuccessRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptJobSuccessRequest(w http.ResponseWriter, r *http.Request, server JobServer) {
	request := &JobSuccessServerRequest{}
	err := readJobSuccessRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &JobSuccessServerResponse{}
	response.status = 200
	err = server.Success(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeJobSuccessResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
