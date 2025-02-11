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

// OrganizationServer represents the interface the manages the 'organization' resource.
type OrganizationServer interface {

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the organization.
	Get(ctx context.Context, request *OrganizationGetServerRequest, response *OrganizationGetServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the organization.
	Update(ctx context.Context, request *OrganizationUpdateServerRequest, response *OrganizationUpdateServerResponse) error

	// Labels returns the target 'generic_labels' resource.
	//
	// Reference to the list of labels of a specific organization.
	Labels() GenericLabelsServer

	// QuotaCost returns the target 'quota_cost' resource.
	//
	// Reference to the service that returns a summary of quota cost for this organization
	QuotaCost() QuotaCostServer

	// QuotaSummary returns the target 'quota_summary' resource.
	//
	// Reference to the service that returns the summary of the resource quota for this
	// organization.
	QuotaSummary() QuotaSummaryServer

	// ResourceQuota returns the target 'resource_quotas' resource.
	//
	// Reference to the service that manages the resource quotas for this
	// organization.
	ResourceQuota() ResourceQuotasServer

	// SummaryDashboard returns the target 'summary_dashboard' resource.
	//
	// Reference to the service that manages the resource quotas for this
	// organization.
	SummaryDashboard() SummaryDashboardServer
}

// OrganizationGetServerRequest is the request for the 'get' method.
type OrganizationGetServerRequest struct {
}

// OrganizationGetServerResponse is the response for the 'get' method.
type OrganizationGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Organization
}

// Body sets the value of the 'body' parameter.
//
//
func (r *OrganizationGetServerResponse) Body(value *Organization) *OrganizationGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *OrganizationGetServerResponse) Status(value int) *OrganizationGetServerResponse {
	r.status = value
	return r
}

// OrganizationUpdateServerRequest is the request for the 'update' method.
type OrganizationUpdateServerRequest struct {
	body *Organization
}

// Body returns the value of the 'body' parameter.
//
//
func (r *OrganizationUpdateServerRequest) Body() *Organization {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *OrganizationUpdateServerRequest) GetBody() (value *Organization, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// OrganizationUpdateServerResponse is the response for the 'update' method.
type OrganizationUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Organization
}

// Body sets the value of the 'body' parameter.
//
//
func (r *OrganizationUpdateServerResponse) Body(value *Organization) *OrganizationUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *OrganizationUpdateServerResponse) Status(value int) *OrganizationUpdateServerResponse {
	r.status = value
	return r
}

// dispatchOrganization navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchOrganization(w http.ResponseWriter, r *http.Request, server OrganizationServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "GET":
			adaptOrganizationGetRequest(w, r, server)
			return
		case "PATCH":
			adaptOrganizationUpdateRequest(w, r, server)
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
	case "quota_cost":
		target := server.QuotaCost()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchQuotaCost(w, r, target, segments[1:])
	case "quota_summary":
		target := server.QuotaSummary()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchQuotaSummary(w, r, target, segments[1:])
	case "resource_quota":
		target := server.ResourceQuota()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchResourceQuotas(w, r, target, segments[1:])
	case "summary_dashboard":
		target := server.SummaryDashboard()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSummaryDashboard(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptOrganizationGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptOrganizationGetRequest(w http.ResponseWriter, r *http.Request, server OrganizationServer) {
	request := &OrganizationGetServerRequest{}
	err := readOrganizationGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &OrganizationGetServerResponse{}
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
	err = writeOrganizationGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptOrganizationUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptOrganizationUpdateRequest(w http.ResponseWriter, r *http.Request, server OrganizationServer) {
	request := &OrganizationUpdateServerRequest{}
	err := readOrganizationUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &OrganizationUpdateServerResponse{}
	response.status = 200
	err = server.Update(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeOrganizationUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
