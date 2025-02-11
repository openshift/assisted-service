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

package v1 // github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// SubscriptionsServer represents the interface the manages the 'subscriptions' resource.
type SubscriptionsServer interface {

	// List handles a request for the 'list' method.
	//
	// Retrieves a list of subscriptions.
	List(ctx context.Context, request *SubscriptionsListServerRequest, response *SubscriptionsListServerResponse) error

	// Post handles a request for the 'post' method.
	//
	// Create a new subscription and register a cluster for it.
	Post(ctx context.Context, request *SubscriptionsPostServerRequest, response *SubscriptionsPostServerResponse) error

	// Labels returns the target 'generic_labels' resource.
	//
	// Reference to the list of labels of a specific subscription.
	Labels() GenericLabelsServer

	// Subscription returns the target 'subscription' server for the given identifier.
	//
	// Reference to the service that manages a specific subscription.
	Subscription(id string) SubscriptionServer
}

// SubscriptionsListServerRequest is the request for the 'list' method.
type SubscriptionsListServerRequest struct {
	fetchaccountsAccounts *bool
	fetchlabelsLabels     *bool
	fields                *string
	labels                *string
	order                 *string
	page                  *int
	search                *string
	size                  *int
}

// FetchaccountsAccounts returns the value of the 'fetchaccounts_accounts' parameter.
//
// If true, includes the account reference information in the output. Could slow request response time.
func (r *SubscriptionsListServerRequest) FetchaccountsAccounts() bool {
	if r != nil && r.fetchaccountsAccounts != nil {
		return *r.fetchaccountsAccounts
	}
	return false
}

// GetFetchaccountsAccounts returns the value of the 'fetchaccounts_accounts' parameter and
// a flag indicating if the parameter has a value.
//
// If true, includes the account reference information in the output. Could slow request response time.
func (r *SubscriptionsListServerRequest) GetFetchaccountsAccounts() (value bool, ok bool) {
	ok = r != nil && r.fetchaccountsAccounts != nil
	if ok {
		value = *r.fetchaccountsAccounts
	}
	return
}

// FetchlabelsLabels returns the value of the 'fetchlabels_labels' parameter.
//
// If true, includes the labels on a subscription in the output. Could slow request response time.
func (r *SubscriptionsListServerRequest) FetchlabelsLabels() bool {
	if r != nil && r.fetchlabelsLabels != nil {
		return *r.fetchlabelsLabels
	}
	return false
}

// GetFetchlabelsLabels returns the value of the 'fetchlabels_labels' parameter and
// a flag indicating if the parameter has a value.
//
// If true, includes the labels on a subscription in the output. Could slow request response time.
func (r *SubscriptionsListServerRequest) GetFetchlabelsLabels() (value bool, ok bool) {
	ok = r != nil && r.fetchlabelsLabels != nil
	if ok {
		value = *r.fetchlabelsLabels
	}
	return
}

// Fields returns the value of the 'fields' parameter.
//
// Projection
// This field contains a comma-separated list of fields you'd like to get in
// a result. No new fields can be added, only existing ones can be filtered.
// To specify a field 'id' of a structure 'plan' use 'plan.id'.
// To specify all fields of a structure 'labels' use 'labels.*'.
//
func (r *SubscriptionsListServerRequest) Fields() string {
	if r != nil && r.fields != nil {
		return *r.fields
	}
	return ""
}

// GetFields returns the value of the 'fields' parameter and
// a flag indicating if the parameter has a value.
//
// Projection
// This field contains a comma-separated list of fields you'd like to get in
// a result. No new fields can be added, only existing ones can be filtered.
// To specify a field 'id' of a structure 'plan' use 'plan.id'.
// To specify all fields of a structure 'labels' use 'labels.*'.
//
func (r *SubscriptionsListServerRequest) GetFields() (value string, ok bool) {
	ok = r != nil && r.fields != nil
	if ok {
		value = *r.fields
	}
	return
}

// Labels returns the value of the 'labels' parameter.
//
// Filter subscriptions by a comma separated list of labels:
//
// [source]
// ----
// env=staging,department=sales
// ----
//
func (r *SubscriptionsListServerRequest) Labels() string {
	if r != nil && r.labels != nil {
		return *r.labels
	}
	return ""
}

// GetLabels returns the value of the 'labels' parameter and
// a flag indicating if the parameter has a value.
//
// Filter subscriptions by a comma separated list of labels:
//
// [source]
// ----
// env=staging,department=sales
// ----
//
func (r *SubscriptionsListServerRequest) GetLabels() (value string, ok bool) {
	ok = r != nil && r.labels != nil
	if ok {
		value = *r.labels
	}
	return
}

// Order returns the value of the 'order' parameter.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement. For example, in order to sort the
// subscriptions descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *SubscriptionsListServerRequest) Order() string {
	if r != nil && r.order != nil {
		return *r.order
	}
	return ""
}

// GetOrder returns the value of the 'order' parameter and
// a flag indicating if the parameter has a value.
//
// Order criteria.
//
// The syntax of this parameter is similar to the syntax of the _order by_ clause of
// a SQL statement. For example, in order to sort the
// subscriptions descending by name identifier the value should be:
//
// [source,sql]
// ----
// name desc
// ----
//
// If the parameter isn't provided, or if the value is empty, then the order of the
// results is undefined.
func (r *SubscriptionsListServerRequest) GetOrder() (value string, ok bool) {
	ok = r != nil && r.order != nil
	if ok {
		value = *r.order
	}
	return
}

// Page returns the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SubscriptionsListServerRequest) Page() int {
	if r != nil && r.page != nil {
		return *r.page
	}
	return 0
}

