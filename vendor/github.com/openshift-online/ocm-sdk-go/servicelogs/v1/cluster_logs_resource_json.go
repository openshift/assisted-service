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

package v1 // github.com/openshift-online/ocm-sdk-go/servicelogs/v1

import (
	"io"
	"net/http"

	"github.com/openshift-online/ocm-sdk-go/helpers"
)

func readClusterLogsAddRequest(request *ClusterLogsAddServerRequest, r *http.Request) error {
	var err error
	request.body, err = UnmarshalLogEntry(r.Body)
	return err
}
func writeClusterLogsAddRequest(request *ClusterLogsAddRequest, writer io.Writer) error {
	return MarshalLogEntry(request.body, writer)
}
func readClusterLogsAddResponse(response *ClusterLogsAddResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalLogEntry(reader)
	return err
}
func writeClusterLogsAddResponse(response *ClusterLogsAddServerResponse, w http.ResponseWriter) error {
	return MarshalLogEntry(response.body, w)
}
func readClusterLogsListRequest(request *ClusterLogsListServerRequest, r *http.Request) error {
	var err error
	query := r.URL.Query()
	request.order, err = helpers.ParseString(query, "order")
	if err != nil {
		return err
	}
	request.page, err = helpers.ParseInteger(query, "page")
	if err != nil {
		return err
	}
	if request.page == nil {
		request.page = helpers.NewInteger(1)
	}
	request.search, err = helpers.ParseString(query, "search")
	if err != nil {
		return err
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
func writeClusterLogsListRequest(request *ClusterLogsListRequest, writer io.Writer) error {
	return nil
}
func readClusterLogsListResponse(response *ClusterLogsListResponse, reader io.Reader) error {
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
			items := readLogEntryList(iterator)
			response.items = &LogEntryList{
				items: items,
			}
		default:
			iterator.ReadAny()
		}
	}
	return iterator.Error
}
func writeClusterLogsListResponse(response *ClusterLogsListServerResponse, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.status)
	stream := helpers.NewStream(w)
	stream.WriteObjectStart()
	stream.WriteObjectField("kind")
	count := 1
	stream.WriteString(LogEntryListKind)
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
			writeLogEntryList(response.items.items, stream)
			count++
		}
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
