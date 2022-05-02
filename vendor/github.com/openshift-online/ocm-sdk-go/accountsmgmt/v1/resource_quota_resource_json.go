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
	"io"
	"net/http"
)

func readResourceQuotaDeleteRequest(request *ResourceQuotaDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeResourceQuotaDeleteRequest(request *ResourceQuotaDeleteRequest, writer io.Writer) error {
	return nil
}
func readResourceQuotaDeleteResponse(response *ResourceQuotaDeleteResponse, reader io.Reader) error {
	return nil
}
func writeResourceQuotaDeleteResponse(response *ResourceQuotaDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readResourceQuotaGetRequest(request *ResourceQuotaGetServerRequest, r *http.Request) error {
	return nil
}
func writeResourceQuotaGetRequest(request *ResourceQuotaGetRequest, writer io.Writer) error {
	return nil
}
func readResourceQuotaGetResponse(response *ResourceQuotaGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalResourceQuota(reader)
	return err
}
func writeResourceQuotaGetResponse(response *ResourceQuotaGetServerResponse, w http.ResponseWriter) error {
	return MarshalResourceQuota(response.body, w)
}
func readResourceQuotaUpdateRequest(request *ResourceQuotaUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalResourceQuota(r.Body)
	return err
}
func writeResourceQuotaUpdateRequest(request *ResourceQuotaUpdateRequest, writer io.Writer) error {
	return MarshalResourceQuota(request.body, writer)
}
func readResourceQuotaUpdateResponse(response *ResourceQuotaUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalResourceQuota(reader)
	return err
}
func writeResourceQuotaUpdateResponse(response *ResourceQuotaUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalResourceQuota(response.body, w)
}
