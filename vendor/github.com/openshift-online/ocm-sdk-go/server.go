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

package sdk // github.com/openshift-online/ocm-sdk-go

import (
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/accountsmgmt"
	"github.com/openshift-online/ocm-sdk-go/authorizations"
	"github.com/openshift-online/ocm-sdk-go/clustersmgmt"
	"github.com/openshift-online/ocm-sdk-go/errors"
	"github.com/openshift-online/ocm-sdk-go/helpers"
	"github.com/openshift-online/ocm-sdk-go/jobqueue"
	"github.com/openshift-online/ocm-sdk-go/servicelogs"
)

// Server is the interface of the top level server.
type Server interface {

	// AccountsMgmt returns the server for service 'accounts_mgmt'.
	AccountsMgmt() accountsmgmt.Server

	// Authorizations returns the server for service 'authorizations'.
	Authorizations() authorizations.Server

	// ClustersMgmt returns the server for service 'clusters_mgmt'.
	ClustersMgmt() clustersmgmt.Server

	// JobQueue returns the server for service 'job_queue'.
	JobQueue() jobqueue.Server

	// ServiceLogs returns the server for service 'service_logs'.
	ServiceLogs() servicelogs.Server
}

// Dispatch navigates the servers tree till it finds one that matches the given set
// of path segments, and then invokes it.
func Dispatch(w http.ResponseWriter, r *http.Request, server Server, segments []string) {
	if len(segments) > 0 && segments[0] == "api" {
		dispatch(w, r, server, segments[1:])
		return
	}
	errors.SendNotFound(w, r)
}
func dispatch(w http.ResponseWriter, r *http.Request, server Server, segments []string) {
	if len(segments) == 0 {
		// TODO: This should send the metadata.
		errors.SendMethodNotAllowed(w, r)
		return
	} else {
		switch segments[0] {
		case "accounts_mgmt":
			service := server.AccountsMgmt()
			if service == nil {
				errors.SendNotFound(w, r)
				return
			}
			accountsmgmt.Dispatch(w, r, service, segments[1:])
		case "authorizations":
			service := server.Authorizations()
			if service == nil {
				errors.SendNotFound(w, r)
				return
			}
			authorizations.Dispatch(w, r, service, segments[1:])
		case "clusters_mgmt":
			service := server.ClustersMgmt()
			if service == nil {
				errors.SendNotFound(w, r)
				return
			}
			clustersmgmt.Dispatch(w, r, service, segments[1:])
		case "job_queue":
			service := server.JobQueue()
			if service == nil {
				errors.SendNotFound(w, r)
				return
			}
			jobqueue.Dispatch(w, r, service, segments[1:])
		case "service_logs":
			service := server.ServiceLogs()
			if service == nil {
				errors.SendNotFound(w, r)
				return
			}
			servicelogs.Dispatch(w, r, service, segments[1:])
		default:
			errors.SendNotFound(w, r)
			return
		}
	}
}

// Adapter is an HTTP handler that knows how to translate HTTP requests into calls
// to the methods of an object that implements the Server interface.
type Adapter struct {
	server Server
}

// NewAdapter creates a new adapter that will translate HTTP requests into calls to
// the given server.
func NewAdapter(server Server) *Adapter {
	return &Adapter{
		server: server,
	}
}

// ServeHTTP is the implementation of the http.Handler interface.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	Dispatch(w, r, a.server, helpers.Segments(r.URL.Path))
}
