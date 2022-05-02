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
	"net/url"
	"time"

	"github.com/openshift-online/ocm-sdk-go/errors"
	"github.com/openshift-online/ocm-sdk-go/helpers"
)

// ResourceClient is the client of the 'resource' resource.
//
// Manages currently available cluster resources
type ResourceClient struct {
	transport http.RoundTripper
	path      string
}

// NewResourceClient creates a new client for the 'resource'
// resource using the given transport to send the requests and receive the
// responses.
func NewResourceClient(transport http.RoundTripper, path string) *ResourceClient {
	return &ResourceClient{
		transport: transport,
		path:      path,
	}
}

// Get creates a request for the 'get' method.
//
// Retrieves currently available cluster resources
func (c *ResourceClient) Get() *ResourceGetRequest {
	return &ResourceGetRequest{
		transport: c.transport,
		path:      c.path,
	}
}

// ResourcePollRequest is the request for the Poll method.
type ResourcePollRequest struct {
	request    *ResourceGetRequest
	interval   time.Duration
	statuses   []int
	predicates []func(interface{}) bool
}

// Parameter adds a query parameter to all the requests that will be used to retrieve the object.
func (r *ResourcePollRequest) Parameter(name string, value interface{}) *ResourcePollRequest {
	r.request.Parameter(name, value)
	return r
}

// Header adds a request header to all the requests that will be used to retrieve the object.
func (r *ResourcePollRequest) Header(name string, value interface{}) *ResourcePollRequest {
	r.request.Header(name, value)
	return r
}

// Interval sets the polling interval. This parameter is mandatory and must be greater than zero.
func (r *ResourcePollRequest) Interval(value time.Duration) *ResourcePollRequest {
	r.interval = value
	return r
}

// Status set the expected status of the response. Multiple values can be set calling this method
// multiple times. The response will be considered successful if the status is any of those values.
func (r *ResourcePollRequest) Status(value int) *ResourcePollRequest {
	r.statuses = append(r.statuses, value)
	return r
}

// Predicate adds a predicate that the response should satisfy be considered successful. Multiple
// predicates can be set calling this method multiple times. The response will be considered successful
// if all the predicates are satisfied.
func (r *ResourcePollRequest) Predicate(value func(*ResourceGetResponse) bool) *ResourcePollRequest {
	r.predicates = append(r.predicates, func(response interface{}) bool {
		return value(response.(*ResourceGetResponse))
	})
	return r
}

// StartContext starts the polling loop. Responses will be considered successful if the status is one of
// the values specified with the Status method and if all the predicates specified with the Predicate
// method return nil.
//
// The context must have a timeout or deadline, otherwise this method will immediately return an error.
func (r *ResourcePollRequest) StartContext(ctx context.Context) (response *ResourcePollResponse, err error) {
	result, err := helpers.PollContext(ctx, r.interval, r.statuses, r.predicates, r.task)
	if result != nil {
		response = &ResourcePollResponse{
			response: result.(*ResourceGetResponse),
		}
	}
	return
}

// task adapts the types of the request/response types so that they can be used with the generic
// polling function from the helpers package.
func (r *ResourcePollRequest) task(ctx context.Context) (status int, result interface{}, err error) {
	response, err := r.request.SendContext(ctx)
	if response != nil {
		status = response.Status()
		result = response
	}
	return
}

// ResourcePollResponse is the response for the Poll method.
type ResourcePollResponse struct {
	response *ResourceGetResponse
}

// Status returns the response status code.
func (r *ResourcePollResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.response.Status()
}

// Header returns header of the response.
func (r *ResourcePollResponse) Header() http.Header {
	if r == nil {
		return nil
	}
	return r.response.Header()
}

// Error returns the response error.
func (r *ResourcePollResponse) Error() *errors.Error {
	if r == nil {
		return nil
	}
	return r.response.Error()
}

// Body returns the value of the 'body' parameter.
//
// List of cluster resources
func (r *ResourcePollResponse) Body() *Resource {
	return r.response.Body()
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// List of cluster resources
func (r *ResourcePollResponse) GetBody() (value *Resource, ok bool) {
	return r.response.GetBody()
}

// Poll creates a request to repeatedly retrieve the object till the response has one of a given set
// of states and satisfies a set of predicates.
func (c *ResourceClient) Poll() *ResourcePollRequest {
	return &ResourcePollRequest{
		request: c.Get(),
	}
}

// ResourceGetRequest is the request for the 'get' method.
type ResourceGetRequest struct {
	transport http.RoundTripper
	path      string
	query     url.Values
	header    http.Header
}

// Parameter adds a query parameter.
func (r *ResourceGetRequest) Parameter(name string, value interface{}) *ResourceGetRequest {
	helpers.AddValue(&r.query, name, value)
	return r
}

// Header adds a request header.
func (r *ResourceGetRequest) Header(name string, value interface{}) *ResourceGetRequest {
	helpers.AddHeader(&r.header, name, value)
	return r
}

// Send sends this request, waits for the response, and returns it.
//
// This is a potentially lengthy operation, as it requires network communication.
// Consider using a context and the SendContext method.
func (r *ResourceGetRequest) Send() (result *ResourceGetResponse, err error) {
	return r.SendContext(context.Background())
}

// SendContext sends this request, waits for the response, and returns it.
func (r *ResourceGetRequest) SendContext(ctx context.Context) (result *ResourceGetResponse, err error) {
	query := helpers.CopyQuery(r.query)
	header := helpers.CopyHeader(r.header)
	uri := &url.URL{
		Path:     r.path,
		RawQuery: query.Encode(),
	}
	request := &http.Request{
		Method: "GET",
		URL:    uri,
		Header: header,
	}
	if ctx != nil {
		request = request.WithContext(ctx)
	}
	response, err := r.transport.RoundTrip(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	result = &ResourceGetResponse{}
	result.status = response.StatusCode
	result.header = response.Header
	if result.status >= 400 {
		result.err, err = errors.UnmarshalError(response.Body)
		if err != nil {
			return
		}
		err = result.err
		return
	}
	err = readResourceGetResponse(result, response.Body)
	if err != nil {
		return
	}
	return
}

// ResourceGetResponse is the response for the 'get' method.
type ResourceGetResponse struct {
	status int
	header http.Header
	err    *errors.Error
	body   *Resource
}

// Status returns the response status code.
func (r *ResourceGetResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.status
}

// Header returns header of the response.
func (r *ResourceGetResponse) Header() http.Header {
	if r == nil {
		return nil
	}
	return r.header
}

// Error returns the response error.
func (r *ResourceGetResponse) Error() *errors.Error {
	if r == nil {
		return nil
	}
	return r.err
}

// Body returns the value of the 'body' parameter.
//
// List of cluster resources
func (r *ResourceGetResponse) Body() *Resource {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// List of cluster resources
func (r *ResourceGetResponse) GetBody() (value *Resource, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}
