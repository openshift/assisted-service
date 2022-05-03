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
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/errors"
)

// FeatureToggleServer represents the interface the manages the 'feature_toggle' resource.
type FeatureToggleServer interface {

	// Query returns the target 'feature_toggle_query' resource.
	//
	//
	Query() FeatureToggleQueryServer
}

// dispatchFeatureToggle navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchFeatureToggle(w http.ResponseWriter, r *http.Request, server FeatureToggleServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "query":
		target := server.Query()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchFeatureToggleQuery(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}
