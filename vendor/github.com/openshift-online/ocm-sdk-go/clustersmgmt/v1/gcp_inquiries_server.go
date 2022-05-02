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
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/errors"
)

// GCPInquiriesServer represents the interface the manages the 'GCP_inquiries' resource.
type GCPInquiriesServer interface {

	// EncryptionKeys returns the target 'encryption_keys_inquiry' resource.
	//
	// Reference to the resource that manages a collection of encryption keys.
	EncryptionKeys() EncryptionKeysInquiryServer

	// KeyRings returns the target 'key_rings_inquiry' resource.
	//
	// Reference to the resource that manages a collection of key rings.
	KeyRings() KeyRingsInquiryServer

	// Regions returns the target 'available_regions_inquiry' resource.
	//
	// Reference to the resource that manages a collection of regions.
	Regions() AvailableRegionsInquiryServer

	// Vpcs returns the target 'vpcs_inquiry' resource.
	//
	// Reference to the resource that manages a collection of vpcs.
	Vpcs() VpcsInquiryServer
}

// dispatchGCPInquiries navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchGCPInquiries(w http.ResponseWriter, r *http.Request, server GCPInquiriesServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "encryption_keys":
		target := server.EncryptionKeys()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchEncryptionKeysInquiry(w, r, target, segments[1:])
	case "key_rings":
		target := server.KeyRings()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchKeyRingsInquiry(w, r, target, segments[1:])
	case "regions":
		target := server.Regions()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAvailableRegionsInquiry(w, r, target, segments[1:])
	case "vpcs":
		target := server.Vpcs()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchVpcsInquiry(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}
