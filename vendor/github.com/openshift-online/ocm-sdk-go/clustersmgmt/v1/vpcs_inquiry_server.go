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

// VpcsInquiryServer represents the interface the manages the 'vpcs_inquiry' resource.
type VpcsInquiryServer interface {

	// Search handles a request for the 'search' method.
	//
	// Retrieves the list of available regions of the cloud provider.
	// IMPORTANT: This collection doesn't currently support paging or searching, so the returned
	// `page` will always be 1 and `size` and `total` will always be the total number of available regions
	// of the provider.
	Search(ctx context.Context, request *VpcsInquirySearchServerRequest, response *VpcsInquirySearchServerResponse) error
}

// VpcsInquirySearchServerRequest is the request for the 'search' method.
type VpcsInquirySearchServerRequest struct {
	body *CloudProviderData
	page *int
	size *int
}

// Body returns the value of the 'body' parameter.
//
// Cloud provider data needed for the inquiry
func (r *VpcsInquirySearchServerRequest) Body() *CloudProviderData {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Cloud provider data needed for the inquiry
func (r *VpcsInquirySearchServerRequest) GetBody() (value *CloudProviderData, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *VpcsInquirySearchServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *VpcsInquirySearchServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// regions of the provider.
func (r *VpcsInquirySearchServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// regions of the provider.
func (r *VpcsInquirySearchServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// VpcsInquirySearchServerResponse is the response for the 'search' method.
type VpcsInquirySearchServerResponse struct {
	status int
	err    *errors.Error
	items  *CloudVPCList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of cloud VPC.
func (r *VpcsInquirySearchServerResponse) Items(value *CloudVPCList) *VpcsInquirySearchServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *VpcsInquirySearchServerResponse) Page(value int) *VpcsInquirySearchServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// regions of the provider.
func (r *VpcsInquirySearchServerResponse) Size(value int) *VpcsInquirySearchServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page. As this collection doesn't support paging or
// searching the result will always be the total number of available regions of the provider.
func (r *VpcsInquirySearchServerResponse) Total(value int) *VpcsInquirySearchServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *VpcsInquirySearchServerResponse) Status(value int) *VpcsInquirySearchServerResponse {
	r.status = value
	return r
}

// dispatchVpcsInquiry navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchVpcsInquiry(w http.ResponseWriter, r *http.Request, server VpcsInquiryServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptVpcsInquirySearchRequest(w, r, server)
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

// adaptVpcsInquirySearchRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptVpcsInquirySearchRequest(w http.ResponseWriter, r *http.Request, server VpcsInquiryServer) {
	request := &VpcsInquirySearchServerRequest{}
	err := readVpcsInquirySearchRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &VpcsInquirySearchServerResponse{}
	response.status = 200
	err = server.Search(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeVpcsInquirySearchResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