// GetPage returns the value of the 'page' parameter and
// a flag indicating if the parameter has a value.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SubscriptionsListServerRequest) GetPage() (value int, ok bool) {
	ok = r != nil && r.page != nil
	if ok {
		value = *r.page
	}
	return
}

// Search returns the value of the 'search' parameter.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause of a
// SQL statement, but using the names of the attributes of the subscription instead
// of the names of the columns of a table. For example, in order to retrieve all the
// subscriptions for managed clusters the value should be:
//
// [source,sql]
// ----
// managed = 't'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// clusters that the user has permission to see will be returned.
func (r *SubscriptionsListServerRequest) Search() string {
	if r != nil && r.search != nil {
		return *r.search
	}
	return ""
}

// GetSearch returns the value of the 'search' parameter and
// a flag indicating if the parameter has a value.
//
// Search criteria.
//
// The syntax of this parameter is similar to the syntax of the _where_ clause of a
// SQL statement, but using the names of the attributes of the subscription instead
// of the names of the columns of a table. For example, in order to retrieve all the
// subscriptions for managed clusters the value should be:
//
// [source,sql]
// ----
// managed = 't'
// ----
//
// If the parameter isn't provided, or if the value is empty, then all the
// clusters that the user has permission to see will be returned.
func (r *SubscriptionsListServerRequest) GetSearch() (value string, ok bool) {
	ok = r != nil && r.search != nil
	if ok {
		value = *r.search
	}
	return
}

// Size returns the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *SubscriptionsListServerRequest) Size() int {
	if r != nil && r.size != nil {
		return *r.size
	}
	return 0
}

// GetSize returns the value of the 'size' parameter and
// a flag indicating if the parameter has a value.
//
// Maximum number of items that will be contained in the returned page.
func (r *SubscriptionsListServerRequest) GetSize() (value int, ok bool) {
	ok = r != nil && r.size != nil
	if ok {
		value = *r.size
	}
	return
}

// SubscriptionsListServerResponse is the response for the 'list' method.
type SubscriptionsListServerResponse struct {
	status int
	err    *errors.Error
	items  *SubscriptionList
	page   *int
	size   *int
	total  *int
}

// Items sets the value of the 'items' parameter.
//
// Retrieved list of subscriptions.
func (r *SubscriptionsListServerResponse) Items(value *SubscriptionList) *SubscriptionsListServerResponse {
	r.items = value
	return r
}

// Page sets the value of the 'page' parameter.
//
// Index of the requested page, where one corresponds to the first page.
func (r *SubscriptionsListServerResponse) Page(value int) *SubscriptionsListServerResponse {
	r.page = &value
	return r
}

// Size sets the value of the 'size' parameter.
//
// Maximum number of items that will be contained in the returned page.
func (r *SubscriptionsListServerResponse) Size(value int) *SubscriptionsListServerResponse {
	r.size = &value
	return r
}

// Total sets the value of the 'total' parameter.
//
// Total number of items of the collection that match the search criteria,
// regardless of the size of the page.
func (r *SubscriptionsListServerResponse) Total(value int) *SubscriptionsListServerResponse {
	r.total = &value
	return r
}

// Status sets the status code.
func (r *SubscriptionsListServerResponse) Status(value int) *SubscriptionsListServerResponse {
	r.status = value
	return r
}

// SubscriptionsPostServerRequest is the request for the 'post' method.
type SubscriptionsPostServerRequest struct {
	request *SubscriptionRegistration
}

// Request returns the value of the 'request' parameter.
//
//
func (r *SubscriptionsPostServerRequest) Request() *SubscriptionRegistration {
	if r == nil {
		return nil
	}
	return r.request
}

// GetRequest returns the value of the 'request' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *SubscriptionsPostServerRequest) GetRequest() (value *SubscriptionRegistration, ok bool) {
	ok = r != nil && r.request != nil
	if ok {
		value = r.request
	}
	return
}

// SubscriptionsPostServerResponse is the response for the 'post' method.
type SubscriptionsPostServerResponse struct {
	status   int
	err      *errors.Error
	response *Subscription
}

// Response sets the value of the 'response' parameter.
//
//
func (r *SubscriptionsPostServerResponse) Response(value *Subscription) *SubscriptionsPostServerResponse {
	r.response = value
	return r
}

// Status sets the status code.
func (r *SubscriptionsPostServerResponse) Status(value int) *SubscriptionsPostServerResponse {
	r.status = value
	return r
}

// dispatchSubscriptions navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchSubscriptions(w http.ResponseWriter, r *http.Request, server SubscriptionsServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptSubscriptionsListRequest(w, r, server)
			return
		case "POST":
			adaptSubscriptionsPostRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "labels":
		target := server.Labels()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchGenericLabels(w, r, target, segments[1:])
	default:
		target := server.Subscription(segments[0])
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSubscription(w, r, target, segments[1:])
	}
}

// adaptSubscriptionsListRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSubscriptionsListRequest(w http.ResponseWriter, r *http.Request, server SubscriptionsServer) {
	request := &SubscriptionsListServerRequest{}
	err := readSubscriptionsListRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SubscriptionsListServerResponse{}
	response.status = 200
	err = server.List(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeSubscriptionsListResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptSubscriptionsPostRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptSubscriptionsPostRequest(w http.ResponseWriter, r *http.Request, server SubscriptionsServer) {
	request := &SubscriptionsPostServerRequest{}
	err := readSubscriptionsPostRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &SubscriptionsPostServerResponse{}
	response.status = 201
	err = server.Post(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeSubscriptionsPostResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
