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

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-online/ocm-sdk-go/helpers"
)

// MarshalQuotaCost writes a value of the 'quota_cost' type to the given writer.
func MarshalQuotaCost(object *QuotaCost, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeQuotaCost(object, stream)
	stream.Flush()
	return stream.Error
}

// writeQuotaCost writes a value of the 'quota_cost' type to the given stream.
func writeQuotaCost(object *QuotaCost, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	var present_ bool
	present_ = object.bitmap_&1 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("allowed")
		stream.WriteInt(object.allowed)
		count++
	}
	present_ = object.bitmap_&2 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("consumed")
		stream.WriteInt(object.consumed)
		count++
	}
	present_ = object.bitmap_&4 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("organization_id")
		stream.WriteString(object.organizationID)
		count++
	}
	present_ = object.bitmap_&8 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("quota_id")
		stream.WriteString(object.quotaID)
		count++
	}
	present_ = object.bitmap_&16 != 0 && object.relatedResources != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("related_resources")
		writeRelatedResourceList(object.relatedResources, stream)
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalQuotaCost reads a value of the 'quota_cost' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalQuotaCost(source interface{}) (object *QuotaCost, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readQuotaCost(iterator)
	err = iterator.Error
	return
}

// readQuotaCost reads a value of the 'quota_cost' type from the given iterator.
func readQuotaCost(iterator *jsoniter.Iterator) *QuotaCost {
	object := &QuotaCost{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "allowed":
			value := iterator.ReadInt()
			object.allowed = value
			object.bitmap_ |= 1
		case "consumed":
			value := iterator.ReadInt()
			object.consumed = value
			object.bitmap_ |= 2
		case "organization_id":
			value := iterator.ReadString()
			object.organizationID = value
			object.bitmap_ |= 4
		case "quota_id":
			value := iterator.ReadString()
			object.quotaID = value
			object.bitmap_ |= 8
		case "related_resources":
			value := readRelatedResourceList(iterator)
			object.relatedResources = value
			object.bitmap_ |= 16
		default:
			iterator.ReadAny()
		}
	}
	return object
}
