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

package v1 // github.com/openshift-online/ocm-sdk-go/authorizations/v1

import (
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/errors"
)

// Server represents the interface the manages the 'root' resource.
type Server interface {

	// AccessReview returns the target 'access_review' resource.
	//
	// Reference to the resource that is used to submit access review requests.
	AccessReview() AccessReviewServer

	// CapabilityReview returns the target 'capability_review' resource.
	//
	// Reference to the resource that is used to submit capability review requests.
	CapabilityReview() CapabilityReviewServer

	// ExportControlReview returns the target 'export_control_review' resource.
	//
	// Reference to the resource that is used to submit export control review requests.
	ExportControlReview() ExportControlReviewServer

	// FeatureReview returns the target 'feature_review' resource.
	//
	// Reference to the resource that is used to submit feature review requests.
	FeatureReview() FeatureReviewServer

	// ResourceReview returns the target 'resource_review' resource.
	//
	// Reference to the resource that is used to submit resource review requests.
	ResourceReview() ResourceReviewServer

	// SelfAccessReview returns the target 'self_access_review' resource.
	//
	// Reference to the resource that is used to submit self access review requests.
	SelfAccessReview() SelfAccessReviewServer

	// SelfCapabilityReview returns the target 'self_capability_review' resource.
	//
	// Reference to the resource that is used to submit self capability review requests.
	SelfCapabilityReview() SelfCapabilityReviewServer

	// SelfFeatureReview returns the target 'self_feature_review' resource.
	//
	// Reference to the resource that is used to submit self feature review requests.
	SelfFeatureReview() SelfFeatureReviewServer

	// SelfTermsReview returns the target 'self_terms_review' resource.
	//
	// Reference to the resource that is used to submit Red Hat's Terms and Conditions
	// for using OpenShift Dedicated and Amazon Red Hat OpenShift self-review requests.
	SelfTermsReview() SelfTermsReviewServer

	// TermsReview returns the target 'terms_review' resource.
	//
	// Reference to the resource that is used to submit Red Hat's Terms and Conditions
	// for using OpenShift Dedicated and Amazon Red Hat OpenShift review requests.
	TermsReview() TermsReviewServer
}

// Dispatch navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func Dispatch(w http.ResponseWriter, r *http.Request, server Server, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "access_review":
		target := server.AccessReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAccessReview(w, r, target, segments[1:])
	case "capability_review":
		target := server.CapabilityReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchCapabilityReview(w, r, target, segments[1:])
	case "export_control_review":
		target := server.ExportControlReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchExportControlReview(w, r, target, segments[1:])
	case "feature_review":
		target := server.FeatureReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchFeatureReview(w, r, target, segments[1:])
	case "resource_review":
		target := server.ResourceReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchResourceReview(w, r, target, segments[1:])
	case "self_access_review":
		target := server.SelfAccessReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSelfAccessReview(w, r, target, segments[1:])
	case "self_capability_review":
		target := server.SelfCapabilityReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSelfCapabilityReview(w, r, target, segments[1:])
	case "self_feature_review":
		target := server.SelfFeatureReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSelfFeatureReview(w, r, target, segments[1:])
	case "self_terms_review":
		target := server.SelfTermsReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchSelfTermsReview(w, r, target, segments[1:])
	case "terms_review":
		target := server.TermsReview()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchTermsReview(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}
