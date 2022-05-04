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

	"github.com/openshift-online/ocm-sdk-go/helpers"
)

func readPermissionsAddRequest(request *PermissionsAddServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalPermission(r.Body)
	return err
}
func writePermissionsAddRequest(request *PermissionsAddRequest, writer io.Writer) error {
	return MarshalPermission(request.body, writer)
}
func readPermissionsAddResponse(response *PermissionsAddResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalPermission(reader)
	return err
}
func writePermissionsAddResponse(response *PermissionsAddServerResponse, w http.ResponseWriter) error {
	return MarshalPermission(response.body, w)
}
func readPermissionsListRequest(request *PermissionsListServerRequest, r *http.Request) error {
	var err error
	query := r.URL.Query()
	request.page, err = helpers.ParseInteger(query, "page")
	if err != nil {
		return err
	}
	if request.page == nil {
		request.page = helpers.NewInteger(1)
	}
	request.size, err = helpers.ParseInteger(query, "size")
	if err != nil {
		return err
	}
	if request.size == nil {
		request.size = helpers.NewInteger(100)
	}
	return nil
}
func writePermissionsListRequest(request *PermissionsListRequest, writer io.Writer) error {
	return nil
}
func readPermissionsListResponse(response *PermissionsListResponse, reader io.Reader) error {
	iterator, err := helpers.NewIterator(reader)
	if err != nil {
		return err
	}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "page":
			value := iterator.ReadInt()
			response.page = &value
		case "size":
			value := iterator.ReadInt()
			response.size = &value
		case "total":
			value := iterator.ReadInt()
			response.total = &value
		case "items":
			items := readPermissionList(iterator)
			response.items = &PermissionList{
				items: items,
			}
		default:
			iterator.ReadAny()
		}
	}
	return iterator.Error
}
func writePermissionsListResponse(response *PermissionsListServerResponse, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.status)
	stream := helpers.NewStream(w)
	stream.WriteObjectStart()
	stream.WriteObjectField("kind")
	count := 1
	stream.WriteString(PermissionListKind)
	if response.items != nil && response.items.href != "" {
		stream.WriteMore()
		stream.WriteObjectField("href")
		stream.WriteString(response.items.href)
		count++
	}
	if response.page != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("page")
		stream.WriteInt(*response.page)
		count++
	}
	if response.size != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("size")
		stream.WriteInt(*response.size)
		count++
	}
	if response.total != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("total")
		stream.WriteInt(*response.total)
		count++
	}
	if response.items != nil {
		if response.items.items != nil {
			if count > 0 {
				stream.WriteMore()
			}
			stream.WriteObjectField("items")
			writePermissionList(response.items.items, stream)
			count++
		}
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
