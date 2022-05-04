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

	"github.com/openshift-online/ocm-sdk-go/helpers"
)

func readJobFailureRequest(request *JobFailureServerRequest, r *http.Request) error {
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
		case "failure_reason":
			value := iterator.ReadString()
			request.failureReason = &value
		case "receipt_id":
			value := iterator.ReadString()
			request.receiptId = &value
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
func writeJobFailureRequest(request *JobFailureRequest, writer io.Writer) error {
	count := 0
	stream := helpers.NewStream(writer)
	stream.WriteObjectStart()
	if request.failureReason != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("failure_reason")
		stream.WriteString(*request.failureReason)
		count++
	}
	if request.receiptId != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("receipt_id")
		stream.WriteString(*request.receiptId)
		count++
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
func readJobFailureResponse(response *JobFailureResponse, reader io.Reader) error {
	return nil
}
func writeJobFailureResponse(response *JobFailureServerResponse, w http.ResponseWriter) error {
	return nil
}
func readJobSuccessRequest(request *JobSuccessServerRequest, r *http.Request) error {
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
		case "receipt_id":
			value := iterator.ReadString()
			request.receiptId = &value
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
func writeJobSuccessRequest(request *JobSuccessRequest, writer io.Writer) error {
	count := 0
	stream := helpers.NewStream(writer)
	stream.WriteObjectStart()
	if request.receiptId != nil {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("receipt_id")
		stream.WriteString(*request.receiptId)
		count++
	}
	stream.WriteObjectEnd()
	stream.Flush()
	return stream.Error
}
func readJobSuccessResponse(response *JobSuccessResponse, reader io.Reader) error {
	return nil
}
func writeJobSuccessResponse(response *JobSuccessServerResponse, w http.ResponseWriter) error {
	return nil
}
