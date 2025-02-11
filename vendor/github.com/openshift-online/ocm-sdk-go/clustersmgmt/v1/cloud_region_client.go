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

// CloudRegionClient is the client of the 'cloud_region' resource.
//
// Manages a specific cloud region.
type CloudRegionClient struct {
	transport http.RoundTripper
	path      string
}

// NewCloudRegionClient creates a new client for the 'cloud_region'
// resource using the given transport to send the requests and receive the
// responses.
func NewCloudRegionClient(transport http.RoundTripper, path string) *CloudRegionClient {
	return &CloudRegionClient{
		transport: transport,
		path:      path,
	}
}

// Get creates a request for the 'get' method.
//
// Retrieves the details of the region.
func (c *CloudRegionClient) Get() *CloudRegionGetRequest {
	return &CloudRegionGetRequest{
		transport: c.transport,
		path:      c.path,
	}
}

// CloudRegionPollRequest is the request for the Poll method.
type CloudRegionPollRequest struct {
	request    *CloudRegionGetRequest
	interval   time.Duration
	statuses   []int
	predicates []func(interface{}) bool
}

// Parameter adds a query parameter to all the requests that will be used to retrieve the object.
func (r *CloudRegionPollRequest) Parameter(name string, value interface{}) *CloudRegionPollRequest {
	r.request.Parameter(name, value)
	return r
}

// Header adds a request header to all the requests that will be used to retrieve the object.
func (r *CloudRegionPollRequest) Header(name string, value interface{}) *CloudRegionPollRequest {
	r.request.Header(name, value)
	return r
}

// Interval sets the polling interval. This parameter is mandatory and must be greater than zero.
func (r *CloudRegionPollRequest) Interval(value time.Duration) *CloudRegionPollRequest {
	r.interval = value
	return r
}

// Status set the expected status of the response. Multiple values can be set calling this method
// multiple times. The response will be considered successful if the status is any of those values.
func (r *CloudRegionPollRequest) Status(value int) *CloudRegionPollRequest {
	r.statuses = append(r.statuses, value)
	return r
}

// Predicate adds a predicate that the response should satisfy be considered successful. Multiple
// predicates can be set calling this method multiple times. The response will be considered successful
// if all the predicates are satisfied.
func (r *CloudRegionPollRequest) Predicate(value func(*CloudRegionGetResponse) bool) *CloudRegionPollRequest {
	r.predicates = append(r.predicates, func(response interface{}) bool {
		return value(response.(*CloudRegionGetResponse))
	})
	return r
}

// StartContext starts the polling loop. Responses will be considered successful if the status is one of
// the values specified with the Status method and if all the predicates specified with the Predicate
// method return nil.
//
// The context must have a timeout or deadline, otherwise this method will immediately return an error.
func (r *CloudRegionPollRequest) StartContext(ctx context.Context) (response *CloudRegionPollResponse, err error) {
	result, err := helpers.PollContext(ctx, r.interval, r.statuses, r.predicates, r.task)
	if result != nil {
		response = &CloudRegionPollResponse{
			response: result.(*CloudRegionGetResponse),
		}
	}
	return
}

// task adapts the types of the request/response types so that they can be used with the generic
// polling function from the helpers package.
func (r *CloudRegionPollRequest) task(ctx context.Context) (status int, result interface{}, err error) {
	response, err := r.request.SendContext(ctx)
	if response != nil {
		status = response.Status()
		result = response
	}
	return
}

// CloudRegionPollResponse is the response for the Poll method.
type CloudRegionPollResponse struct {
	response *CloudRegionGetResponse
}

// Status returns the response status code.
func (r *CloudRegionPollResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.response.Status()
}

// Header returns header of the response.
func (r *CloudRegionPollResponse) Header() http.Header {
	if r == nil {
		return nil
	}
	return r.response.Header()
}

// Error returns the response error.
func (r *CloudRegionPollResponse) Error() *errors.Error {
	if r == nil {
		return nil
	}
	return r.response.Error()
}

// Body returns the value of the 'body' parameter.
//
//
func (r *CloudRegionPollResponse) Body() *CloudRegion {
	return r.response.Body()
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *CloudRegionPollResponse) GetBody() (value *CloudRegion, ok bool) {
	return r.response.GetBody()
}

// Poll creates a request to repeatedly retrieve the object till the response has one of a given set
// of states and satisfies a set of predicates.
func (c *CloudRegionClient) Poll() *CloudRegionPollRequest {
	return &CloudRegionPollRequest{
		request: c.Get(),
	}
}

// CloudRegionGetRequest is the request for the 'get' method.
type CloudRegionGetRequest struct {
	transport http.RoundTripper
	path      string
	query     url.Values
	header    http.Header
}

// Parameter adds a query parameter.
func (r *CloudRegionGetRequest) Parameter(name string, value interface{}) *CloudRegionGetRequest {
	helpers.AddValue(&r.query, name, value)
	return r
}

// Header adds a request header.
func (r *CloudRegionGetRequest) Header(name string, value interface{}) *CloudRegionGetRequest {
	helpers.AddHeader(&r.header, name, value)
	return r
}

// Send sends this request, waits for the response, and returns it.
//
// This is a potentially lengthy operation, as it requires network communication.
// Consider using a context and the SendContext method.
func (r *CloudRegionGetRequest) Send() (result *CloudRegionGetResponse, err error) {
	return r.SendContext(context.Background())
}

// SendContext sends this request, waits for the response, and returns it.
func (r *CloudRegionGetRequest) SendContext(ctx context.Context) (result *CloudRegionGetResponse, err error) {
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
	result = &CloudRegionGetResponse{}
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
	err = readCloudRegionGetResponse(result, response.Body)
	if err != nil {
		return
	}
	return
}

// CloudRegionGetResponse is the response for the 'get' method.
type CloudRegionGetResponse struct {
	status int
	header http.Header
	err    *errors.Error
	body   *CloudRegion
}

// Status returns the response status code.
func (r *CloudRegionGetResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.status
}

// Header returns header of the response.
func (r *CloudRegionGetResponse) Header() http.Header {
	if r == nil {
		return nil
	}
	return r.header
}

// Error returns the response error.
func (r *CloudRegionGetResponse) Error() *errors.Error {
	if r == nil {
		return nil
	}
	return r.err
}

// Body returns the value of the 'body' parameter.
//
//
func (r *CloudRegionGetResponse) Body() *CloudRegion {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *CloudRegionGetResponse) GetBody() (value *CloudRegion, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}
