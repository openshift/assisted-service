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

func readRoleDeleteRequest(request *RoleDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeRoleDeleteRequest(request *RoleDeleteRequest, writer io.Writer) error {
	return nil
}
func readRoleDeleteResponse(response *RoleDeleteResponse, reader io.Reader) error {
	return nil
}
func writeRoleDeleteResponse(response *RoleDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readRoleGetRequest(request *RoleGetServerRequest, r *http.Request) error {
	return nil
}
func writeRoleGetRequest(request *RoleGetRequest, writer io.Writer) error {
	return nil
}
func readRoleGetResponse(response *RoleGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalRole(reader)
	return err
}
func writeRoleGetResponse(response *RoleGetServerResponse, w http.ResponseWriter) error {
	return MarshalRole(response.body, w)
}
func readRoleUpdateRequest(request *RoleUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalRole(r.Body)
	return err
}
func writeRoleUpdateRequest(request *RoleUpdateRequest, writer io.Writer) error {
	return MarshalRole(request.body, writer)
}
func readRoleUpdateResponse(response *RoleUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalRole(reader)
	return err
}
func writeRoleUpdateResponse(response *RoleUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalRole(response.body, w)
}
