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

package accountsmgmt // github.com/openshift-online/ocm-sdk-go/accountsmgmt

import (
	"net/http"

	v1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// Server is the interface for the 'accounts_mgmt' service.
type Server interface {

	// V1 returns the server for version 'v1'.
	V1() v1.Server
}

// Dispatch navigates the servers tree till it finds one that matches the given set
// of path segments, and then invokes it.
func Dispatch(w http.ResponseWriter, r *http.Request, server Server, segments []string) {
	if len(segments) == 0 {
		// TODO: This should send the service metadata.
		errors.SendMethodNotAllowed(w, r)
		return
	} else {
		switch segments[0] {
		case "v1":
			version := server.V1()
			if version == nil {
				errors.SendNotFound(w, r)
				return
			}
			v1.Dispatch(w, r, version, segments[1:])
		default:
			errors.SendNotFound(w, r)
			return
		}
	}
}
