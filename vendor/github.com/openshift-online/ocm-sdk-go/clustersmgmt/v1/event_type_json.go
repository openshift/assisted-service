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

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-online/ocm-sdk-go/helpers"
)

// MarshalEvent writes a value of the 'event' type to the given writer.
func MarshalEvent(object *Event, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeEvent(object, stream)
	stream.Flush()
	return stream.Error
}

// writeEvent writes a value of the 'event' type to the given stream.
func writeEvent(object *Event, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	var present_ bool
	present_ = object.bitmap_&1 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("key")
		stream.WriteString(object.key)
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalEvent reads a value of the 'event' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalEvent(source interface{}) (object *Event, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readEvent(iterator)
	err = iterator.Error
	return
}

// readEvent reads a value of the 'event' type from the given iterator.
func readEvent(iterator *jsoniter.Iterator) *Event {
	object := &Event{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "key":
			value := iterator.ReadString()
			object.key = value
			object.bitmap_ |= 1
		default:
			iterator.ReadAny()
		}
	}
	return object
}
