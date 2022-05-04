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

// KeyRingsInquiryServer represents the interface the manages the 'key_rings_inquiry' resource.
type KeyRingsInquiryServer interface {

	// Search handles a request for the 'search' method.
	//
	// Retrieves the list of available regions of the cloud provider.
	// IMPORTANT: This collection doesn't currently support paging or searching, so the returned
	// `page` will always be 1 and `size` and `total` will always be the total number of available regions
	// of the provider.
	Search(ctx context.Context, request *KeyRingsInquirySearchServerRequest, response *KeyRingsInquirySearchServerResponse) error
}

// KeyRingsInquirySearchServerRequest is the request for the 'search' method.
type KeyRingsInquirySearchServerRequest struct {
	body *CloudProviderData
	page *int
	size *int
}

// Body returns the value of the 'body' parameter.
//
// Cloud provider data needed for the inquiry
func (r *KeyRingsInquirySearchServerRequest) Body() *CloudProviderData {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
// Cloud provider data needed for the inquiry
func (r *KeyRingsInquirySearchServerRequest) GetBody() (value *CloudProviderData, ok bool) {
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
func (r *KeyRingsInquirySearchServerRequest) Page() int {
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
func (r *KeyRingsInquirySearchServerRequest) GetPage() (value int, ok bool) {
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
func (r *KeyRingsInquirySearchServerRequest) Size() int {
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
func (r *KeyRingsInquirySearchServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// KeyRingsInquirySearchServerResponse is the response for the 'search' method.
type KeyRingsInquirySearchServerResponse struct {
	status int
	err    *errors.Error
	items  *KeyRingList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of key rings.
func (r *KeyRingsInquirySearchServerResponse) Items(value *KeyRingList) *KeyRingsInquirySearchServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the returned page, where one corresponds to the first page. As this
// collection doesn't support paging the result will always be `1`.
func (r *KeyRingsInquirySearchServerResponse) Page(value int) *KeyRingsInquirySearchServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Number of items that will be contained in the returned page. As this collection
// doesn't support paging or searching the result will always be the total number of
// regions of the provider.
func (r *KeyRingsInquirySearchServerResponse) Size(value int) *KeyRingsInquirySearchServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page. As this collection doesn't support paging or
// searching the result will always be the total number of available regions of the provider.
func (r *KeyRingsInquirySearchServerResponse) Total(value int) *KeyRingsInquirySearchServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *KeyRingsInquirySearchServerResponse) Status(value int) *KeyRingsInquirySearchServerResponse {
	r.status = value
	return r
}

// dispatchKeyRingsInquiry navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchKeyRingsInquiry(w http.ResponseWriter, r *http.Request, server KeyRingsInquiryServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "POST":
			adaptKeyRingsInquirySearchRequest(w, r, server)
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

// adaptKeyRingsInquirySearchRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptKeyRingsInquirySearchRequest(w http.ResponseWriter, r *http.Request, server KeyRingsInquiryServer) {
	request := &KeyRingsInquirySearchServerRequest{}
	err := readKeyRingsInquirySearchRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &KeyRingsInquirySearchServerResponse{}
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
	err = writeKeyRingsInquirySearchResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
