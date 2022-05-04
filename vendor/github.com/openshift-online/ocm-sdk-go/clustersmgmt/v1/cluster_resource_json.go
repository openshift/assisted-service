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
	"io"
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/helpers"
)

func readClusterDeleteRequest(request *ClusterDeleteServerRequest, r *http.Request) error {
	var err error
	query := r.URL.Query()
	request.deprovision, err = helpers.ParseBoolean(query, "deprovision")
	if err != nil {
		return err
	}
	if request.deprovision == nil {
		request.deprovision = helpers.NewBoolean(true)
	}
	return nil
}
func writeClusterDeleteRequest(request *ClusterDeleteRequest, writer io.Writer) error {
	return nil
}
func readClusterDeleteResponse(response *ClusterDeleteResponse, reader io.Reader) error {
	return nil
}
func writeClusterDeleteResponse(response *ClusterDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readClusterGetRequest(request *ClusterGetServerRequest, r *http.Request) error {
	return nil
}
func writeClusterGetRequest(request *ClusterGetRequest, writer io.Writer) error {
	return nil
}
func readClusterGetResponse(response *ClusterGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalCluster(reader)
	return err
}
func writeClusterGetResponse(response *ClusterGetServerResponse, w http.ResponseWriter) error {
	return MarshalCluster(response.body, w)
}
func readClusterHibernateRequest(request *ClusterHibernateServerRequest, r *http.Request) error {
	return nil
}
func writeClusterHibernateRequest(request *ClusterHibernateRequest, writer io.Writer) error {
	return nil
}
func readClusterHibernateResponse(response *ClusterHibernateResponse, reader io.Reader) error {
	return nil
}
func writeClusterHibernateResponse(response *ClusterHibernateServerResponse, w http.ResponseWriter) error {
	return nil
}
func readClusterResumeRequest(request *ClusterResumeServerRequest, r *http.Request) error {
	return nil
}
func writeClusterResumeRequest(request *ClusterResumeRequest, writer io.Writer) error {
	return nil
}
func readClusterResumeResponse(response *ClusterResumeResponse, reader io.Reader) error {
	return nil
}
func writeClusterResumeResponse(response *ClusterResumeServerResponse, w http.ResponseWriter) error {
	return nil
}
func readClusterUpdateRequest(request *ClusterUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalCluster(r.Body)
	return err
}
func writeClusterUpdateRequest(request *ClusterUpdateRequest, writer io.Writer) error {
	return MarshalCluster(request.body, writer)
}
func readClusterUpdateResponse(response *ClusterUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalCluster(reader)
	return err
}
func writeClusterUpdateResponse(response *ClusterUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalCluster(response.body, w)
}
