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

func readRoleBindingDeleteRequest(request *RoleBindingDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeRoleBindingDeleteRequest(request *RoleBindingDeleteRequest, writer io.Writer) error {
	return nil
}
func readRoleBindingDeleteResponse(response *RoleBindingDeleteResponse, reader io.Reader) error {
	return nil
}
func writeRoleBindingDeleteResponse(response *RoleBindingDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readRoleBindingGetRequest(request *RoleBindingGetServerRequest, r *http.Request) error {
	return nil
}
func writeRoleBindingGetRequest(request *RoleBindingGetRequest, writer io.Writer) error {
	return nil
}
func readRoleBindingGetResponse(response *RoleBindingGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalRoleBinding(reader)
	return err
}
func writeRoleBindingGetResponse(response *RoleBindingGetServerResponse, w http.ResponseWriter) error {
	return MarshalRoleBinding(response.body, w)
}
func readRoleBindingUpdateRequest(request *RoleBindingUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalRoleBinding(r.Body)
	return err
}
func writeRoleBindingUpdateRequest(request *RoleBindingUpdateRequest, writer io.Writer) error {
	return MarshalRoleBinding(request.body, writer)
}
func readRoleBindingUpdateResponse(response *RoleBindingUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalRoleBinding(reader)
	return err
}
func writeRoleBindingUpdateResponse(response *RoleBindingUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalRoleBinding(response.body, w)
}
