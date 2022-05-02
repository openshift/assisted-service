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

package v1 // github.com/openshift-online/ocm-sdk-go/jobqueue/v1

import (
	"io"
	"net/http"
	"time"

	"github.com/openshift-online/ocm-sdk-go/helpers"
)

func readQueueGetRequest(request *QueueGetServerRequest, r *http.Request) error {
	return nil
}
func writeQueueGetRequest(request *QueueGetRequest, writer io.Writer) error {
	return nil
}
func readQueueGetResponse(response *QueueGetResponse, reader io.Reader) error {
	var err error
	response.body, err = UnmarshalQueue(reader)
	return err
}
func writeQueueGetResponse(response *QueueGetServerResponse, w http.ResponseWriter) error {
	return MarshalQueue(response.body, w)
}
func readQueuePopRequest(request *QueuePopServerRequest, r *http.Request) error {
	return nil
}
func writeQueuePopRequest(request *QueuePopRequest, writer io.Writer) error {
	return nil
}
func readQueuePopResponse(response *QueuePopResponse, reader io.Reader) error {
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
		case "href":
			value := iterator.ReadString()
			response.href = &value
		case "id":
			value := iterator.ReadString()
			response.id = &value
		case "abandoned_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.abandonedAt = &value
		case "arguments":
			value := iterator.ReadString()
			response.arguments = &value
		case "attempts":
			value := iterator.ReadInt()
			response.attempts = &value
		case "created_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.createdAt = &value
		case "kind":
			value := iterator.ReadString()
			response.kind = &value
		case "receipt_id":
			value := iterator.ReadString()
			response.receiptId = &value
		case "updated_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.updatedAt = &value
		default:
			iterator.ReadAny()
		}
	}
	return iterator.Error
}
func writeQueuePopResponse(response *QueuePopServerResponse, w http.ResponseWriter) error {
	count := 0
	stream := helpers.NewStream(w)
	stream.WriteObjectStart()
	if response.href != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("href")
		stream.WriteString(*response.href)
		count++
	}
	if response.id != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("id")
		stream.WriteString(*response.id)
		count++
	}
	if response.abandonedAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("abandoned_at")
		stream.WriteString((*response.abandonedAt).Format(time.RFC3339))
		count++
	}
	if response.arguments != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("arguments")
		stream.WriteString(*response.arguments)
		count++
	}
	if response.attempts != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("attempts")
		stream.WriteInt(*response.attempts)
		count++
	}
	if response.createdAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("created_at")
		stream.WriteString((*response.createdAt).Format(time.RFC3339))
		count++
	}
	if response.kind != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("kind")
		stream.WriteString(*response.kind)
		count++
	}
	if response.receiptId != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("receipt_id")
		stream.WriteString(*response.receiptId)
		count++
	}
	if response.updatedAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("updated_at")
		stream.WriteString((*response.updatedAt).Format(time.RFC3339))
		count++
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
func readQueuePushRequest(request *QueuePushServerRequest, r *http.Request) error {
	iterator, err := helpers.NewIterator(r.Body)
	if err != nil {
		return err
	}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "abandoned_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			request.abandonedAt = &value
		case "arguments":
			value := iterator.ReadString()
			request.arguments = &value
		case "attempts":
			value := iterator.ReadInt()
			request.attempts = &value
		case "created_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			request.createdAt = &value
		default:
			iterator.ReadAny()
		}
	}
	err = iterator.Error
	if err != nil {
		return err
	}
	return nil
}
func writeQueuePushRequest(request *QueuePushRequest, writer io.Writer) error {
	count := 0
	stream := helpers.NewStream(writer)
	stream.WriteObjectStart()
	if request.abandonedAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("abandoned_at")
		stream.WriteString((*request.abandonedAt).Format(time.RFC3339))
		count++
	}
	if request.arguments != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("arguments")
		stream.WriteString(*request.arguments)
		count++
	}
	if request.attempts != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("attempts")
		stream.WriteInt(*request.attempts)
		count++
	}
	if request.createdAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("created_at")
		stream.WriteString((*request.createdAt).Format(time.RFC3339))
		count++
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
func readQueuePushResponse(response *QueuePushResponse, reader io.Reader) error {
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
		case "href":
			value := iterator.ReadString()
			response.href = &value
		case "id":
			value := iterator.ReadString()
			response.id = &value
		case "abandoned_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.abandonedAt = &value
		case "arguments":
			value := iterator.ReadString()
			response.arguments = &value
		case "attempts":
			value := iterator.ReadInt()
			response.attempts = &value
		case "created_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.createdAt = &value
		case "kind":
			value := iterator.ReadString()
			response.kind = &value
		case "receipt_id":
			value := iterator.ReadString()
			response.receiptId = &value
		case "updated_at":
			text := iterator.ReadString()
			value, err := time.Parse(time.RFC3339, text)
			if err != nil {
				iterator.ReportError("", err.Error())
			}
			response.updatedAt = &value
		default:
			iterator.ReadAny()
		}
	}
	return iterator.Error
}
func writeQueuePushResponse(response *QueuePushServerResponse, w http.ResponseWriter) error {
	count := 0
	stream := helpers.NewStream(w)
	stream.WriteObjectStart()
	if response.href != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("href")
		stream.WriteString(*response.href)
		count++
	}
	if response.id != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("id")
		stream.WriteString(*response.id)
		count++
	}
	if response.abandonedAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("abandoned_at")
		stream.WriteString((*response.abandonedAt).Format(time.RFC3339))
		count++
	}
	if response.arguments != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("arguments")
		stream.WriteString(*response.arguments)
		count++
	}
	if response.attempts != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("attempts")
		stream.WriteInt(*response.attempts)
		count++
	}
	if response.createdAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("created_at")
		stream.WriteString((*response.createdAt).Format(time.RFC3339))
		count++
	}
	if response.kind != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("kind")
		stream.WriteString(*response.kind)
		count++
	}
	if response.receiptId != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("receipt_id")
		stream.WriteString(*response.receiptId)
		count++
	}
	if response.updatedAt != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("updated_at")
		stream.WriteString((*response.updatedAt).Format(time.RFC3339))
		count++
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
