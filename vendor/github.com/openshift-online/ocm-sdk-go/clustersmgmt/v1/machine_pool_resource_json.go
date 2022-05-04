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

func readMachinePoolDeleteRequest(request *MachinePoolDeleteServerRequest, r *http.Request) error {
	return nil
}
func writeMachinePoolDeleteRequest(request *MachinePoolDeleteRequest, writer io.Writer) error {
	return nil
}
func readMachinePoolDeleteResponse(response *MachinePoolDeleteResponse, reader io.Reader) error {
	return nil
}
func writeMachinePoolDeleteResponse(response *MachinePoolDeleteServerResponse, w http.ResponseWriter) error {
	return nil
}
func readMachinePoolGetRequest(request *MachinePoolGetServerRequest, r *http.Request) error {
	return nil
}
func writeMachinePoolGetRequest(request *MachinePoolGetRequest, writer io.Writer) error {
	return nil
}
func readMachinePoolGetResponse(response *MachinePoolGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalMachinePool(reader)
	return err
}
func writeMachinePoolGetResponse(response *MachinePoolGetServerResponse, w http.ResponseWriter) error {
	return MarshalMachinePool(response.body, w)
}
func readMachinePoolUpdateRequest(request *MachinePoolUpdateServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalMachinePool(r.Body)
	return err
}
func writeMachinePoolUpdateRequest(request *MachinePoolUpdateRequest, writer io.Writer) error {
	return MarshalMachinePool(request.body, writer)
}
func readMachinePoolUpdateResponse(response *MachinePoolUpdateResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalMachinePool(reader)
	return err
}
func writeMachinePoolUpdateResponse(response *MachinePoolUpdateServerResponse, w http.ResponseWriter) error {
	return MarshalMachinePool(response.body, w)
}
