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

func readGenericLabelDeleteRequest(request *GenericLabelDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeGenericLabelDeleteRequest(request *GenericLabelDeleteRequest, writer io.Writer) error {
	return nil
}
func readGenericLabelDeleteResponse(response *GenericLabelDeleteResponse, reader io.Reader) error {
	return nil
}
func writeGenericLabelDeleteResponse(response *GenericLabelDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readGenericLabelGetRequest(request *GenericLabelGetServerRequest, r *http.Request) error {
	return nil
}
func writeGenericLabelGetRequest(request *GenericLabelGetRequest, writer io.Writer) error {
	return nil
}
func readGenericLabelGetResponse(response *GenericLabelGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalLabel(reader)
	return err
}
func writeGenericLabelGetResponse(response *GenericLabelGetServerResponse, w http.ResponseWriter) error {
	return MarshalLabel(response.body, w)
}
func readGenericLabelUpdateRequest(request *GenericLabelUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalLabel(r.Body)
	return err
}
func writeGenericLabelUpdateRequest(request *GenericLabelUpdateRequest, writer io.Writer) error {
	return MarshalLabel(request.body, writer)
}
func readGenericLabelUpdateResponse(response *GenericLabelUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalLabel(reader)
	return err
}
func writeGenericLabelUpdateResponse(response *GenericLabelUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalLabel(response.body, w)
}
