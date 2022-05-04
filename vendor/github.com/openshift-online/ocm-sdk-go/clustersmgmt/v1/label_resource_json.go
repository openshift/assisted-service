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
)

func readLabelDeleteRequest(request *LabelDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeLabelDeleteRequest(request *LabelDeleteRequest, writer io.Writer) error {
	return nil
}
func readLabelDeleteResponse(response *LabelDeleteResponse, reader io.Reader) error {
	return nil
}
func writeLabelDeleteResponse(response *LabelDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readLabelGetRequest(request *LabelGetServerRequest, r *http.Request) error {
	return nil
}
func writeLabelGetRequest(request *LabelGetRequest, writer io.Writer) error {
	return nil
}
func readLabelGetResponse(response *LabelGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalLabel(reader)
	return err
}
func writeLabelGetResponse(response *LabelGetServerResponse, w http.ResponseWriter) error {
	return MarshalLabel(response.body, w)
}
func readLabelUpdateRequest(request *LabelUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalLabel(r.Body)
	return err
}
func writeLabelUpdateRequest(request *LabelUpdateRequest, writer io.Writer) error {
	return MarshalLabel(request.body, writer)
}
func readLabelUpdateResponse(response *LabelUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalLabel(reader)
	return err
}
func writeLabelUpdateResponse(response *LabelUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalLabel(response.body, w)
}
